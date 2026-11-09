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

// extractPrefix 解包 zip 里 PayloadPrefix 前缀下的全部文件到 destDir（严格 zip-slip）。
// manifest.json 和前缀外的条目一概忽略。返回解出的字节数。
func extractPrefix(ctx context.Context, src, destDir string,
	progress func(done, total int64), fileCount func(n int, done, total int64)) (int64, error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return 0, fmt.Errorf("打不开交接包（%w）", err)
	}
	defer zr.Close()
	type entry struct {
		f    *zip.File
		name string // 前缀内的相对路径（斜杠）
		size int64
	}
	var ents []entry
	var total int64
	for _, f := range zr.File {
		if f.Name == manifestName {
			continue
		}
		name := strings.ReplaceAll(f.Name, "\\", "/")
		rel, ok := strings.CutPrefix(name, PayloadPrefix)
		if !ok || rel == "" {
			continue
		}
		if strings.HasPrefix(rel, "./") {
			rel = strings.TrimPrefix(rel, "./")
		}
		if rel != filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel))) {
			return 0, fmt.Errorf("交接包里有可疑路径: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			continue // 目录由文件的路径带出来
		}
		ents = append(ents, entry{f: f, name: rel, size: int64(f.UncompressedSize64)})
		total += int64(f.UncompressedSize64)
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
			return 0, err
		}
		target := filepath.Join(cleanDest, filepath.FromSlash(e.name))
		if target == cleanDest || !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) {
			return 0, fmt.Errorf("交接包里有越界路径: %s", e.f.Name)
		}
		rc, err := e.f.Open()
		if err != nil {
			return 0, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			rc.Close()
			return 0, err
		}
		w, err := os.Create(target)
		if err != nil {
			rc.Close()
			return 0, err
		}
		// 解压炸弹防线：申报多大就最多解多大——超过申报体积立刻失败，不写到整盘才回头对账
		k, cerr := io.CopyBuffer(w, io.LimitReader(rc, e.size+1), buf)
		if werr := w.Close(); cerr == nil {
			cerr = werr
		}
		rc.Close()
		if cerr != nil {
			return 0, fmt.Errorf("解包 %s 失败: %w", e.name, cerr)
		}
		if k != e.size {
			return 0, fmt.Errorf("解包 %s 后不完整（写到 %d 字节，包里记的是 %d）", e.name, k, e.size)
		}
		done += e.size
		progress(done, total)
		n++
		if n%2048 == 0 {
			fileCount(n, done, total)
		}
	}
	fileCount(n, done, total)
	return done, nil
}

func modTime(path string) time.Time {
	if st, err := os.Stat(path); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}
