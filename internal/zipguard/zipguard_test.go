package zipguard

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func zipBytes(t *testing.T, n int) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i < n; i++ {
		w, err := zw.Create(fmt.Sprintf("dir/file-%06d.txt", i))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// 包尾申报的条目数和清单大小，读出来的和标准库打开以后看到的一样；前面带别的数据的、带注释的、ZIP64 的都一样。
func TestDeclaredMatchesStdlib(t *testing.T) {
	plain := zipBytes(t, 300)
	prefixed := append(bytes.Repeat([]byte("MZ-junk-"), 5000), plain...) // 自解压包那样，前面有别的数据

	var commented bytes.Buffer
	zw := zip.NewWriter(&commented)
	w, _ := zw.Create("a.txt")
	_, _ = w.Write([]byte("a"))
	_ = zw.SetComment(string(bytes.Repeat([]byte("c"), 4000))) // 结束记录不在最后 1 KB 里
	_ = zw.Close()

	// 65536 个条目：标准库写出来的就是 ZIP64
	big := zipBytes(t, 65536)

	for name, b := range map[string][]byte{"普通": plain, "前面带数据": prefixed, "带长注释": commented.Bytes(), "ZIP64": big} {
		records, dirSize, err := Declared(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Errorf("%s：读不出申报的数：%v", name, err)
			continue
		}
		zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatalf("%s：标准库打不开测试用的包：%v", name, err)
		}
		if int(records) != len(zr.File) || dirSize == 0 {
			t.Errorf("%s：申报 %d 个、清单 %d 字节，标准库看到 %d 个", name, records, dirSize, len(zr.File))
		}
		// 经 NewReader 打开，结果和标准库一样
		got, err := NewReader(bytes.NewReader(b), int64(len(b)), Default)
		if err != nil || len(got.File) != len(zr.File) {
			t.Errorf("%s：正常的包应当照常打开：%v", name, err)
		}
	}
	// 不是 ZIP、太短的：读不出来，不交给标准库
	for _, b := range [][]byte{nil, []byte("PK"), bytes.Repeat([]byte("not a zip "), 10000)} {
		if _, _, err := Declared(bytes.NewReader(b), int64(len(b))); !errors.Is(err, zip.ErrFormat) {
			t.Errorf("%d 字节的非 ZIP 应当读不出结束记录：%v", len(b), err)
		}
		if _, err := NewReader(bytes.NewReader(b), int64(len(b)), Default); err == nil {
			t.Errorf("%d 字节的非 ZIP 不该打得开", len(b))
		}
	}
}

// 申报的条目数、清单大小超过上限：不打开。实际的清单比申报的长（申报的数是假的）：读到预算用完就停。
func TestLimits(t *testing.T) {
	b := zipBytes(t, 3000)
	if _, err := NewReader(bytes.NewReader(b), int64(len(b)), Limits{MaxEntries: 2999, MaxDirBytes: 64 << 20}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("申报的条目数超了应当拒绝：%v", err)
	}
	if _, err := NewReader(bytes.NewReader(b), int64(len(b)), Limits{MaxEntries: 3000, MaxDirBytes: 64 << 20}); err != nil {
		t.Errorf("正好到上限应当打得开：%v", err)
	}
	if _, err := NewReader(bytes.NewReader(b), int64(len(b)), Limits{MaxEntries: 1 << 20, MaxDirBytes: 32 << 10}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("申报的清单大小超了应当拒绝：%v", err)
	}
	// 把包尾申报的条目数和清单大小改小（假的）：第一层过了，第二层在读的时候拦住
	lie := append([]byte(nil), b...)
	end := bytes.LastIndex(lie, []byte{'P', 'K', 0x05, 0x06})
	binary.LittleEndian.PutUint16(lie[end+8:], 10)  // 本盘条目数
	binary.LittleEndian.PutUint16(lie[end+10:], 10) // 总条目数
	dirSize := binary.LittleEndian.Uint32(lie[end+12:])
	dirOffset := binary.LittleEndian.Uint32(lie[end+16:])
	binary.LittleEndian.PutUint32(lie[end+12:], 1024)                   // 清单大小：说只有 1 KB
	binary.LittleEndian.PutUint32(lie[end+16:], dirOffset+dirSize-1024) // 偏移跟着改，标准库算出来的起点才还是 0
	counted := &countingReaderAt{r: bytes.NewReader(lie)}
	_, err := NewReader(counted, int64(len(lie)), Limits{MaxEntries: 1 << 20, MaxDirBytes: 32 << 10})
	if err == nil {
		t.Fatal("申报的数是假的、实际很长的清单不该打得开（要么预算拦住，要么格式不对）")
	}
	if counted.n > 32<<10+70<<10 { // 清单预算，加上找结束记录时读的那 66 KB
		t.Errorf("预算是 32 KB，却读了 %d 字节", counted.n)
	}
}

