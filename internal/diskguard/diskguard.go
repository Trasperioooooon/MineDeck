// Package diskguard coordinates MineDeck's bulk writes on the same volume.
// The lease covers a whole copy/archive, including free-space checks during writes.
package diskguard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows"
)

const Reserve int64 = 256 << 20

var ErrUnknown = errors.New("查不到磁盘剩余空间，暂未继续写入")

type NoSpace struct{ Need, Free int64 }

func (e *NoSpace) Error() string {
	return fmt.Sprintf("磁盘空间不够（需要保留或写入 %.2f GiB，只剩 %.2f GiB）", float64(e.Need)/(1<<30), float64(e.Free)/(1<<30))
}

type Options struct {
	Need    int64
	Reserve int64
	// AllowUnknown 查不到这个盘还剩多少时照做（查得到又不够时照拦）。两种情况会设它：
	// 地图导入的计划里用户明确确认过「查不到余量」；以及加这道检查之前就不查余量的那些操作
	// （下载、备份、还原、解包、交接包）：有的盘就是查不了，不能因为多了一道检查，这些原来能做的事在那种盘上全都做不了
	// （2026-11-14 第三轮修复的复审，发现 5）。接收上传和没确认过的地图暂存不设，查不到就停。
	AllowUnknown bool
	// Shared 不占这个盘的独占名额，只做余量检查（开始时一次，写的过程中每隔 CheckEvery 一次）。
	// 给速度由网络决定的写入用（下载、接收上传）：它们占着独占名额的话，一个慢连接会让同一个盘上所有
	// 要写盘的操作等它传完，几路并发的下载也变成一个一个来（2026-11-14 第三轮修复的复审，发现 2）。
	// 本机上的大批量复制、压缩、解包仍然用独占名额。
	Shared     bool
	CheckEvery int64
	OnWait     func()
	Query      func(string) (int64, error) // nil uses the Windows volume query; injectable for deterministic tests.
}

type gate struct {
	slot chan struct{}
	refs int
}

var volumes = struct {
	sync.Mutex
	gates map[uint32]*gate
}{gates: map[uint32]*gate{}}

type Lease struct {
	ctx   context.Context
	dir   string
	opt   Options
	key   uint32
	g     *gate
	once  sync.Once
	since int64
}

// Acquire resolves the actual Windows volume via a handle, so junctions, drive
// aliases and case variants participate in the same queue. Different volumes
// can make progress independently. Callers must not acquire a nested lease.
// With Options.Shared the call never queues: it only performs the free-space checks.
func Acquire(ctx context.Context, path string, opt Options) (*Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, key, err := volumeOf(path)
	if err != nil {
		return nil, fmt.Errorf("无法确认写入磁盘：%w", err)
	}
	if opt.Reserve <= 0 {
		opt.Reserve = Reserve
	}
	if opt.CheckEvery <= 0 {
		opt.CheckEvery = 64 << 20
	}
	if opt.Query == nil {
		opt.Query = Free
	}
	if opt.Shared {
		l := &Lease{ctx: ctx, dir: dir, opt: opt, key: key}
		if err := l.Check(opt.Need); err != nil {
			return nil, err
		}
		return l, nil
	}
	volumes.Lock()
	g := volumes.gates[key]
	if g == nil {
		g = &gate{slot: make(chan struct{}, 1)}
		volumes.gates[key] = g
	}
	g.refs++
	volumes.Unlock()
	drop := func() {
		volumes.Lock()
		g.refs--
		if g.refs == 0 {
			delete(volumes.gates, key)
		}
		volumes.Unlock()
	}
	select {
	case g.slot <- struct{}{}:
	default:
		if opt.OnWait != nil {
			opt.OnWait()
		}
		select {
		case g.slot <- struct{}{}:
		case <-ctx.Done():
			drop()
			return nil, ctx.Err()
		}
	}
	l := &Lease{ctx: ctx, dir: dir, opt: opt, key: key, g: g}
	if err := l.Check(opt.Need); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

func (l *Lease) Close() error {
	if l.g == nil { // Shared：没有占名额
		return nil
	}
	l.once.Do(func() {
		<-l.g.slot
		volumes.Lock()
		l.g.refs--
		if l.g.refs == 0 {
			delete(volumes.gates, l.key)
		}
		volumes.Unlock()
	})
	return nil
}

func (l *Lease) Check(bytes int64) error {
	if err := l.ctx.Err(); err != nil {
		return err
	}
	free, err := l.opt.Query(l.dir)
	if err != nil {
		if l.opt.AllowUnknown {
			return nil
		}
		return fmt.Errorf("%w：%v", ErrUnknown, err)
	}
	need := int64(math.MaxInt64)
	if bytes >= 0 && bytes <= math.MaxInt64-l.opt.Reserve {
		need = bytes + l.opt.Reserve
	}
	if free < need {
		return &NoSpace{Need: need, Free: free}
	}
	return nil
}

// Writer checks at most CheckEvery bytes apart, even when a caller writes a
// single very large buffer. Use one Writer at a time per lease.
func (l *Lease) Writer(w io.Writer) io.Writer { return &writer{l, w} }

// Reader protects APIs such as ParseMultipartForm that write their input to
// private temporary files internally. Do not use concurrently with Writer.
func (l *Lease) Reader(r io.Reader) io.Reader { return &reader{l, r} }

type reader struct {
	l *Lease
	r io.Reader
}

func (r *reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := r.l.ctx.Err(); err != nil {
		return 0, err
	}
	if r.l.since == 0 {
		if err := r.l.Check(0); err != nil {
			return 0, err
		}
	}
	size := min(int64(len(p)), r.l.opt.CheckEvery-r.l.since)
	n, err := r.r.Read(p[:int(size)])
	r.l.since = (r.l.since + int64(n)) % r.l.opt.CheckEvery
	if e := r.l.ctx.Err(); e != nil {
		return n, e
	}
	return n, err
}

type writer struct {
	l *Lease
	w io.Writer
}

func (w *writer) Write(p []byte) (int, error) {
	n := 0
	for len(p) > 0 {
		if err := w.l.ctx.Err(); err != nil {
			return n, err
		}
		if w.l.since == 0 {
			if err := w.l.Check(0); err != nil {
				return n, err
			}
		}
		size := min(int64(len(p)), w.l.opt.CheckEvery-w.l.since)
		written, err := w.w.Write(p[:int(size)])
		n += written
		w.l.since = (w.l.since + int64(written)) % w.l.opt.CheckEvery
		p = p[written:]
		if err != nil {
			return n, err
		}
		if written != int(size) {
			return n, io.ErrShortWrite
		}
	}
	return n, nil
}

func Free(path string) (int64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, err
	}
	if free > math.MaxInt64 {
		return math.MaxInt64, nil
	}
	return int64(free), nil
}

func volumeOf(path string) (string, uint32, error) {
	dir, err := filepath.Abs(path)
	if err != nil {
		return "", 0, err
	}
	for {
		st, err := os.Stat(dir)
		if err == nil {
			if !st.IsDir() {
				dir = filepath.Dir(dir)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(dir) == dir {
			return "", 0, err
		}
		dir = filepath.Dir(dir)
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return "", 0, err
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", 0, err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return "", 0, err
	}
	// A serial collision only queues unrelated volumes together; it cannot permit
	// two writers on one volume to bypass the gate.
	return dir, info.VolumeSerialNumber, nil
}
