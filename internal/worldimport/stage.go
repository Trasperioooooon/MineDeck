package worldimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"minedeck/internal/winname"
)

// 暂存：把候选地图的文件按映射复制到目标实例的 .minedeck/world-import/<编号>/new/ 下面（结构和实例目录一样），
// 来源只读、流式复制、按实际字节数限流。复制完核对来源有没有在这期间变过。

// stagedFile 一个要复制的文件：来源里的条目 → 实例里的相对路径。
type stagedFile struct {
	e  *entry
	to string // 相对实例目录（斜杠分隔）
}

// resolveMoves 按映射给候选的每个文件算出落点。session.lock 不带（由游戏重新建）；drop 里的不带。
func resolveMoves(info *candInfo, mp mapping) ([]stagedFile, []string, error) {
	var out []stagedFile
	var dropped []string
	seen := map[string]string{}
	for k := range info.files {
		e := &info.files[k]
		if strings.EqualFold(pathBase(e.rel), "session.lock") {
			continue
		}
		var best *Move
		for m := range mp.moves {
			mv := &mp.moves[m]
			if mv.From == "" || strings.EqualFold(e.rel, mv.From) || strings.HasPrefix(winname.Fold(e.rel), winname.Fold(mv.From)+"/") {
				if best == nil || len(mv.From) > len(best.From) {
					best = mv
				}
			}
		}
		dropHit := ""
		for _, d := range mp.drop {
			if strings.EqualFold(e.rel, d) || strings.HasPrefix(winname.Fold(e.rel), winname.Fold(d)+"/") {
				dropHit = d
			}
		}
		if best == nil || dropHit != "" && len(dropHit) >= len(best.From) {
			dropped = append(dropped, e.rel)
			continue
		}
		to := best.To + "/" + relRest(e.rel, best.From) // 按段数切，见 relRest
		if best.From != "" && winname.Fold(e.rel) == winname.Fold(best.From) {
			to = best.To
		}
		k := winname.Fold(to)
		if prev, dup := seen[k]; dup {
			return nil, nil, fmt.Errorf("两个文件会落到同一个位置（%s 和 %s → %s），没法自动整理", prev, e.rel, to)
		}
		seen[k] = e.rel
		out = append(out, stagedFile{e: e, to: to})
	}
	return out, dropped, nil
}

func pathBase(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[i+1:]
	}
	return rel
}

// stageFiles 复制到 stageRoot（= <事务目录>/new）。progress(已复制字节, 总字节)。
func stageFiles(ctx context.Context, src *source, files []stagedFile, stageRoot string, progress func(done, total int64)) error {
	var total int64
	for _, f := range files {
		total += f.e.size
	}
	buf := make([]byte, 1<<20)
	var done int64
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		dst := filepath.Join(stageRoot, filepath.FromSlash(f.to))
		if !strings.HasPrefix(dst, filepath.Clean(stageRoot)+string(os.PathSeparator)) {
			return fmt.Errorf("落点越界：%s", f.to)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return diskErr(err)
		}
		if err := copyEntry(ctx, src, f.e, dst, buf); err != nil {
			return err
		}
		done += f.e.size
		if progress != nil {
			progress(done, total)
		}
	}
	return nil
}

func copyEntry(ctx context.Context, src *source, e *entry, dst string, buf []byte) error {
	rc, err := src.open(e)
	if err != nil {
		return fmt.Errorf("读不了来源里的 %s：%w", e.rel, err)
	}
	defer rc.Close()
	w, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return diskErr(err)
	}
	// 按申报的大小最多读一个字节多：ZIP 里申报的大小不可信（炸弹），文件夹里的文件在复制时还可能在变
	n, cerr := io.CopyBuffer(w, io.LimitReader(ctxReader{ctx, rc}, e.size+1), buf)
	if werr := w.Close(); cerr == nil {
		cerr = werr
	}
	if cerr != nil {
		if errors.Is(cerr, context.Canceled) {
			return cerr
		}
		if src.kind == "zip" {
			return fmt.Errorf("压缩包里的 %s 解不开（可能下载不完整、校验不对），没有替换任何文件：%w", e.rel, diskErr(cerr))
		}
		return fmt.Errorf("复制 %s 失败：%w", e.rel, diskErr(cerr))
	}
	if n != e.size {
		if src.kind == "zip" {
			return fmt.Errorf("压缩包里的 %s 实际大小和记录的不一样，这个包被改过或者坏了", e.rel)
		}
		return fmt.Errorf("来源里的 %s 在复制期间变了。请先关掉正在使用这张地图的程序，再重新检测", e.rel)
	}
	if src.kind == "folder" {
		// 复制完再看一眼：大小、修改时间变了就是有人在写
		if st, err := os.Stat(filepath.Join(src.root, filepath.FromSlash(e.rel))); err != nil || st.Size() != e.size || !st.ModTime().Equal(e.mod) {
			return fmt.Errorf("来源里的 %s 在复制期间变了。请先关掉正在使用这张地图的程序，再重新检测", e.rel)
		}
	}
	if !e.mod.IsZero() {
		_ = os.Chtimes(dst, e.mod, e.mod)
	}
	return nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// diskErr 盘满时说人话。
func diskErr(err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) && (errno == syscall.Errno(windows.ERROR_DISK_FULL) || errno == syscall.Errno(windows.ERROR_HANDLE_DISK_FULL)) {
		return fmt.Errorf("磁盘空间不够，没有替换任何文件（%w）", err)
	}
	return err
}