type countingReaderAt struct {
	r io.ReaderAt
	n int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.n += int64(n)
	return n, err
}

// 真正要守住的性质：一个很大的文件，包尾申报着几百万个条目（清单其实是空的），打开它不能分配出一大块内存。
// 标准库会按申报的条目数给文件表留位置，只拿整个文件的大小去核这个数。这里量的就是分配量，不只是看返回了什么错：
// 以后 Go 的版本变了、这段检查被改坏了，这条都会报出来。
func TestHugeDeclaredCountAllocatesNothing(t *testing.T) {
	const size = 96 << 20
	const declared = 3_000_000 // 96 MiB / 30 ≈ 335 万，标准库会信这个数，留出 2400 万字节的指针表
	path := filepath.Join(t.TempDir(), "sparse.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil { // 不真的写 96 MiB 数据
		t.Fatal(err)
	}
	// 包尾：ZIP64 结束记录（56）+ ZIP64 定位符（20）+ 结束记录（22）
	tail := make([]byte, 0, 98)
	z64 := make([]byte, 56)
	binary.LittleEndian.PutUint32(z64, 0x06064b50)
	binary.LittleEndian.PutUint64(z64[4:], 44)
	binary.LittleEndian.PutUint64(z64[24:], declared) // 本盘条目数
	binary.LittleEndian.PutUint64(z64[32:], declared) // 总条目数
	binary.LittleEndian.PutUint64(z64[40:], 0)        // 清单大小
	binary.LittleEndian.PutUint64(z64[48:], 0)        // 清单位置
	loc := make([]byte, 20)
	binary.LittleEndian.PutUint32(loc, 0x07064b50)
	binary.LittleEndian.PutUint64(loc[8:], size-98)
	binary.LittleEndian.PutUint32(loc[16:], 1)
	end := make([]byte, 22)
	copy(end, []byte{'P', 'K', 0x05, 0x06})
	binary.LittleEndian.PutUint16(end[8:], 0xffff)
	binary.LittleEndian.PutUint16(end[10:], 0xffff)
	binary.LittleEndian.PutUint32(end[12:], 0xffffffff)
	binary.LittleEndian.PutUint32(end[16:], 0xffffffff)
	tail = append(append(append(tail, z64...), loc...), end...)
	if _, err := f.WriteAt(tail, size-98); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	alloc := func(fn func()) uint64 {
		var a, b runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&a)
		fn()
		runtime.ReadMemStats(&b)
		return b.TotalAlloc - a.TotalAlloc
	}
	// 先确认这个文件确实能让标准库分配一大块：不然下面那条断言什么都没证明
	if std := alloc(func() {
		if zr, err := zip.OpenReader(path); err == nil {
			zr.Close()
		}
	}); std < 16<<20 {
		t.Logf("注意：这一版标准库打开它只分配了 %d 字节，可能已经不再按申报的条目数预先分配", std)
	}
	var openErr error
	got := alloc(func() {
		zr, err := Open(path, Default)
		if err == nil {
			zr.Close()
		}
		openErr = err
	})
	if !errors.Is(openErr, ErrTooLarge) {
		t.Errorf("申报三百万个条目的包应当按文件太多拒绝：%v", openErr)
	}
	if got > 1<<20 {
		t.Errorf("拒绝它的过程中分配了 %d 字节，应当只有读包尾用的几十 KB", got)
	}
}
