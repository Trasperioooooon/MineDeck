package transfer

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/modinfo"
)

// 交接包的 zip 原语。备份的 zipDirs / unzipTo（internal/inst/backup.go）打的是「几个世界子目录」，
// 交接包要打的是「一整个含集」（世界 + 配置 + 模组 + 名单，瘦身或完整两种），跳过表也不同，
// 所以在这里另写一套；防穿越（zip-slip）和流式拷贝（绝不整读进内存——大包多 GB 是常态）的纪律一样。

// zipMethod 按扩展名选压缩方式：jar / mca / png 本身已压缩或不可再压，Store 直接存，
// 比再压一遍快得多且体积不亏；其余（配置文本等）用 Deflate。
func zipMethod(rel string) uint16 {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".jar", ".zip", ".png", ".ogg", ".mp3", ".mca", ".bin":
		return zip.Store
	}
	return zip.Deflate
}

// packFile 待打包的一项：实例目录里的相对路径 + 元数据（ mods / plugins 里 modinfo 读出的信息，
// 或 nil 表示只是普通文件）。
type packFile struct {
	rel  string // 相对实例目录（本机分隔符）
	abs  string
	size int64
	mod  *modinfo.Mod // 非 nil ⇒ 是 mods/plugins 里的 jar，清单要记 sha1（打包时顺带算）
}

