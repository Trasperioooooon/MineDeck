package nbt

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

// helloWorld 是 NBT 规范里的示例文件 hello_world.nbt（不压缩）。
const helloWorld = "0a000b68656c6c6f20776f726c640800046e616d65000942616e616e72616d6100"

func gz(b []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(b)
	w.Close()
	return buf.Bytes()
}

func TestHelloWorld(t *testing.T) {
	raw, _ := hex.DecodeString(helloWorld)
	for _, c := range []struct {
		name string
		in   []byte
		comp Compression
	}{
		{"raw", raw, CompNone},
		{"gzip", gz(raw), CompGzip},
		{"zlib", func() []byte {
			var buf bytes.Buffer
			w := zlib.NewWriter(&buf)
			w.Write(raw)
			w.Close()
			return buf.Bytes()
		}(), CompZlib},
	} {
		t.Run(c.name, func(t *testing.T) {
			name, root, comp, err := Read(bytes.NewReader(c.in), LevelLimits)
			if err != nil {
				t.Fatal(err)
			}
			if name != "hello world" || comp != c.comp {
				t.Fatalf("name=%q comp=%v", name, comp)
			}
			if s, _ := root.String("name"); s != "Bananrama" {
				t.Fatalf("name = %q", s)
			}
		})
	}
}

// enc 测试里手写 NBT 用的小工具。
type enc struct{ bytes.Buffer }

func (e *enc) tag(t byte, name string) { e.WriteByte(t); e.str(name) }
func (e *enc) str(s string) {
	binary.Write(&e.Buffer, binary.BigEndian, uint16(len(s)))
	e.WriteString(s)
}
func (e *enc) i32(v int32) { binary.Write(&e.Buffer, binary.BigEndian, v) }

func TestLimits(t *testing.T) {
	t.Run("gzip 炸弹", func(t *testing.T) {
		var e enc
		e.tag(TagCompound, "")
		e.tag(TagByteArray, "x")
		e.i32(40 << 20)
		e.Write(make([]byte, 40<<20))
		e.WriteByte(TagEnd)
		_, _, _, err := Read(bytes.NewReader(gz(e.Bytes())), LevelLimits)
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("嵌套过深", func(t *testing.T) {
		var e enc
		e.tag(TagCompound, "")
		for range 600 {
			e.tag(TagCompound, "a")
		}
		for range 601 {
			e.WriteByte(TagEnd)
		}
		_, _, err := Decode(e.Bytes(), LevelLimits)
		if !errors.Is(err, ErrTooDeep) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("列表声明的长度比文件长", func(t *testing.T) {
		var e enc
		e.tag(TagCompound, "")
		e.tag(TagList, "l")
		e.WriteByte(TagInt)
		e.i32(1<<31 - 1)
		_, _, err := Decode(e.Bytes(), LevelLimits)
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("复合列表声明很长", func(t *testing.T) {
		var e enc
		e.tag(TagCompound, "")
		e.tag(TagList, "l")
		e.WriteByte(TagCompound)
		e.i32(1<<31 - 1)
		e.Write(make([]byte, 1000)) // 一千个空复合，后面就没了
		_, _, err := Decode(e.Bytes(), LevelLimits)
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("节点太多", func(t *testing.T) {
		var e enc
		e.tag(TagCompound, "")
		e.tag(TagList, "l")
		e.WriteByte(TagByte)
		e.i32(5000)
		e.Write(make([]byte, 5000))
		e.WriteByte(TagEnd)
		_, _, err := Decode(e.Bytes(), Limits{MaxNodes: 1000})
		if !errors.Is(err, ErrTooMany) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("截断", func(t *testing.T) {
		raw, _ := hex.DecodeString(helloWorld)
		_, _, _, err := Read(bytes.NewReader(gz(raw)[:20]), LevelLimits)
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("不是 NBT", func(t *testing.T) {
		_, _, _, err := Read(bytes.NewReader([]byte("PK\x03\x04garbage")), LevelLimits)
		if !errors.Is(err, ErrNotNBT) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("文件本身太大", func(t *testing.T) {
		_, _, _, err := Read(io.MultiReader(bytes.NewReader([]byte{0x0a, 0, 0}), bytes.NewReader(make([]byte, 100))), Limits{MaxFile: 50})
		if err == nil {
			t.Fatal("应该报错")
		}
	})
}

func TestMUTF8(t *testing.T) {
	cases := map[string]string{
		"空岛生存":   "\xe7\xa9\xba\xe5\xb2\x9b\xe7\x94\x9f\xe5\xad\x98",
		"a\x00b": "a\xc0\x80b",
		"😀":      "\xed\xa0\xbd\xed\xb8\x80", // U+1F600 拆成 D83D DE00
	}
	for want, in := range cases {
		if got := decodeMUTF8([]byte(in)); got != want {
			t.Errorf("decode(%x) = %q, want %q", in, got, want)
		}
	}
}

func TestUUID(t *testing.T) {
	ints := []int32{0x069a79f4, 0x44e94726, -0x5e5a6a8e, 0x1e2c6d2a}
	got, ok := UUID(ints)
	if !ok || got != "069a79f4-44e9-4726-a1a5-95721e2c6d2a" {
		t.Fatalf("UUID = %q %v", got, ok)
	}
	if s, ok := UUID("069A79F444E94726A1A595721E2C6D2A"); !ok || s != got {
		t.Fatalf("无横线写法 = %q %v", s, ok)
	}
	if _, ok := UUID("not-a-uuid"); ok {
		t.Fatal("不该认")
	}
	if s := UUIDMostLeast(0x069a79f444e94726, -0x5e5a6a8de1d392d6); s != got {
		t.Fatalf("Most/Least = %q", s)
	}
}

func TestGetters(t *testing.T) {
	var e enc
	e.tag(TagCompound, "")
	e.tag(TagCompound, "Data")
	e.tag(TagByte, "hardcore")
	e.WriteByte(1)
	e.tag(TagList, "ServerBrands")
	e.WriteByte(TagString)
	e.i32(2)
	e.str("vanilla")
	e.str("fabric")
	e.WriteByte(TagEnd)
	e.WriteByte(TagEnd)
	_, root, err := Decode(e.Bytes(), LevelLimits)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := root.Bool("Data", "hardcore"); !ok || !v {
		t.Fatal("hardcore")
	}
	if s, ok := root.Strings("Data", "ServerBrands"); !ok || len(s) != 2 || s[1] != "fabric" {
		t.Fatalf("brands = %v", s)
	}
	if _, ok := root.Int("Data", "missing"); ok {
		t.Fatal("missing")
	}
}
