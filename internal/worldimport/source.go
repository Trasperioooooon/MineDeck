package worldimport

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"minedeck/internal/winname"
)

// 来源（本机文件夹或 ZIP）的读取。两种来源最后都变成同一张「文件清单」，后面的识别、暂存不再管它是哪一种。
// 文件夹和 ZIP 守同一套规矩：名字在 Windows 上能原样落盘、不往外走、不跟链接、大小和条目数有上限。

const (
	maxEntries   = 1_000_000 // 一份来源最多多少个文件（和交接包一致）
	maxPathDepth = 64        // 路径最多多少段
	maxRelLen    = 1024      // 相对路径最长多少字符
)

// entry 来源里的一个普通文件。
type entry struct {
	rel  string // 斜杠分隔，相对来源根，保留原来的大小写
	fold string // winname.Fold(rel)
	size int64
	mod  time.Time
	crc  uint32    // 只有 ZIP 有
	zf   *zip.File // 只有 ZIP 有
}

// sourceErr 来源整体不能用的原因：Kind 告诉界面这是哪一类东西，Msg 是给玩家看的结论，Detail 是细节。
type sourceErr struct {
	Kind   InputKind
	Code   string
	Msg    string
	Detail string
}

func (e *sourceErr) Error() string {
	if e.Detail != "" {
		return e.Msg + "（" + e.Detail + "）"
	}
	return e.Msg
}

// source 打开着的一份来源。
type source struct {
	kind  string // folder | zip
	root  string // 文件夹来源的根（真实路径）；ZIP 来源是文件路径
	files []entry
	dirs  map[string]bool // 出现过的目录（fold 后），含所有上级
	zc    io.Closer       // ZIP 来源底下打开着的文件
	links []string        // 文件夹里遇到的链接、目录联接这类特殊项（相对路径）：不跟进去，导入时拦下
	bad   []string        // 名字在 Windows 上不能原样落盘的项
}

func (s *source) Close() error {
	if s.zc != nil {
		return s.zc.Close()
	}
	return nil
}

// open 打开 e 读内容。
func (s *source) open(e *entry) (io.ReadCloser, error) {
	if e.zf != nil {
		return e.zf.Open()
	}
	return os.Open(filepath.Join(s.root, filepath.FromSlash(e.rel)))
}

// find 按不分大小写的相对路径找文件。
func (s *source) find(rel string) *entry {
	k := winname.Fold(rel)
	i := sort.Search(len(s.files), func(i int) bool { return s.files[i].fold >= k })
	if i < len(s.files) && s.files[i].fold == k {
		return &s.files[i]
	}
	return nil
}

// isDir rel 是不是一个目录（不分大小写）。
func (s *source) isDir(rel string) bool {
	if rel == "" {
		return true
	}
	return s.dirs[winname.Fold(rel)]
}

// under 列出 dir 下面的文件（含更深层），dir 为 "" 时是全部。
func (s *source) under(dir string) []entry {
	if dir == "" {
		return s.files
	}
	p := winname.Fold(dir) + "/"
	i := sort.Search(len(s.files), func(i int) bool { return s.files[i].fold >= p })
	j := i
	for j < len(s.files) && strings.HasPrefix(s.files[j].fold, p) {
		j++
	}
	return s.files[i:j]
}

// children dir 下面直接的子项名字（文件和目录，原样大小写，去重）。
func (s *source) children(dir string) (files, subdirs []string) {
	seenF, seenD := map[string]bool{}, map[string]bool{}
	for _, e := range s.under(dir) {
		rest := relRest(e.rel, dir)
		if first, _, nested := strings.Cut(rest, "/"); nested {
			if k := strings.ToUpper(first); !seenD[k] {
				seenD[k] = true
				subdirs = append(subdirs, first)
			}
		} else if k := strings.ToUpper(rest); !seenF[k] {
			seenF[k] = true
			files = append(files, rest)
		}
	}
	return files, subdirs
}

// relRest rel 在目录前缀 dir 后面的部分（不带开头的斜杠）。前缀是按 winname.Fold 不分大小写比上的，
// 所以按路径段数切，不按字节数切：strings.ToUpper 会改变个别字符（土耳其语的 ı、ſ、ȿ…）的字节长度。
func relRest(rel, dir string) string {
	if dir == "" {
		return rel
	}
	n := strings.Count(dir, "/") + 1
	segs := strings.SplitN(rel, "/", n+1)
	if len(segs) <= n {
		return ""
	}
	return segs[n]
}

