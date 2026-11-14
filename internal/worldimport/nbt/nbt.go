// Package nbt 只读地解析 Java 版的 NBT（level.dat 这类文件），带展开大小、嵌套深度和节点数的上限。
//
// 地图是别人给的文件，按恶意输入对待：gzip 炸弹展开到上限就停；列表、数组声明的长度先和剩下的字节数比，
// 不按声明去分配内存；嵌套过深、节点过多都报错。这个包不提供写入：导入从不改写 level.dat，
// 免得把读不全的结构写回去丢掉别人的数据。
package nbt

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"unicode/utf16"
)

// 标签类型。
const (
	TagEnd byte = iota
	TagByte
	TagShort
	TagInt
	TagLong
	TagFloat
	TagDouble
	TagByteArray
	TagString
	TagList
	TagCompound
	TagIntArray
	TagLongArray
)

// Compound 复合标签：名字 → 值。值的 Go 类型：int8、int16、int32、int64、float32、float64、[]byte、string、
// List、Compound、[]int32、[]int64。
type Compound map[string]any

// List 列表标签。
type List struct {
	Elem  byte
	Items []any
}

// Limits 解析的上限。
type Limits struct {
	MaxFile  int64 // 压缩文件本身最大多少字节（读文件时）
	MaxBytes int64 // 展开以后最多多少字节
	MaxDepth int   // 复合、列表最多嵌套几层
	MaxNodes int   // 最多多少个值
	// Budget 不是 nil 时，这个文件展开的字节数和值的个数还要从这份共用的额度里扣（见 Budget）。
	Budget *Budget
}

// Budget 一批 NBT 文件合起来的上限：一次检测里读的所有文件共用一份。单个文件各有各的上限，合起来以前没有，
// 一个包里放上几百个塞满的 level.dat，逐个都不超，加起来能把检测拖住很久。
// 每读一个文件，从里面扣掉它展开的字节数和值的个数；不够扣了就返回 ErrBudget，之后的文件一律不再读。
// 不能几个 goroutine 同时用：一次检测是顺着读的。
type Budget struct {
	Bytes int64 // 还能展开多少字节
	Nodes int64 // 还能有多少个值
	spent bool
}

// Spent 额度是不是已经用完过（有文件因为它没读成）。
func (b *Budget) Spent() bool { return b != nil && b.spent }

// LevelLimits 读 level.dat 用的上限。正常的 level.dat 展开后几十 KB，背包里塞满写满字的书也到不了几 MB。
var LevelLimits = Limits{MaxFile: 16 << 20, MaxBytes: 32 << 20, MaxDepth: 128, MaxNodes: 1_000_000}

// 错误。
var (
	ErrTooLarge = errors.New("文件展开后太大")
	ErrTooDeep  = errors.New("嵌套层数太多")
	ErrTooMany  = errors.New("内容条目太多")
	ErrBudget   = errors.New("这一批文件合起来太大，没有读完")
	ErrCorrupt  = errors.New("文件内容不完整或已损坏")
	ErrNotNBT   = errors.New("不是 Java 版的 NBT 文件")
)

// Compression 文件的压缩方式。
type Compression int

const (
	CompNone Compression = iota
	CompGzip
	CompZlib
)

func (c Compression) String() string {
	switch c {
	case CompGzip:
		return "gzip"
	case CompZlib:
		return "zlib"
	}
	return "none"
}

// ReadFile 读一个 NBT 文件（自动认 gzip / zlib / 不压缩）。
func ReadFile(path string, lim Limits) (string, Compound, Compression, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", nil, CompNone, err
	}
	defer f.Close()
	if lim.MaxFile > 0 {
		if st, err := f.Stat(); err == nil && st.Size() > lim.MaxFile {
			return "", nil, CompNone, fmt.Errorf("%w（%d 字节）", ErrTooLarge, st.Size())
		}
	}
	return Read(f, lim)
}