// writePack 把 files 打进 dest（先当你给的只是半成品：调用方用 .part 名写，成功后自己换名）。
// manifest 在最后写进包根（此刻 modinfo 的 sha1 都算完了）。
// progress(done, total) 按字节报；fileCount(n, done, total) 每隔一批文件报一次数量（任务的 label 换字用）。
func writePack(ctx context.Context, dest string, files []packFile, mf *Manifest,
	progress func(done, total int64), fileCount func(n int, done, total int64)) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	var done, n int64
	buf := make([]byte, 512<<10)
	fail := func(err error) error {
		if cerr := zw.Close(); err == nil {
			err = cerr
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	}
	cancelled := func() error {
		if ctx == nil {
			return nil
		}
		return ctx.Err()
	}
	for _, pf := range files {
		if err := cancelled(); err != nil {
			return fail(err)
		}
		hdr := &zip.FileHeader{
			Name:     PayloadPrefix + filepath.ToSlash(pf.rel),
			Method:   zipMethod(pf.rel),
			Modified: modTime(pf.abs),
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return fail(err)
		}
		r, err := os.Open(pf.abs)
		if err != nil {
			return fail(fmt.Errorf("读不了 %s: %w", pf.rel, err))
		}
		var sum string
		var isMod bool
		if pf.mod != nil {
			isMod = true
			h := sha1.New()
			// sha1 用 TeeReader 顺带走，不额外读第二遍
			_, err = io.CopyBuffer(w, io.TeeReader(r, h), buf)
			if err == nil {
				sum = hex.EncodeToString(h.Sum(nil))
			}
		} else {
			_, err = io.CopyBuffer(w, r, buf)
		}
		r.Close()
		if err != nil {
			return fail(fmt.Errorf("打包 %s 失败: %w", pf.rel, err))
		}
		if isMod {
			mf.addMod(pf, sum)
		}
		done += pf.size
		if done > mf.Counts.Bytes { // 收集时点之后文件还在变大：不能让进度越过总量
			done = mf.Counts.Bytes
		}
		n++
		progress(done, mf.Counts.Bytes)
		if n%2048 == 0 {
			fileCount(int(n), done, mf.Counts.Bytes)
		}
	}
	fileCount(int(n), done, mf.Counts.Bytes)
	b, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return fail(err)
	}
	w, err := zw.Create(manifestName)
	if err != nil {
		return fail(err)
	}
	if _, err := w.Write(b); err != nil {
		return fail(err)
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// extractOpts 解包时的几道限制（交接包是别人给的文件，按恶意输入对待）。
type extractOpts struct {
	// slim 瘦身模式的包（清单里 full=false）：服务端核心是导入时刚下载、校验过的，包里不该有、有也不采用。
	//   - 目标位置已经有文件时不覆盖：解包之前目录里只有下载的核心和安装器装出来的依赖库。
	//   - 核心的地盘上的东西不解：根目录的 jar，和 libraries、versions、cache、.fabric 这几个目录。
	//     有的核心（Paper、Fabric、原版）第一次启动才把依赖库解到这些目录里，那时候目录里先有一份别人放的就不好说了。
	//     MineDeck 自己导出的瘦身包里没有这些（见 export.go），所以正常的包不受影响。
	slim bool
	// declared 清单里写的字节总数（manifest.counts.bytes）；包里实际申报的总量比它大出一截就拒绝。0 = 不比。
	declared int64
	// free 目标盘还剩多少字节；nil = 不查。装不下就在动手之前拒绝，免得把盘写满连累同一块盘上别的服务器。
	free func() (uint64, error)
}

// extractResult 解包结果。
type extractResult struct {
	bytes   int64
	control []string // 包里带了、但没有采用的控制文件（instance.json、run.bat 这类由 MineDeck 按本机生成的）
	kept    []string // slim 时没有解出来的：和下载的核心重名，或者落在核心的地盘上
}

// coreTurf 瘦身模式下属于服务端核心的路径（rel 相对实例目录，本机分隔符）：根目录的 jar 和核心自己的几个目录。
func coreTurf(rel string) bool {
	low := strings.ToLower(filepath.ToSlash(rel))
	first, _, nested := strings.Cut(low, "/")
	if !nested {
		return strings.HasSuffix(low, ".jar")
	}
	switch first {
	case "libraries", "versions", "cache", ".fabric":
		return true
	}
	return false
}

// controlRoot 实例根目录下由 MineDeck 自己写、自己读的文件（小写）：包里带了也不采用。
//   - instance.json、run.bat、user_jvm_args.txt 决定这台电脑上执行什么。不拦的话，导入在解包之后、收尾之前被打断
//     （关程序、断电），目录里留下的就是包里那份 instance.json——下次启动扫描会把它登记成一台服务器，
//     用哪个程序当 Java、带什么参数全由包说了算。
//   - log4j2_112-116.xml 是老版本的日志配置：MineDeck 看到这个文件在，就给启动参数加上对它的引用；
//     老版本 log4j 的配置文件能跑脚本、能发 JNDI 请求，和 JVM 参数是同一类东西。导入时按官方的重新下载。
//   - gameai.json、gameai-state.json 是游戏内 AI 的设置和记账（谁能用、用了多少）：花的是接手的人的钱，不能由包来定。
var controlRoot = map[string]bool{
	"instance.json": true, "instance.json.tmp": true, "instance.json.bak": true,
	"run.bat": true, "run.before-minedeck.bat": true, "user_jvm_args.txt": true,
	"log4j2_112-116.xml": true,
	"gameai.json":        true, "gameai-state.json": true,
}

// controlDirs 整个目录都不采用的（小写、斜杠结尾）：MineDeck 和自家模组之间的端口、令牌（config/minedeck），
// 以及 MineDeck 给配置文件留的备份（.minedeck）。令牌是原服主那台电脑上的，接手的人要用这些功能时 MineDeck 会重新生成。
var controlDirs = []string{"config/minedeck/", ".minedeck/"}

// isControl rel（相对实例目录，本机分隔符）是不是不采用的控制文件。导出时同样不带这些。
func isControl(rel string) bool {
	low := strings.ToLower(filepath.ToSlash(rel))
	if controlRoot[low] {
		return true
	}
	for _, d := range controlDirs {
		if strings.HasPrefix(low, d) {
			return true
		}
	}
	return false
}

// payloadPath 校验一条包内路径，返回前缀内的相对路径（本机分隔符）。ok=false 表示不属于实例文件（前缀之外、目录项），跳过即可；
// 报错表示路径可疑，整个包都不要。
// 按 Windows 的规矩查：filepath.IsLocal 拒绝绝对路径、盘符、往上走的 ..、保留设备名（NUL、COM1…）和带冒号的名字
// （a.txt:x 会写进备用数据流）；某一段以点或空格结尾的也拒绝——Windows 会把结尾的点和空格吃掉，"run.bat." 落下来就是 run.bat。
func payloadPath(f *zip.File) (rel string, ok bool, err error) {
	name := strings.ReplaceAll(f.Name, "\\", "/")
	in, found := strings.CutPrefix(name, PayloadPrefix)
	if !found {
		return "", false, nil
	}
	if f.FileInfo().IsDir() || strings.HasSuffix(in, "/") {
		return "", false, nil // 目录由文件的路径带出来（别的压缩工具重新打过的包会有目录项）
	}
	if in == "" {
		return "", false, nil
	}
	local := filepath.FromSlash(in)
	if !filepath.IsLocal(local) || local != filepath.Clean(local) {
		return "", false, fmt.Errorf("交接包里有可疑路径: %s", f.Name)
	}
	for _, seg := range strings.Split(in, "/") {
		if strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") {
			return "", false, fmt.Errorf("交接包里有可疑路径: %s", f.Name)
		}
	}
	if !f.Mode().IsRegular() {
		return "", false, fmt.Errorf("交接包里有不是普通文件的条目: %s", f.Name)
	}
	return local, true, nil
}