// fingerprint 一组文件的指纹：路径、大小、修改时间（ZIP 再加 CRC）。提交前重算比对，来源被改过就要重新检测。
func fingerprint(es []entry) string {
	h := sha256.New()
	var b [8]byte
	for _, e := range es {
		io.WriteString(h, e.fold)
		h.Write([]byte{0})
		binary.BigEndian.PutUint64(b[:], uint64(e.size))
		h.Write(b[:])
		binary.BigEndian.PutUint64(b[:], uint64(e.mod.UnixNano()))
		h.Write(b[:])
		binary.BigEndian.PutUint32(b[:4], e.crc)
		h.Write(b[:4])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// finish 排序、建目录表、查重名（只差大小写的两个文件、文件和目录同名）。
func (s *source) finish() error {
	sort.Slice(s.files, func(a, b int) bool { return s.files[a].fold < s.files[b].fold })
	s.dirs = map[string]bool{}
	for _, e := range s.files {
		for d := path.Dir(e.fold); d != "."; d = path.Dir(d) {
			if s.dirs[d] {
				break
			}
			s.dirs[d] = true
		}
	}
	for k := 1; k < len(s.files); k++ {
		if s.files[k].fold == s.files[k-1].fold {
			return &sourceErr{Kind: KindUnknown, Code: "case-collision",
				Msg:    "压缩包里有两个文件在 Windows 上会是同一个文件（只差大小写），内容却不一样，这个包被改过或者坏了",
				Detail: s.files[k-1].rel + " / " + s.files[k].rel}
		}
	}
	for _, e := range s.files {
		if s.dirs[e.fold] {
			return &sourceErr{Kind: KindUnknown, Code: "file-dir-conflict",
				Msg: "压缩包里同一个名字既是文件又是文件夹，这个包被改过或者坏了", Detail: e.rel}
		}
	}
	return nil
}

// ---------- 文件夹 ----------

// openFolder 读一个本机文件夹的文件清单。不跟符号链接和目录联接（记下来，导入时拦下）。
func openFolder(dir string) (*source, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return nil, &sourceErr{Kind: KindUnknown, Code: "not-found", Msg: "找不到这个文件夹", Detail: err.Error()}
	}
	if !st.IsDir() {
		return nil, &sourceErr{Kind: KindUnknown, Code: "not-dir", Msg: "选的不是文件夹"}
	}
	root, err := filepath.EvalSymlinks(dir) // 用户亲手选的根可以是链接，解开它；里面的链接不跟
	if err != nil {
		root = dir
	}
	root, _ = filepath.Abs(root)
	s := &source{kind: "folder", root: root}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			return &sourceErr{Kind: KindUnknown, Code: "unreadable", Msg: "来源里有读不了的文件或文件夹，可能被别的程序占着或者没有权限",
				Detail: filepath.ToSlash(rel) + "：" + err.Error()}
		}
		if p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.Type()&(fs.ModeSymlink|fs.ModeIrregular|fs.ModeDevice|fs.ModeNamedPipe|fs.ModeSocket|fs.ModeCharDevice) != 0 {
			s.links = append(s.links, rel)
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if strings.Count(rel, "/")+1 > maxPathDepth || len(rel) > maxRelLen {
			return &sourceErr{Kind: KindUnknown, Code: "too-deep", Msg: "来源里的文件夹层数太多或者路径太长", Detail: rel}
		}
		if !winname.OK(d.Name()) {
			s.bad = append(s.bad, rel)
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if len(s.files) >= maxEntries {
			return &sourceErr{Kind: KindUnknown, Code: "too-many", Msg: fmt.Sprintf("来源里的文件超过了 %d 个，不像一张地图", maxEntries)}
		}
		info, err := d.Info()
		if err != nil {
			return &sourceErr{Kind: KindUnknown, Code: "unreadable", Msg: "来源里有读不了的文件", Detail: rel + "：" + err.Error()}
		}
		s.files = append(s.files, entry{rel: rel, fold: winname.Fold(rel), size: info.Size(), mod: info.ModTime()})
		return nil
	})
	if err != nil {
		var se *sourceErr
		if errors.As(err, &se) {
			return nil, se
		}
		return nil, &sourceErr{Kind: KindUnknown, Code: "unreadable", Msg: "读不了这个文件夹", Detail: err.Error()}
	}
	if err := s.finish(); err != nil {
		return nil, err
	}
	return s, nil
}

// ---------- ZIP ----------

