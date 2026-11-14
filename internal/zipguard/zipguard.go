// Package zipguard 打开别人给的 ZIP：在标准库动手之前，先看包里申报了多少个文件、文件清单有多大，超了就不打开。
//
// 为什么要自己先看一眼（2026-11-14 Codex 第二轮复审阻挡项 4）：Go 的 archive/zip 打开一个包时，
//  1. 先按包尾「结束记录」里申报的条目数给文件表留位置（一条一个指针）。它只拿整个文件的大小去核这个数，
//     所以一个几个 GB、清单其实是空的文件，可以让它一次分配几个 GB 的内存；
//  2. 再把清单里的每一条读进内存，一直读到不像清单的地方才停，不看申报了多少条。
//
// 条目数的上限要是等它打开完再比，这两步都已经发生了。这里的做法：
//   - 照标准库的找法读出结束记录（和 ZIP64 的结束记录），申报的条目数或清单大小超了上限，直接拒绝，不调标准库；
//   - 读不出结束记录的也直接拒绝（标准库同样打不开它）；
//   - 过了这一关再交给标准库，同时数着它打开时读了多少字节，超过清单大小的上限就停。这是第二层：
//     包里申报的数可以是假的，实际的清单可以比申报的长。
package zipguard

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

// Limits 一个包最多申报多少个文件、文件清单最多多少字节。
type Limits struct {
	MaxEntries  int
	MaxDirBytes int64
}

// Default 一般的上限：一百万个文件，清单 64 MiB（清单里一条最少 46 字节，64 MiB 最多一百四十万条）。
var Default = Limits{MaxEntries: 1_000_000, MaxDirBytes: 64 << 20}

// ErrTooLarge 包里的文件太多、清单太大。
var ErrTooLarge = errors.New("压缩包里的文件太多了")

// ReadCloser 同 zip.ReadCloser：File 是包里的文件，用完 Close。
type ReadCloser struct {
	*zip.Reader
	f *os.File
}

// Close 关掉底下的文件。
func (r *ReadCloser) Close() error { return r.f.Close() }

// Open 像 zip.OpenReader 那样打开 path，但申报的条目数、清单大小超过 lim 时返回 ErrTooLarge（errors.Is 认得出）。
func Open(path string, lim Limits) (*ReadCloser, error) {
	return OpenContext(context.Background(), path, lim)
}

// OpenContext 在每次读取前检查取消；不能打断已经阻塞在操作系统里的读取。
func OpenContext(ctx context.Context, path string, lim Limits) (*ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	zr, err := NewReaderContext(ctx, f, st.Size(), lim)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &ReadCloser{Reader: zr, f: f}, nil
}

// NewReader 同 zip.NewReader，带上面说的两层检查。
func NewReader(r io.ReaderAt, size int64, lim Limits) (*zip.Reader, error) {
	return NewReaderContext(context.Background(), r, size, lim)
}

func NewReaderContext(ctx context.Context, r io.ReaderAt, size int64, lim Limits) (*zip.Reader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r = contextReaderAt{ctx: ctx, r: r}
	records, dirSize, err := Declared(r, size)
	if err != nil {
		return nil, err
	}
	if lim.MaxEntries > 0 && records > uint64(lim.MaxEntries) {
		return nil, fmt.Errorf("%w（包里写着有 %d 个，最多 %d 个）", ErrTooLarge, records, lim.MaxEntries)
	}
	if lim.MaxDirBytes > 0 && dirSize > uint64(lim.MaxDirBytes) {
		return nil, fmt.Errorf("%w（文件清单有 %d 字节，最多 %d 字节）", ErrTooLarge, dirSize, lim.MaxDirBytes)
	}
	b := &budgetReaderAt{r: r}
	b.left.Store(lim.MaxDirBytes)
	b.limited.Store(lim.MaxDirBytes > 0)
	zr, err := zip.NewReader(b, size)
	b.limited.Store(false) // 打开完成：之后读文件内容不受清单预算的限制
	if b.over.Load() {
		return nil, fmt.Errorf("%w（文件清单超过 %d 字节）", ErrTooLarge, lim.MaxDirBytes)
	}
	if err != nil {
		return nil, err
	}
	if lim.MaxEntries > 0 && len(zr.File) > lim.MaxEntries {
		return nil, fmt.Errorf("%w（实际有 %d 个，最多 %d 个）", ErrTooLarge, len(zr.File), lim.MaxEntries)
	}
	return zr, nil
}

// budgetReaderAt 打开的这段时间里数着读了多少字节，超过预算就报错；打开完成以后放开。
type budgetReaderAt struct {
	r       io.ReaderAt
	limited atomic.Bool
	left    atomic.Int64
	over    atomic.Bool
}

var errBudget = errors.New("zipguard: 读清单的预算用完了")

