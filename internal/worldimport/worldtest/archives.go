package worldtest

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ZipEntry describes a single entry written by WriteZip.
type ZipEntry struct {
	Name         string      // raw entry name, written as-is (may be hostile)
	Body         []byte      // uncompressed entry payload
	Method       uint16      // zip.Store or zip.Deflate (0 defaults to Deflate for files, Store for dirs)
	Mode         fs.FileMode // e.g. fs.ModeSymlink|0o777; 0 => regular 0644 (or 0755 dir when Dir is true)
	Dir          bool        // write as a directory entry
	Encrypted    bool        // set general-purpose bit 0 (payload not actually encrypted)
	BadCRC       bool        // write a wrong CRC-32 in the headers (payload intact)
	DeclaredSize int64       // if > 0, lie about UncompressedSize64 in headers
}

// ZipOpts configures archive-level options for WriteZip and ZipTree.
type ZipOpts struct {
	ForceZip64 bool // attach ZIP64 extra fields to entries even when small
}

var fixedZipTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// WriteZip writes entries to path using raw ZIP headers so hostile names, bad CRCs,
// size lies, encryption flags, and symlink modes are preserved verbatim.
func WriteZip(path string, entries []ZipEntry, o ZipOpts) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			_ = f.Close()
			_ = os.Remove(path)
		}
	}()

	zw := zip.NewWriter(f)
	for i, e := range entries {
		method := e.Method
		if method == 0 {
			if e.Dir {
				method = zip.Store
			} else {
				method = zip.Deflate
			}
		}

		var compressed []byte
		switch method {
		case zip.Store:
			compressed = e.Body
		case zip.Deflate:
			var buf bytes.Buffer
			fw, err := flate.NewWriter(&buf, flate.DefaultCompression)
			if err != nil {
				return err
			}
			if _, err := fw.Write(e.Body); err != nil {
				_ = fw.Close()
				return err
			}
			if err := fw.Close(); err != nil {
				return err
			}
			compressed = buf.Bytes()
		default:
			return fmt.Errorf("worldtest: entry %d (%q): unsupported zip method %d", i, e.Name, method)
		}

		crc := crc32.ChecksumIEEE(e.Body)
		if e.BadCRC {
			crc ^= 0xDEADBEEF
		}

		uncompSize := uint64(len(e.Body))
		if e.DeclaredSize > 0 {
			uncompSize = uint64(e.DeclaredSize)
		}
		compSize := uint64(len(compressed))

		mode := e.Mode
		if mode == 0 {
			if e.Dir {
				mode = fs.ModeDir | 0o755
			} else {
				mode = 0o644
			}
		}

		fh := zip.FileHeader{
			Name:               e.Name,
			Method:             method,
			Modified:           fixedZipTime,
			CRC32:              crc,
			CompressedSize64:   compSize,
			UncompressedSize64: uncompSize,
		}
		fh.SetMode(mode)
		if e.Encrypted {
			fh.Flags |= 0x1
		}
		if o.ForceZip64 {
			fh.Extra = append(fh.Extra, zip64ExtraField(uncompSize, compSize)...)
		}

		w, err := zw.CreateRaw(&fh)
		if err != nil {
			return fmt.Errorf("worldtest: create raw entry %d (%q): %w", i, e.Name, err)
		}
		if len(compressed) > 0 {
			if _, err := w.Write(compressed); err != nil {
				return fmt.Errorf("worldtest: write raw entry %d (%q): %w", i, e.Name, err)
			}
		}
	}

	if err := zw.Close(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	success = true
	return nil
}

func zip64ExtraField(uncompressedSize, compressedSize uint64) []byte {
	var b [20]byte
	binary.LittleEndian.PutUint16(b[0:2], 0x0001)
	binary.LittleEndian.PutUint16(b[2:4], 16)
	binary.LittleEndian.PutUint64(b[4:12], uncompressedSize)
	binary.LittleEndian.PutUint64(b[12:20], compressedSize)
	return b[:]
}

// ZipTree zips every regular file under srcDir into path with entry names prefixed by prefix.
func ZipTree(path, srcDir, prefix string, o ZipOpts) error {
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	var entries []ZipEntry
	err := filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		entries = append(entries, ZipEntry{
			Name: prefix + filepath.ToSlash(rel),
			Body: body,
		})
		return nil
	})
	if err != nil {
		return err
	}
	return WriteZip(path, entries, o)
}

// BigSparseZip64 writes a genuine ZIP64 archive containing 65536 tiny entries
// so archive/zip emits ZIP64 end-of-central-directory records.
func BigSparseZip64(path string) error {
	return writeTinyZipEntries(path, 65536, false)
}

