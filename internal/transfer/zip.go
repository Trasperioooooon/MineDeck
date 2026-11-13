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

	"minedeck/internal/inst"
	"minedeck/internal/modinfo"
	"minedeck/internal/winname"
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

// maxEntries 一个交接包最多有多少个文件。真实的服务器（世界、配置、模组、插件数据）到不了这个数的十分之一。
const maxEntries = 1_000_000

// extractOpts 解包时的几道限制（交接包是别人给的文件，按恶意输入对待）。
type extractOpts struct {
	// slim 瘦身模式的包（清单里 full=false）：服务端核心是导入时刚下载、校验过的，包里不该有、有也不采用。
	//   - 目标位置已经有文件时不覆盖：解包之前目录里只有下载的核心和安装器装出来的依赖库。
	//   - 核心的地盘上的东西不解：根目录的 jar，和 libraries、versions、cache、.fabric 这几个目录。
	//     有的核心（Paper、Fabric、原版）第一次启动才把依赖库解到这些目录里，那时候目录里先有一份别人放的就不好说了。
	//     MineDeck 自己导出的瘦身包里没有这些（见 export.go），所以正常的包不受影响。
	slim bool
	// declared、declaredFiles 清单里写的字节总数和文件数（manifest.counts）；包里实际的比它大出一截就拒绝。0 = 不比。
	declared      int64
	declaredFiles int64
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
	// 第一段像 8.3 短名的（LIBRAR~1）也算：Forge、NeoForge 的 libraries 在解包前就存在，
	// Windows 给它起过短名，包里用短名写的路径会落进同一个目录。正常导出的包里顶层没有这种名字。
	return shortNameLike(first)
}

// shortNameLike 名字像不像 Windows 自动生成的 8.3 短名：波浪号后面跟着数字。
func shortNameLike(seg string) bool {
	i := strings.IndexByte(seg, '~')
	return i > 0 && i+1 < len(seg) && seg[i+1] >= '0' && seg[i+1] <= '9'
}

// payloadPath 校验一条包内路径，返回前缀内的相对路径（本机分隔符）。ok=false 表示不属于实例文件（前缀之外、目录项），跳过即可；
// 报错表示路径可疑，整个包都不要。
// 按 Windows 的规矩查：filepath.IsLocal 拒绝绝对路径、盘符、往上走的 ..；每一段还要能原样写到 Windows 上（winname.PathOK）：
// 保留设备名不管带不带扩展名都拒绝（NUL、COM1、CON.txt，不看这台 Windows 把它当不当设备），带冒号的拒绝
// （a.txt:x 会写进备用数据流），以点或空格结尾的也拒绝——Windows 会把结尾的点和空格吃掉，"run.bat." 落下来就是 run.bat。
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
	if !filepath.IsLocal(local) || local != filepath.Clean(local) || !winname.PathOK(in) {
		if onlyBadNames(in) {
			// 路径本身没问题，只是某一段的名字在 Windows 上不能用：设备名、结尾是点或空格这类名字在 Windows 11 上建得出来，
			// 3.1.0 及以前在那种电脑上导出的包里可能有，不一定是包被改过，说清怎么办
			return "", false, fmt.Errorf("交接包里的 %s 在 Windows 上不能当文件名（CON、AUX 这类设备名，或者结尾是点、空格），"+
				"整个包没有导入。在原来的电脑上把它改个名字，再导出一次就行", f.Name)
		}
		return "", false, fmt.Errorf("交接包里有可疑路径: %s", f.Name)
	}
	if !f.Mode().IsRegular() {
		return "", false, fmt.Errorf("交接包里有不是普通文件的条目: %s", f.Name)
	}
	return local, true, nil
}

// onlyBadNames 路径的样子没问题（不往上走、不是绝对路径、没有盘符和冒号、没有空段），毛病只在某一段的名字本身
// （设备名、结尾是点或空格、带 Windows 不收的字符）。用来决定报哪一种错。
func onlyBadNames(rel string) bool {
	if strings.HasPrefix(rel, "/") || strings.Contains(rel, ":") {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// extractPrefix 解包 zip 里 PayloadPrefix 前缀下的全部文件到 destDir（严格 zip-slip）。
// manifest.json 和前缀外的条目一概忽略，控制文件（inst.IsControlFile）不采用。
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
	seen := map[string]int{} // 要解出来的文件：不分大小写的路径 → 在 ents 里的位置
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
		if inst.IsControlFile(rel) {
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
		// 只差大小写（或者一模一样）的两个文件在 Windows 上是同一个，后一个会把前一个盖掉（v3.1.0 正式版前检查 C-2e）。
		// 内容一样的是同一个文件打了两遍（3.1.0 及以前导出完整包时，启动 jar 叫 Server.jar 就会和 server.jar 各打一份），
		// 跳过后一个；内容不一样的是被改过的包，和可疑路径一样整包不要
		k := winname.Fold(filepath.ToSlash(rel))
		if j, dup := seen[k]; dup {
			if p := ents[j]; p.f.CRC32 == f.CRC32 && p.size == size {
				continue
			}
			return res, fmt.Errorf("交接包里的 %s 和 %s 在 Windows 上是同一个文件，内容却不一样，这个包被改过或者坏了",
				filepath.ToSlash(ents[j].name), filepath.ToSlash(rel))
		}
		seen[k] = len(ents)
		ents = append(ents, entry{f: f, name: rel, size: size})
		total += size
	}
	// 总量两道关：和清单对账（申报的比清单多出一截，是有人改过的包）；再看盘上装不装得下。
	// 单个文件还有按申报体积的限流（见下），所以实际写盘不会超过这里算出来的 total。
	// 清单里的数也是包里来的，可以写得很大：比较时用减法，不拿它做加法（加出来会溢出）。
	if opts.declared > 0 && total > opts.declared && total-opts.declared > opts.declared/8+(64<<20) {
		return res, fmt.Errorf("交接包解开后有 %s，清单里写的只有 %s，这个包被改过或者坏了", fmtBytes(total), fmtBytes(opts.declared))
	}
	// 文件数也对一遍：几十万个空文件不占字节数，上面两道关都拦不住，却能把导入拖上几十分钟。
	// 清单里的数是包自己写的，所以另有一个不看清单的上限。
	if n := int64(len(ents)); opts.declaredFiles > 0 && n > opts.declaredFiles && n-opts.declaredFiles > opts.declaredFiles/8+1024 {
		return res, fmt.Errorf("交接包里有 %d 个文件，清单里写的只有 %d 个，这个包被改过或者坏了", n, opts.declaredFiles)
	}
	if len(ents) > maxEntries {
		return res, fmt.Errorf("交接包里有 %d 个文件，超过了一次能导入的上限（%d 个）", len(ents), maxEntries)
	}
	if opts.free != nil {
		// 每个文件在盘上至少占一条文件记录、内容按簇取整：按每个文件多算 4 KB 估
		if free, err := opts.free(); err == nil && uint64(total)+uint64(len(ents))*4096+(256<<20) > free {
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