func (b *budgetReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if b.limited.Load() {
		if b.left.Add(-int64(len(p))) < 0 {
			b.over.Store(true)
			return 0, errBudget
		}
	}
	return b.r.ReadAt(p, off)
}

// 下面三个函数照 Go 标准库 archive/zip 的 readDirectoryEnd、findDirectory64End、readDirectory64End、
// findSignatureInBlock 写（Go 1.27）：找法必须和它一样，不然会出现「这里没找到、它找到了」的包。
// 有一处刻意比它严：这里读不出结束记录就拒绝，不再交给标准库。

const (
	directoryEndLen   = 22 // 结束记录的固定部分，后面跟注释
	directory64LocLen = 20 // ZIP64 定位符
	directory64EndLen = 56 // ZIP64 结束记录的固定部分
)

// Declared 读出包尾申报的条目数和文件清单的大小。读不出来返回 zip.ErrFormat（或者读盘的错误）。
func Declared(r io.ReaderAt, size int64) (records, dirSize uint64, err error) {
	if size < directoryEndLen {
		return 0, 0, zip.ErrFormat
	}
	// 先在最后 1 KB 里找结束记录的标记，找不到再在最后 65 KB 里找
	var buf []byte
	var endOffset int64
	for i, bLen := range []int64{1024, 65 * 1024} {
		if bLen > size {
			bLen = size
		}
		buf = make([]byte, int(bLen))
		if _, err := r.ReadAt(buf, size-bLen); err != nil && err != io.EOF {
			return 0, 0, err
		}
		if p := findSignatureInBlock(buf); p >= 0 {
			buf = buf[p:]
			endOffset = size - bLen + int64(p)
			break
		}
		if i == 1 || bLen == size {
			return 0, 0, zip.ErrFormat
		}
	}
	records = uint64(binary.LittleEndian.Uint16(buf[10:]))
	dirSize = uint64(binary.LittleEndian.Uint32(buf[12:]))
	dirOffset := uint64(binary.LittleEndian.Uint32(buf[16:]))
	// 这几个值顶到头，说明可能是 ZIP64：真正的数在前面的 ZIP64 结束记录里。
	// 定位符不对（没有、不是它、盘号不对）的时候标准库会当它不是 ZIP64、照用上面的数，这里也一样
	if records == 0xffff || dirSize == 0xffffffff || dirOffset == 0xffffffff {
		p, err := findDirectory64End(r, endOffset)
		if err == nil && p >= 0 {
			records, dirSize, err = readDirectory64End(r, p)
		}
		if err != nil {
			return 0, 0, err
		}
	}
	return records, dirSize, nil
}

func findSignatureInBlock(b []byte) int {
	for i := len(b) - directoryEndLen; i >= 0; i-- {
		if b[i] == 'P' && b[i+1] == 'K' && b[i+2] == 0x05 && b[i+3] == 0x06 {
			n := int(b[i+directoryEndLen-2]) | int(b[i+directoryEndLen-1])<<8 // 注释的长度
			if n+directoryEndLen+i > len(b) {
				return -1 // 注释被截断了：标准库也不认
			}
			return i
		}
	}
	return -1
}

func findDirectory64End(r io.ReaderAt, endOffset int64) (int64, error) {
	locOffset := endOffset - directory64LocLen
	if locOffset < 0 {
		return -1, nil
	}
	buf := make([]byte, directory64LocLen)
	if _, err := r.ReadAt(buf, locOffset); err != nil {
		return -1, err
	}
	if binary.LittleEndian.Uint32(buf) != 0x07064b50 {
		return -1, nil
	}
	if binary.LittleEndian.Uint32(buf[4:]) != 0 { // ZIP64 结束记录所在的盘号
		return -1, nil
	}
	p := binary.LittleEndian.Uint64(buf[8:])       // ZIP64 结束记录的位置
	if binary.LittleEndian.Uint32(buf[16:]) != 1 { // 一共几张盘
		return -1, nil
	}
	return int64(p), nil
}

func readDirectory64End(r io.ReaderAt, offset int64) (records, dirSize uint64, err error) {
	buf := make([]byte, directory64EndLen)
	if _, err := r.ReadAt(buf, offset); err != nil {
		return 0, 0, err
	}
	if binary.LittleEndian.Uint32(buf) != 0x06064b50 {
		return 0, 0, zip.ErrFormat
	}
	// 标记 4 字节，记录长度 8，两个版本号各 2，两个盘号各 4，本盘条目数 8，然后才是总条目数、清单大小
	return binary.LittleEndian.Uint64(buf[32:]), binary.LittleEndian.Uint64(buf[40:]), nil
}

type contextReaderAt struct {
	ctx context.Context
	r   io.ReaderAt
}

func (r contextReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.ReadAt(p, off)
}