// ManyEntriesZip writes a zip with a valid w/level.dat plus n tiny files under w/region/.
func ManyEntriesZip(path string, n int) error {
	if n < 0 {
		return errors.New("worldtest: negative entry count")
	}
	return writeTinyZipEntries(path, n, true)
}

func writeTinyZipEntries(path string, n int, includeLevel bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			_ = f.Close()
			_ = os.Remove(path)
		}
	}()

	zw := zip.NewWriter(f)
	if includeLevel {
		levelBytes, err := validLevelDatBytes()
		if err != nil {
			return err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{
			Name:     "w/level.dat",
			Method:   zip.Deflate,
			Modified: fixedZipTime,
		})
		if err != nil {
			return err
		}
		if _, err := w.Write(levelBytes); err != nil {
			return err
		}
	}

	var nameBuf [32]byte
	for i := range n {
		var name string
		if includeLevel {
			name = fmt.Sprintf("w/region/r.%d.0.mca", i)
		} else {
			name = fmt.Sprintf("e/%05d", i)
		}
		_ = nameBuf
		w, err := zw.CreateHeader(&zip.FileHeader{
			Name:     name,
			Method:   zip.Store,
			Modified: fixedZipTime,
		})
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte{0}); err != nil {
			return err
		}
	}

	if err := zw.Close(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	success = true
	return nil
}

// BombZip writes a single-entry Deflate zip of realBytes zero bytes whose header declares 1 KiB.
func BombZip(path string, realBytes int64) error {
	if realBytes < 0 {
		return errors.New("worldtest: negative BombZip size")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var compBuf bytes.Buffer
	fw, err := flate.NewWriter(&compBuf, flate.BestCompression)
	if err != nil {
		return err
	}
	crcHash := crc32.NewIEEE()
	mw := io.MultiWriter(fw, crcHash)

	var zeros [32 * 1024]byte
	rem := realBytes
	for rem > 0 {
		chunk := int64(len(zeros))
		if rem < chunk {
			chunk = rem
		}
		if _, err := mw.Write(zeros[:chunk]); err != nil {
			_ = fw.Close()
			return err
		}
		rem -= chunk
	}
	if err := fw.Close(); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			_ = f.Close()
			_ = os.Remove(path)
		}
	}()

	zw := zip.NewWriter(f)
	compBytes := compBuf.Bytes()
	fh := zip.FileHeader{
		Name:               "w/level.dat",
		Method:             zip.Deflate,
		Modified:           fixedZipTime,
		CRC32:              crcHash.Sum32(),
		CompressedSize64:   uint64(len(compBytes)),
		UncompressedSize64: 1024, // declare 1 KiB
	}
	fh.SetMode(0o644)
	w, err := zw.CreateRaw(&fh)
	if err != nil {
		return err
	}
	if _, err := w.Write(compBytes); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	success = true
	return nil
}

// HostileCase describes a hostile ZIP test case containing a valid small world plus one hostile condition.
type HostileCase struct {
	Name    string
	Entries []ZipEntry
	Why     string
}

