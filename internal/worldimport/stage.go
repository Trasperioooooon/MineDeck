package worldimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"minedeck/internal/app"
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

// stageReserve 暂存时给这个盘留的余量：复制每个文件之前，剩余空间要够放下它再多出这么多，不然就停。
// 盘写满不只是这次导入失败：同一个盘上别的服务器正在存的世界、日志也会跟着写不进去。做成变量是为了测试能调。
var stageReserve int64 = 256 << 20

// diskFree 查一个路径所在的盘还剩多少。测试里换掉它。
var diskFree = func(path string) (int64, error) {
	n, err := app.DiskFree(path)
	if err != nil {
		return 0, err
	}
	if n > math.MaxInt64 {
		return math.MaxInt64, nil
	}
	return int64(n), nil
}

// needWithReserve 要 n 字节、再留出余量，一共要多少；加法溢出时按最大算（不会变成负数）。
func needWithReserve(n, reserve int64) int64 {
	if n < 0 || n > math.MaxInt64-reserve {
		return math.MaxInt64
	}
	return n + reserve
}

// errNoSpace 盘的余量不够。
type errNoSpace struct{ need, free int64 }

func (e *errNoSpace) Error() string {
	return fmt.Sprintf("磁盘空间不够（还要 %s，只剩 %s），没有替换任何文件", fmtBytes(e.need), fmtBytes(e.free))
}

// checkSpace dir 所在的盘够不够放下 n 字节再留出余量。查不到剩余空间时不拦（返回 nil）：有的盘就是查不了，
// 真写满了复制那一步会报错、清掉暂存。
func checkSpace(dir string, n int64) error {
	free, err := diskFree(dir)
	if err != nil {
		return nil
	}
	if need := needWithReserve(n, stageReserve); need > free {
		return &errNoSpace{need: need, free: free}
	}
	return nil
}

// stageFiles 复制到 stageRoot（= <事务目录>/new）。progress(已复制字节, 总字节)。
func stageFiles(ctx context.Context, src *source, files []stagedFile, stageRoot string, progress func(done, total int64)) error {
	var total int64
	for _, f := range files {
		if f.e.size < 0 || f.e.size > math.MaxInt64-total {
			return fmt.Errorf("地图里记的文件大小不对，没有替换任何文件")
		}
		total += f.e.size
	}
	// 开始之前按整张地图再查一遍余量：做计划时查过，但从预览到点确认可能隔了很久，别的导入也可能占掉了空间
	if err := os.MkdirAll(stageRoot, 0o755); err != nil {
		return diskErr(err)
	}
	if err := checkSpace(stageRoot, total); err != nil {
		return err
	}
	buf := make([]byte, 1<<20)
	var done int64
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		// 每个文件之前再看一眼：压缩包里申报的大小不可信，单个文件最多写到它申报的那么大，
		// 所以「剩余空间放得下它再多出余量」成立，这个文件就写不满盘
		if err := checkSpace(stageRoot, f.e.size); err != nil {
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

// stagedDirs 复制完以后，暂存区里要搬进服务器的世界目录（主目录，以及旧 Bukkit 布局的 _nether、_the_end）。
//
// 搬哪些不能看暂存区里「现在有什么」：以前是逐个 exists，某个维度目录一时看不了就被当成没有，不进搬入的清单，
// 而旧的那个维度照样被移走，主世界的核对又通过，结果是悄悄少装一个维度（2026-11-14 Codex 第二轮复审阻挡项 2）。
// 现在从「要复制的文件清单」推出该有哪些目录，再逐个确认它们真的在：
//   - 每个要复制的文件都必须落在某个世界目录下面，不然它复制了也不会被搬进去；
//   - 该有的目录看不了、或者不在，都报错；不该有的目录在，也报错。
func stagedDirs(stageRoot, level string, files []stagedFile) ([]string, error) {
	names := worldDirNames(level)
	want := map[string]bool{}
	for _, f := range files {
		to := strings.ToLower(filepath.ToSlash(f.to))
		hit := false
		for _, n := range names {
			if ln := strings.ToLower(n); to == ln || strings.HasPrefix(to, ln+"/") {
				want[n], hit = true, true
				break
			}
		}
		if !hit {
			return nil, fmt.Errorf("要复制的「%s」不在任何一个世界文件夹下面，没有替换任何文件", f.to)
		}
	}
	var out []string
	for _, n := range names {
		there, err := present(filepath.Join(stageRoot, filepath.FromSlash(n)))
		switch {
		case err != nil:
			return nil, fmt.Errorf("%w，没有替换任何文件", err)
		case want[n] && !there:
			return nil, fmt.Errorf("暂存区里少了「%s」，没有替换任何文件", n)
		case !want[n] && there:
			return nil, fmt.Errorf("暂存区里多出了「%s」，没有替换任何文件", n)
		case there:
			out = append(out, n)
		}
	}
	return out, nil
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