// Read 从 r 读一个 NBT 文件（自动认 gzip / zlib / 不压缩）。r 最多读 MaxFile 字节。
func Read(r io.Reader, lim Limits) (string, Compound, Compression, error) {
	if lim.MaxFile > 0 {
		r = &capReader{r: r, left: lim.MaxFile}
	}
	br := bufio.NewReader(r)
	head, err := br.Peek(2)
	if err != nil {
		return "", nil, CompNone, fmt.Errorf("%w：文件是空的或太短", ErrCorrupt)
	}
	var body io.Reader = br
	comp := CompNone
	switch {
	case head[0] == 0x1f && head[1] == 0x8b:
		zr, err := gzip.NewReader(br)
		if err != nil {
			return "", nil, CompGzip, fmt.Errorf("%w：%v", ErrCorrupt, err)
		}
		defer zr.Close()
		body, comp = zr, CompGzip
	case head[0] == 0x78 && (uint16(head[0])<<8|uint16(head[1]))%31 == 0:
		zr, err := zlib.NewReader(br)
		if err != nil {
			return "", nil, CompZlib, fmt.Errorf("%w：%v", ErrCorrupt, err)
		}
		defer zr.Close()
		body, comp = zr, CompZlib
	case head[0] == TagCompound:
	default:
		return "", nil, CompNone, ErrNotNBT
	}
	max := lim.MaxBytes
	if max <= 0 {
		max = LevelLimits.MaxBytes
	}
	// 共用的额度：剩下的比这个文件自己的上限还少，就按剩下的读；因此读不完的算额度用完，不算这个文件太大
	bud := lim.Budget
	byBudget := false
	if bud != nil {
		if bud.spent || bud.Bytes <= 0 || bud.Nodes <= 0 {
			bud.spent = true
			return "", nil, comp, ErrBudget
		}
		if bud.Bytes < max {
			max, byBudget = bud.Bytes, true
		}
	}
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(body, max+1))
	if bud != nil {
		bud.Bytes -= min(n, max)
	}
	if errors.Is(err, ErrTooLarge) {
		return "", nil, comp, err
	}
	if err != nil {
		return "", nil, comp, fmt.Errorf("%w：%v", ErrCorrupt, err)
	}
	if n > max {
		if byBudget {
			bud.spent = true
			return "", nil, comp, ErrBudget
		}
		return "", nil, comp, ErrTooLarge
	}
	if bud == nil {
		name, root, err := Decode(buf.Bytes(), lim)
		return name, root, comp, err
	}
	if lim.MaxNodes <= 0 {
		lim.MaxNodes = LevelLimits.MaxNodes
	}
	byBudget = false
	if bud.Nodes < int64(lim.MaxNodes) {
		lim.MaxNodes, byBudget = int(bud.Nodes), true
	}
	name, root, nodes, err := decode(buf.Bytes(), lim)
	bud.Nodes -= int64(nodes)
	if byBudget && errors.Is(err, ErrTooMany) {
		bud.spent = true
		err = ErrBudget
	}
	return name, root, comp, err
}

