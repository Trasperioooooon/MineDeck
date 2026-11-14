// Package worldtest generates synthetic Minecraft Java worlds, archives, and
// non-world inputs for unit testing the worldimport package.
package worldtest

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// NBT tag IDs for Minecraft Java Edition (big-endian).
const (
	TagEnd       byte = 0
	TagByte      byte = 1
	TagShort     byte = 2
	TagInt       byte = 3
	TagLong      byte = 4
	TagFloat     byte = 5
	TagDouble    byte = 6
	TagByteArray byte = 7
	TagString    byte = 8
	TagList      byte = 9
	TagCompound  byte = 10
	TagIntArray  byte = 11
	TagLongArray byte = 12
)

type Byte int8
type Short int16
type Int int32
type Long int64
type Float float32
type Double float64
type ByteArray []byte
type String string
type IntArray []int32
type LongArray []int64

// List represents a homogeneous NBT TagList. Elem may be TagEnd only when Items is empty.
type List struct {
	Elem  byte
	Items []any
}

// Field is a named entry inside an NBT Compound.
type Field struct {
	Name  string
	Value any
}

// Compound is an ordered list of NBT fields.
type Compound []Field

// Compression selects the outer wrapper used by WriteFile.
type Compression int

const (
	None Compression = iota
	Gzip
	Zlib
)

// Encode writes root as a named TAG_Compound in big-endian Java NBT format.
func Encode(w io.Writer, rootName string, root Compound) error {
	if w == nil {
		return errors.New("worldtest: nil writer")
	}
	if err := writeByte(w, TagCompound); err != nil {
		return err
	}
	if err := writeModUTF8(w, rootName); err != nil {
		return err
	}
	return writeCompoundPayload(w, root)
}