// HostileCases returns table-driven hostile archive cases that an importer must reject.
func HostileCases() []HostileCase {
	levelBytes, err := validLevelDatBytes()
	if err != nil {
		panic(err)
	}
	marker := "MARK:minecraft:overworld:T"
	regionBytes := make([]byte, regionHeaderSize+len(marker))
	copy(regionBytes[regionHeaderSize:], marker)

	base := func(extra ...ZipEntry) []ZipEntry {
		out := make([]ZipEntry, 0, 2+len(extra))
		out = append(out,
			ZipEntry{Name: "w/level.dat", Body: levelBytes},
			ZipEntry{Name: "w/region/r.0.0.mca", Body: regionBytes},
		)
		out = append(out, extra...)
		return out
	}

	deepParts := make([]string, 80)
	for i := range deepParts {
		deepParts[i] = fmt.Sprintf("d%02d", i)
	}
	deepPath := "w/" + strings.Join(deepParts, "/") + "/x.txt"

	oneMiBZeros := make([]byte, 1<<20)

	return []HostileCase{
		{
			Name:    "traversal-dotdot",
			Entries: base(ZipEntry{Name: "../x", Body: []byte("evil")}),
			Why:     "reject parent directory traversal ../x",
		},
		{
			Name:    "traversal-nested-dotdot",
			Entries: base(ZipEntry{Name: "w/../../x", Body: []byte("evil")}),
			Why:     "reject nested parent directory traversal w/../../x",
		},
		{
			Name:    "traversal-backslash",
			Entries: base(ZipEntry{Name: `..\x`, Body: []byte("evil")}),
			Why:     "reject backslash traversal ..\\x",
		},
		{
			Name:    "absolute-unix",
			Entries: base(ZipEntry{Name: "/etc/x", Body: []byte("evil")}),
			Why:     "reject absolute Unix path /etc/x",
		},
		{
			Name:    "absolute-drive",
			Entries: base(ZipEntry{Name: "C:/x", Body: []byte("evil")}),
			Why:     "reject Windows drive path C:/x",
		},
		{
			Name:    "drive-relative",
			Entries: base(ZipEntry{Name: "C:x", Body: []byte("evil")}),
			Why:     "reject Windows drive-relative path C:x",
		},
		{
			Name:    "unc-path",
			Entries: base(ZipEntry{Name: "//server/share/x", Body: []byte("evil")}),
			Why:     "reject UNC network path //server/share/x",
		},
		{
			Name:    "ntfs-ads",
			Entries: base(ZipEntry{Name: "w/level.dat:evil", Body: []byte("evil")}),
			Why:     "reject NTFS alternate data stream colon in entry name",
		},
		{
			Name:    "windows-device-con",
			Entries: base(ZipEntry{Name: "w/CON", Body: []byte("evil")}),
			Why:     "reject Windows reserved device name CON",
		},
		{
			Name:    "windows-device-nul",
			Entries: base(ZipEntry{Name: "w/nul.txt", Body: []byte("evil")}),
			Why:     "reject Windows reserved device name nul.txt",
		},
		{
			Name:    "windows-device-com1",
			Entries: base(ZipEntry{Name: "w/COM1.mca", Body: []byte("evil")}),
			Why:     "reject Windows reserved device name COM1.mca",
		},
		{
			Name:    "trailing-dot",
			Entries: base(ZipEntry{Name: "w/region.", Body: []byte("evil")}),
			Why:     "reject path segment with trailing dot",
		},
		{
			Name:    "trailing-space",
			Entries: base(ZipEntry{Name: "w/data ", Body: []byte("evil")}),
			Why:     "reject path segment with trailing space",
		},
		{
			Name:    "case-collision",
			Entries: base(ZipEntry{Name: "w/LEVEL.DAT", Body: []byte("collision")}),
			Why:     "reject entries differing only by ASCII case on Windows",
		},
		{
			Name: "file-dir-conflict",
			Entries: base(
				ZipEntry{Name: "w/a", Body: []byte("file")},
				ZipEntry{Name: "w/a/b", Body: []byte("nested")},
			),
			Why: "reject entry that acts as both file and directory",
		},
		{
			Name:    "symlink",
			Entries: base(ZipEntry{Name: "w/link", Body: []byte("/etc/passwd"), Mode: fs.ModeSymlink | 0o777}),
			Why:     "reject symlink entry inside archive",
		},
		{
			Name:    "deep-nesting",
			Entries: base(ZipEntry{Name: deepPath, Body: []byte("deep")}),
			Why:     "reject archive exceeding path nesting depth limit",
		},
		{
			Name:    "encrypted",
			Entries: base(ZipEntry{Name: "w/secret.txt", Body: []byte("enc"), Encrypted: true}),
			Why:     "reject encrypted archive entry (flag bit 0 set)",
		},
		{
			Name:    "bad-crc",
			Entries: base(ZipEntry{Name: "w/corrupt.txt", Body: []byte("bad"), BadCRC: true}),
			Why:     "reject entry with mismatched CRC-32 checksum",
		},
		{
			Name:    "size-lie",
			Entries: base(ZipEntry{Name: "w/bomb.bin", Body: oneMiBZeros, DeclaredSize: 10}),
			Why:     "reject entry whose decompressed bytes exceed declared size (zip bomb)",
		},
		{
			Name:    "duplicate-entry",
			Entries: base(ZipEntry{Name: "w/region/r.0.0.mca", Body: []byte("duplicate-content")}),
			Why:     "reject duplicate entry names in the same archive",
		},
		{
			Name:    "nul-byte-name",
			Entries: base(ZipEntry{Name: "w/evil\x00.txt", Body: []byte("nul")}),
			Why:     "reject entry name containing NUL byte",
		},
		{
			Name:    "empty-name",
			Entries: base(ZipEntry{Name: "", Body: []byte("empty")}),
			Why:     "reject empty entry name",
		},
		{
			Name:    "slash-only-name",
			Entries: base(ZipEntry{Name: "/", Dir: true}),
			Why:     "reject entry name consisting only of /",
		},
	}
}

func validLevelDatBytes() ([]byte, error) {
	return gzipCompoundBytes(LevelDat(LevelOpts{
		VersionName: "1.20.4",
		DataVersion: 3700,
		LevelName:   "HostileTest",
		GameType:    0,
		Difficulty:  2,
	}))
}