// capReader 最多读 left 字节，再往后读就报 ErrTooLarge（文件本身超过上限）。
type capReader struct {
	r    io.Reader
	left int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		var one [1]byte
		if n, _ := c.r.Read(one[:]); n > 0 {
			return 0, ErrTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

// Decode 解析不压缩的 NBT：根必须是复合标签。
func Decode(b []byte, lim Limits) (string, Compound, error) {
	name, root, _, err := decode(b, lim)
	return name, root, err
}

// decode 同 Decode，另外返回一共读了多少个值（出错时是读到出错为止的个数）。
func decode(b []byte, lim Limits) (string, Compound, int, error) {
	d := &decoder{b: b, lim: lim}
	if d.lim.MaxDepth <= 0 {
		d.lim.MaxDepth = LevelLimits.MaxDepth
	}
	if d.lim.MaxNodes <= 0 {
		d.lim.MaxNodes = LevelLimits.MaxNodes
	}
	t, err := d.u8()
	if err != nil {
		return "", nil, 0, err
	}
	if t != TagCompound {
		return "", nil, 0, ErrNotNBT
	}
	name, err := d.str()
	if err != nil {
		return "", nil, d.nodes, err
	}
	v, err := d.payload(TagCompound, 1)
	if err != nil {
		return "", nil, d.nodes, err
	}
	return name, v.(Compound), d.nodes, nil
}

type decoder struct {
	b     []byte
	p     int
	lim   Limits
	nodes int
}

func (d *decoder) need(n int) error {
	if n < 0 || len(d.b)-d.p < n {
		return fmt.Errorf("%w：内容在第 %d 字节处提前结束", ErrCorrupt, d.p)
	}
	return nil
}

func (d *decoder) u8() (byte, error) {
	if err := d.need(1); err != nil {
		return 0, err
	}
	v := d.b[d.p]
	d.p++
	return v, nil
}

func (d *decoder) u16() (uint16, error) {
	if err := d.need(2); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint16(d.b[d.p:])
	d.p += 2
	return v, nil
}

func (d *decoder) u32() (uint32, error) {
	if err := d.need(4); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint32(d.b[d.p:])
	d.p += 4
	return v, nil
}

func (d *decoder) u64() (uint64, error) {
	if err := d.need(8); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint64(d.b[d.p:])
	d.p += 8
	return v, nil
}

// length 读一个长度（有符号 int32），并确认剩下的字节至少还有 n*unit 个：按声明分配内存之前先核对。
func (d *decoder) length(unit int) (int, error) {
	v, err := d.u32()
	if err != nil {
		return 0, err
	}
	n := int32(v)
	if n < 0 {
		return 0, fmt.Errorf("%w：长度是负数", ErrCorrupt)
	}
	if unit > 0 && int64(n)*int64(unit) > int64(len(d.b)-d.p) {
		return 0, fmt.Errorf("%w：声明了 %d 项，文件里没有这么多内容", ErrCorrupt, n)
	}
	return int(n), nil
}

func (d *decoder) str() (string, error) {
	n, err := d.u16()
	if err != nil {
		return "", err
	}
	if err := d.need(int(n)); err != nil {
		return "", err
	}
	s := decodeMUTF8(d.b[d.p : d.p+int(n)])
	d.p += int(n)
	return s, nil
}

func (d *decoder) node() error {
	d.nodes++
	if d.nodes > d.lim.MaxNodes {
		return ErrTooMany
	}
	return nil
}

func (d *decoder) payload(t byte, depth int) (any, error) {
	if err := d.node(); err != nil {
		return nil, err
	}
	switch t {
	case TagByte:
		v, err := d.u8()
		return int8(v), err
	case TagShort:
		v, err := d.u16()
		return int16(v), err
	case TagInt:
		v, err := d.u32()
		return int32(v), err
	case TagLong:
		v, err := d.u64()
		return int64(v), err
	case TagFloat:
		v, err := d.u32()
		return math.Float32frombits(v), err
	case TagDouble:
		v, err := d.u64()
		return math.Float64frombits(v), err
	case TagByteArray:
		n, err := d.length(1)
		if err != nil {
			return nil, err
		}
		v := append([]byte(nil), d.b[d.p:d.p+n]...)
		d.p += n
		return v, nil
	case TagString:
		return d.str()
	case TagIntArray:
		n, err := d.length(4)
		if err != nil {
			return nil, err
		}
		v := make([]int32, n)
		for k := range v {
			v[k] = int32(binary.BigEndian.Uint32(d.b[d.p:]))
			d.p += 4
		}
		return v, nil
	case TagLongArray:
		n, err := d.length(8)
		if err != nil {
			return nil, err
		}
		v := make([]int64, n)
		for k := range v {
			v[k] = int64(binary.BigEndian.Uint64(d.b[d.p:]))
			d.p += 8
		}
		return v, nil
	case TagList:
		if depth > d.lim.MaxDepth {
			return nil, ErrTooDeep
		}
		et, err := d.u8()
		if err != nil {
			return nil, err
		}
		if et > TagLongArray {
			return nil, fmt.Errorf("%w：未知的标签类型 %d", ErrCorrupt, et)
		}
		n, err := d.length(minSize(et))
		if err != nil {
			return nil, err
		}
		if et == TagEnd && n > 0 {
			return nil, fmt.Errorf("%w：空类型的列表声明了 %d 项", ErrCorrupt, n)
		}
		l := List{Elem: et, Items: make([]any, 0, min(n, 4096))}
		for range n {
			v, err := d.payload(et, depth+1)
			if err != nil {
				return nil, err
			}
			l.Items = append(l.Items, v)
		}
		return l, nil
	case TagCompound:
		if depth > d.lim.MaxDepth {
			return nil, ErrTooDeep
		}
		c := Compound{}
		for {
			ct, err := d.u8()
			if err != nil {
				return nil, err
			}
			if ct == TagEnd {
				return c, nil
			}
			if ct > TagLongArray {
				return nil, fmt.Errorf("%w：未知的标签类型 %d", ErrCorrupt, ct)
			}
			name, err := d.str()
			if err != nil {
				return nil, err
			}
			v, err := d.payload(ct, depth+1)
			if err != nil {
				return nil, err
			}
			c[name] = v // 同名的后一个盖掉前一个，和游戏自己读的结果一样
		}
	}
	return nil, fmt.Errorf("%w：未知的标签类型 %d", ErrCorrupt, t)
}

// minSize 一个值至少占几个字节（列表声明的长度用它和剩余字节比）。
func minSize(t byte) int {
	switch t {
	case TagByte, TagCompound:
		return 1 // 空复合只有一个 TAG_End
	case TagShort, TagString:
		return 2
	case TagInt, TagFloat, TagByteArray, TagIntArray, TagLongArray:
		return 4
	case TagLong, TagDouble:
		return 8
	case TagList:
		return 5
	}
	return 0
}

// decodeMUTF8 解 Java 的 modified UTF-8（NUL 写成 C0 80，补充平面的字符拆成两个各 3 字节的代理项）。
// 写得不对的字节换成 U+FFFD，不报错：这里读出来的字符串只用来显示和比对。
func decodeMUTF8(b []byte) string {
	if isASCII(b) {
		return string(b)
	}
	var units []uint16
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c < 0x80:
			units = append(units, uint16(c))
			i++
		case c&0xe0 == 0xc0 && i+1 < len(b) && b[i+1]&0xc0 == 0x80:
			units = append(units, uint16(c&0x1f)<<6|uint16(b[i+1]&0x3f))
			i += 2
		case c&0xf0 == 0xe0 && i+2 < len(b) && b[i+1]&0xc0 == 0x80 && b[i+2]&0xc0 == 0x80:
			units = append(units, uint16(c&0x0f)<<12|uint16(b[i+1]&0x3f)<<6|uint16(b[i+2]&0x3f))
			i += 3
		default:
			units = append(units, 0xfffd)
			i++
		}
	}
	return string(utf16.Decode(units))
}