// Marshal returns the uncompressed big-endian NBT encoding of root.
func Marshal(rootName string, root Compound) ([]byte, error) {
	var buf bytes.Buffer
	if err := Encode(&buf, rootName, root); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteFile encodes root to path using the requested compression, creating parent directories as needed.
func WriteFile(path, rootName string, root Compound, c Compression) error {
	raw, err := Marshal(rootName, root)
	if err != nil {
		return err
	}
	var payload []byte
	switch c {
	case None:
		payload = raw
	case Gzip:
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		if _, err := gw.Write(raw); err != nil {
			_ = gw.Close()
			return err
		}
		if err := gw.Close(); err != nil {
			return err
		}
		payload = buf.Bytes()
	case Zlib:
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		if _, err := zw.Write(raw); err != nil {
			_ = zw.Close()
			return err
		}
		if err := zw.Close(); err != nil {
			return err
		}
		payload = buf.Bytes()
	default:
		return fmt.Errorf("worldtest: unknown compression %d", c)
	}
	return writeFileBytes(path, payload)
}

// UUIDInts converts a UUID string ("xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" or 32 hex chars)
// into Minecraft's 4-int32 big-endian IntArray representation.
func UUIDInts(s string) (IntArray, error) {
	clean := strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(clean) != 32 {
		return nil, fmt.Errorf("worldtest: invalid UUID %q", s)
	}
	b, err := hex.DecodeString(clean)
	if err != nil || len(b) != 16 {
		return nil, fmt.Errorf("worldtest: invalid UUID %q", s)
	}
	out := make(IntArray, 4)
	for i := range 4 {
		out[i] = int32(binary.BigEndian.Uint32(b[i*4 : (i+1)*4]))
	}
	return out, nil
}

func tagOf(v any) (byte, error) {
	switch v.(type) {
	case Byte:
		return TagByte, nil
	case Short:
		return TagShort, nil
	case Int:
		return TagInt, nil
	case Long:
		return TagLong, nil
	case Float:
		return TagFloat, nil
	case Double:
		return TagDouble, nil
	case ByteArray:
		return TagByteArray, nil
	case String:
		return TagString, nil
	case List:
		return TagList, nil
	case Compound:
		return TagCompound, nil
	case IntArray:
		return TagIntArray, nil
	case LongArray:
		return TagLongArray, nil
	default:
		return 0, fmt.Errorf("worldtest: unsupported NBT value type %T (must use worldtest named types)", v)
	}
}

func writePayload(w io.Writer, tag byte, v any) error {
	switch tag {
	case TagByte:
		return writeByte(w, byte(v.(Byte)))
	case TagShort:
		return writeUint16(w, uint16(v.(Short)))
	case TagInt:
		return writeUint32(w, uint32(v.(Int)))
	case TagLong:
		return writeUint64(w, uint64(v.(Long)))
	case TagFloat:
		return writeUint32(w, math.Float32bits(float32(v.(Float))))
	case TagDouble:
		return writeUint64(w, math.Float64bits(float64(v.(Double))))
	case TagByteArray:
		ba := v.(ByteArray)
		if err := writeUint32(w, uint32(len(ba))); err != nil {
			return err
		}
		_, err := w.Write(ba)
		return err
	case TagString:
		return writeModUTF8(w, string(v.(String)))
	case TagList:
		return writeListPayload(w, v.(List))
	case TagCompound:
		return writeCompoundPayload(w, v.(Compound))
	case TagIntArray:
		ia := v.(IntArray)
		if err := writeUint32(w, uint32(len(ia))); err != nil {
			return err
		}
		for _, x := range ia {
			if err := writeUint32(w, uint32(x)); err != nil {
				return err
			}
		}
		return nil
	case TagLongArray:
		la := v.(LongArray)
		if err := writeUint32(w, uint32(len(la))); err != nil {
			return err
		}
		for _, x := range la {
			if err := writeUint64(w, uint64(x)); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("worldtest: invalid tag %d", tag)
	}
}

func writeCompoundPayload(w io.Writer, c Compound) error {
	for _, f := range c {
		tag, err := tagOf(f.Value)
		if err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
		if err := writeByte(w, tag); err != nil {
			return err
		}
		if err := writeModUTF8(w, f.Name); err != nil {
			return fmt.Errorf("field name %q: %w", f.Name, err)
		}
		if err := writePayload(w, tag, f.Value); err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
	}
	return writeByte(w, TagEnd)
}

func writeListPayload(w io.Writer, l List) error {
	if l.Elem > TagLongArray {
		return fmt.Errorf("worldtest: invalid list element tag %d", l.Elem)
	}
	if l.Elem == TagEnd && len(l.Items) > 0 {
		return errors.New("worldtest: TagEnd list cannot contain items")
	}
	if err := writeByte(w, l.Elem); err != nil {
		return err
	}
	if err := writeUint32(w, uint32(len(l.Items))); err != nil {
		return err
	}
	for i, item := range l.Items {
		itemTag, err := tagOf(item)
		if err != nil {
			return fmt.Errorf("list item %d: %w", i, err)
		}
		if itemTag != l.Elem {
			return fmt.Errorf("worldtest: list item %d has tag %d, expected %d", i, itemTag, l.Elem)
		}
		if err := writePayload(w, l.Elem, item); err != nil {
			return fmt.Errorf("list item %d: %w", i, err)
		}
	}
	return nil
}

// encodeModUTF8 converts a Go string into Java Modified UTF-8 bytes.
func encodeModUTF8(s string) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r == 0:
			out = append(out, 0xC0, 0x80)
		case r <= 0x007F:
			out = append(out, byte(r))
		case r <= 0x07FF:
			out = append(out, 0xC0|byte(r>>6), 0x80|byte(r&0x3F))
		case r <= 0xFFFF:
			out = append(out, 0xE0|byte(r>>12), 0x80|byte((r>>6)&0x3F), 0x80|byte(r&0x3F))
		default:
			r1, r2 := utf16.EncodeRune(r)
			for _, u := range [2]rune{r1, r2} {
				out = append(out, 0xE0|byte(u>>12), 0x80|byte((u>>6)&0x3F), 0x80|byte(u&0x3F))
			}
		}
		if len(out) > 65535 {
			return nil, fmt.Errorf("worldtest: modified UTF-8 string exceeds 65535 bytes")
		}
	}
	return out, nil
}

func writeModUTF8(w io.Writer, s string) error {
	b, err := encodeModUTF8(s)
	if err != nil {
		return err
	}
	if err := writeUint16(w, uint16(len(b))); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func writeByte(w io.Writer, b byte) error {
	var buf [1]byte
	buf[0] = b
	_, err := w.Write(buf[:])
	return err
}

func writeUint16(w io.Writer, v uint16) error {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], v)
	_, err := w.Write(buf[:])
	return err
}

func writeUint32(w io.Writer, v uint32) error {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	_, err := w.Write(buf[:])
	return err
}

func writeUint64(w io.Writer, v uint64) error {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	_, err := w.Write(buf[:])
	return err
}

func writeFileBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