var (
	sigZip      = []byte("PK\x03\x04")
	sigZipEmpty = []byte("PK\x05\x06")
	sigZipSpan  = []byte("PK\x07\x08")
	sigRar      = []byte("Rar!\x1a\x07")
	sig7z       = []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}
	sigGzip     = []byte{0x1f, 0x8b}
)

// sniff 看文件开头几个字节是什么格式。
func sniff(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, 8)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	switch {
	case bytes.HasPrefix(head, sigZip), bytes.HasPrefix(head, sigZipEmpty):
		return "zip", nil
	case bytes.HasPrefix(head, sigZipSpan):
		return "zip-split", nil
	case bytes.HasPrefix(head, sigRar):
		return "rar", nil
	case bytes.HasPrefix(head, sig7z):
		return "7z", nil
	case bytes.HasPrefix(head, sigGzip):
		return "gzip", nil
	case n > 0 && head[0] == 0x0a:
		return "nbt", nil
	}
	return "", nil
}

// cleanZipName 校验一条 ZIP 条目名，返回斜杠分隔的相对路径。反斜杠按分隔符算（Windows 上的压缩工具就这么认）。
// 绝对路径、盘符、UNC、往上走的 ..、空段、冒号（盘符和备用数据流）、控制字符、设备名、结尾的点和空格，一律不收。
func cleanZipName(name string) (rel string, isDir bool, err error) {
	if strings.IndexFunc(name, func(r rune) bool { return r == 0 || unicode.IsControl(r) }) >= 0 {
		return "", false, fmt.Errorf("名字里有控制字符")
	}
	n := strings.ReplaceAll(name, `\`, "/")
	isDir = strings.HasSuffix(n, "/")
	n = strings.TrimSuffix(n, "/")
	switch {
	case n == "":
		return "", false, fmt.Errorf("空名字")
	case strings.HasPrefix(n, "/"):
		return "", false, fmt.Errorf("绝对路径")
	case strings.Contains(n, ":"):
		return "", false, fmt.Errorf("带盘符或冒号")
	case len(n) > maxRelLen:
		return "", false, fmt.Errorf("路径太长")
	}
	segs := strings.Split(n, "/")
	if len(segs) > maxPathDepth {
		return "", false, fmt.Errorf("层数太多")
	}
	for _, seg := range segs {
		if seg == ".." || seg == "." || seg == "" {
			return "", false, fmt.Errorf("带 . 或 .. 或空段")
		}
		if !winname.OK(seg) {
			return "", false, fmt.Errorf("「%s」在 Windows 上不能当名字", seg)
		}
	}
	return n, isDir, nil
}

// maxZipDirBytes 打开 ZIP 时读文件清单（中央目录）最多读多少字节。
//
// Go 的 zip 包打开时会把清单里的每一条都读进内存，一直读到不像清单的地方才停，不看包里自己申报有多少条；
// 条目数要等它全读完才比得了。一个做过手脚的包可以在清单里塞几千万条，还没轮到比条目数，内存先被吃光
// （2026-11-14 Codex 复审第 1 条）。所以给打开这一步读的字节数设预算：清单里一条最少 46 字节，64 MiB 最多一百四十万条，
// 读完以后再照旧比 maxEntries。正常的地图哪怕有几十万个文件，清单也只有十几兆。做成变量是为了测试能调小。
var maxZipDirBytes int64 = 64 << 20

var errZipDirTooBig = errors.New("压缩包的文件清单太大")

// maxZipDeclaredBytes 压缩包里各文件申报的大小加起来最多多少。申报的大小是包里自己写的，不可信：
// 以前只防加起来溢出，合计可以到 MaxInt64，后面算「要多少空间」时再加上余量就溢出成负数，空间不够的检查放行
// （2026-11-14 Codex 合并复审第 6 条）。1 TiB 远大于任何真的地图。做成变量是为了测试能调小。
var maxZipDeclaredBytes int64 = 1 << 40

// budgetReaderAt 打开 ZIP 的这段时间里数着读了多少字节，超过预算就报错；打开完成以后放开，读文件内容不受它限制。
type budgetReaderAt struct {
	r       io.ReaderAt
	limited atomic.Bool
	left    atomic.Int64
	over    atomic.Bool
}

func (b *budgetReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if b.limited.Load() {
		if b.left.Add(-int64(len(p))) < 0 {
			b.over.Store(true)
			return 0, errZipDirTooBig
		}
	}
	return b.r.ReadAt(p, off)
}

// openZipReader 打开 ZIP、读出文件清单，读清单的字节数不超过 maxZipDirBytes。返回的 Closer 是底下的文件。
func openZipReader(p string) (*zip.Reader, io.Closer, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	b := &budgetReaderAt{r: f}
	b.left.Store(maxZipDirBytes)
	b.limited.Store(true)
	zr, err := zip.NewReader(b, st.Size())
	b.limited.Store(false)
	if b.over.Load() {
		err = errZipDirTooBig
	}
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return zr, f, nil
}

// openZip 读一个 ZIP 的文件清单（只读中央目录，不解压）。条目有一条可疑，整个包都不要。
func openZip(p string) (*source, error) {
	zr, zc, err := openZipReader(p)
	if errors.Is(err, errZipDirTooBig) {
		return nil, &sourceErr{Kind: KindUnknown, Code: "too-many",
			Msg: fmt.Sprintf("压缩包里的文件太多了（文件清单超过 %s），不像一张地图", fmtBytes(maxZipDirBytes))}
	}
	if err != nil {
		return nil, &sourceErr{Kind: KindArchive, Code: "zip-broken", Msg: "这个压缩包打不开，可能下载不完整或者已经损坏", Detail: err.Error()}
	}
	s := &source{kind: "zip", root: p, zc: zc}
	fail := func(e *sourceErr) (*source, error) {
		zc.Close()
		return nil, e
	}
	if len(zr.File) > maxEntries {
		return fail(&sourceErr{Kind: KindUnknown, Code: "too-many", Msg: fmt.Sprintf("压缩包里的文件超过了 %d 个，不像一张地图", maxEntries)})
	}
	seen := map[string]int{}
	var total int64
	for _, f := range zr.File {
		rel, isDir, err := cleanZipName(f.Name)
		if err != nil {
			return fail(&sourceErr{Kind: KindUnknown, Code: "bad-path", Msg: "压缩包里有可疑的路径，为了安全整个包都不导入",
				Detail: fmt.Sprintf("%q：%v", f.Name, err)})
		}
		if f.Flags&0x1 != 0 {
			return fail(&sourceErr{Kind: KindArchive, Code: "encrypted", Msg: "这个压缩包加了密码。先用解压软件解开，再选解出来的地图文件夹"})
		}
		mode := f.Mode()
		if isDir || mode.IsDir() {
			continue
		}
		if !mode.IsRegular() {
			return fail(&sourceErr{Kind: KindUnknown, Code: "special-entry", Msg: "压缩包里有链接或特殊文件，为了安全整个包都不导入", Detail: f.Name})
		}
		if f.Method != zip.Store && f.Method != zip.Deflate {
			return fail(&sourceErr{Kind: KindArchive, Code: "method", Msg: "这个压缩包用了不支持的压缩方式。先用解压软件解开，再选解出来的地图文件夹",
				Detail: fmt.Sprintf("%s：方式 %d", f.Name, f.Method)})
		}
		size := int64(f.UncompressedSize64)
		if size < 0 || size > maxZipDeclaredBytes-total {
			return fail(&sourceErr{Kind: KindUnknown, Code: "bad-size",
				Msg:    fmt.Sprintf("压缩包里记的文件大小不对（加起来超过 %s），这个包被改过或者坏了", fmtBytes(maxZipDeclaredBytes)),
				Detail: f.Name})
		}
		total += size
		k := winname.Fold(rel)
		if j, dup := seen[k]; dup {
			prev := s.files[j]
			if prev.crc == f.CRC32 && prev.size == size {
				continue // 同一个文件打了两遍
			}
			return fail(&sourceErr{Kind: KindUnknown, Code: "case-collision",
				Msg:    "压缩包里有两个文件在 Windows 上会是同一个文件（只差大小写或重复），内容却不一样，这个包被改过或者坏了",
				Detail: prev.rel + " / " + rel})
		}
		seen[k] = len(s.files)
		s.files = append(s.files, entry{rel: rel, fold: k, size: size, mod: f.Modified, crc: f.CRC32, zf: f})
	}
	if err := s.finish(); err != nil {
		zc.Close()
		return nil, err
	}
	return s, nil
}

// readSmall 读来源里的一个小文件（说明书、清单这类），最多 max 字节。
func (s *source) readSmall(e *entry, max int64) ([]byte, error) {
	if e.size > max {
		return nil, fmt.Errorf("文件太大")
	}
	rc, err := s.open(e)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("文件太大")
	}
	return b, nil
}