func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 || c == 0 {
			return false
		}
	}
	return true
}

// ---------- 取值 ----------

// Get 按路径取值。
func (c Compound) Get(path ...string) (any, bool) {
	var cur any = c
	for _, k := range path {
		m, ok := cur.(Compound)
		if !ok {
			return nil, false
		}
		if cur, ok = m[k]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// Compound 取一个复合标签。
func (c Compound) Compound(path ...string) (Compound, bool) {
	v, ok := c.Get(path...)
	m, ok2 := v.(Compound)
	return m, ok && ok2
}

// String 取一个字符串。
func (c Compound) String(path ...string) (string, bool) {
	v, ok := c.Get(path...)
	s, ok2 := v.(string)
	return s, ok && ok2
}

// Int 取一个整数（字节、短整、整数、长整都认）。
func (c Compound) Int(path ...string) (int64, bool) {
	v, ok := c.Get(path...)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

// Bool 取一个布尔值（游戏存成字节）。
func (c Compound) Bool(path ...string) (val, ok bool) {
	n, ok := c.Int(path...)
	return n != 0, ok
}

// List 取一个列表。
func (c Compound) List(path ...string) (List, bool) {
	v, ok := c.Get(path...)
	l, ok2 := v.(List)
	return l, ok && ok2
}

// Strings 取一个字符串列表（不是字符串的项跳过）。
func (c Compound) Strings(path ...string) ([]string, bool) {
	l, ok := c.List(path...)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(l.Items))
	for _, it := range l.Items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out, true
}

// UUID 把一个 UUID 值转成标准写法：认 4 个整数的数组（1.16 起的写法）、字符串，以及 UUIDMost / UUIDLeast 两个长整。
func UUID(v any) (string, bool) {
	switch u := v.(type) {
	case []int32:
		if len(u) != 4 {
			return "", false
		}
		var b [16]byte
		for k, x := range u {
			binary.BigEndian.PutUint32(b[k*4:], uint32(x))
		}
		return fmtUUID(b), true
	case string:
		s := strings.ToLower(strings.TrimSpace(u))
		if len(s) == 32 && !strings.Contains(s, "-") {
			s = s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
		}
		if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
			return "", false
		}
		for k, r := range s {
			if k == 8 || k == 13 || k == 18 || k == 23 {
				continue
			}
			if !strings.ContainsRune("0123456789abcdef", r) {
				return "", false
			}
		}
		return s, true
	}
	return "", false
}

// UUIDMostLeast 老写法：UUIDMost、UUIDLeast 两个长整。
func UUIDMostLeast(most, least int64) string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(most))
	binary.BigEndian.PutUint64(b[8:], uint64(least))
	return fmtUUID(b)
}

func fmtUUID(b [16]byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for k, x := range b {
		if k == 4 || k == 6 || k == 8 || k == 10 {
			out = append(out, '-')
		}
		out = append(out, hex[x>>4], hex[x&15])
	}
	return string(out)
}