// extractPrefix 解包 zip 里 PayloadPrefix 前缀下的全部文件到 destDir（严格 zip-slip）。
// manifest.json 和前缀外的条目一概忽略，控制文件（isControl）不采用。
func extractPrefix(ctx context.Context, src, destDir string, opts extractOpts,
	progress func(done, total int64), fileCount func(n int, done, total int64)) (extractResult, error) {
	var res extractResult
	zr, err := zip.OpenReader(src)
	if err != nil {
		return res, fmt.Errorf("打不开交接包（%w）", err)
	}
	defer zr.Close()
	type entry struct {
		f    *zip.File
		name string // 前缀内的相对路径（本机分隔符）
		size int64
	}
	var ents []entry
	var total int64
	for _, f := range zr.File {
		if f.Name == manifestName {
			continue
		}
		rel, ok, err := payloadPath(f)
		if err != nil {
			return res, err
		}
		if !ok {
			continue
		}
		if isControl(rel) {
			res.control = append(res.control, filepath.ToSlash(rel))
			continue
		}
		if opts.slim && coreTurf(rel) {
			res.kept = append(res.kept, filepath.ToSlash(rel))
			continue
		}
		size := int64(f.UncompressedSize64)
		if size < 0 || total+size < total {
			return res, fmt.Errorf("交接包里的文件大小不对: %s", f.Name)
		}
		ents = append(ents, entry{f: f, name: rel, size: size})
		total += size
	}
	// 总量两道关：和清单对账（申报的比清单多出一截，是有人改过的包）；再看盘上装不装得下。
	// 单个文件还有按申报体积的限流（见下），所以实际写盘不会超过这里算出来的 total。
	if opts.declared > 0 && total > opts.declared+opts.declared/8+(64<<20) {
		return res, fmt.Errorf("交接包解开后有 %s，清单里写的只有 %s，这个包被改过或者坏了", fmtBytes(total), fmtBytes(opts.declared))
	}
	if opts.free != nil {
		if free, err := opts.free(); err == nil && uint64(total)+(256<<20) > free {
			return res, fmt.Errorf("磁盘空间不够：交接包解开要 %s，存放位置所在的盘只剩 %s", fmtBytes(total), fmtBytes(int64(free)))
		}
	}
	cleanDest := filepath.Clean(destDir)
	var done int64
	var n int
	buf := make([]byte, 512<<10)
	cancelled := func() error {
		if ctx == nil {
			return nil
		}
		return ctx.Err()
	}
	for _, e := range ents {
		if err := cancelled(); err != nil {
			return res, err
		}
		target := filepath.Join(cleanDest, e.name)
		if target == cleanDest || !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) {
			return res, fmt.Errorf("交接包里有越界路径: %s", e.f.Name)
		}
		if opts.slim {
			if _, err := os.Lstat(target); err == nil {
				res.kept = append(res.kept, filepath.ToSlash(e.name))
				done += e.size
				progress(done, total)
				continue
			}
		}
		rc, err := e.f.Open()
		if err != nil {
			return res, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			rc.Close()
			return res, err
		}
		w, err := os.Create(target)
		if err != nil {
			rc.Close()
			return res, err
		}
		// 解压炸弹防线：申报多大就最多解多大——超过申报体积立刻失败，不写到整盘才回头对账
		k, cerr := io.CopyBuffer(w, io.LimitReader(rc, e.size+1), buf)
		if werr := w.Close(); cerr == nil {
			cerr = werr
		}
		rc.Close()
		if cerr != nil {
			return res, fmt.Errorf("解包 %s 失败: %w", e.name, cerr)
		}
		if k != e.size {
			return res, fmt.Errorf("解包 %s 后不完整（写到 %d 字节，包里记的是 %d）", e.name, k, e.size)
		}
		done += e.size
		res.bytes += e.size
		progress(done, total)
		n++
		if n%2048 == 0 {
			fileCount(n, done, total)
		}
	}
	fileCount(n, done, total)
	return res, nil
}

func modTime(path string) time.Time {
	if st, err := os.Stat(path); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}
