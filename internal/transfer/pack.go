package transfer

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/modinfo"
)

// PackOptions 打包一台实例成一个 .mdpack。
type PackOptions struct {
	Dir      string                  // 实例目录
	Instance json.RawMessage         // instance.json 的原文（打进 manifest；目录里的 instance.json / run.bat 条目照默认清单剔除）
	Mode     string                  // handover | distributable，决定默认剔除什么
	Excludes []string                // 额外剔除（相对实例目录的路径，会原样写进 manifest 的剔除清单）
	ModDir   string                  // 模组目录名（mods 或 plugins）；留空按 mods
	Exporter string                  // 导出方版本号（如 "MineDeck 3.0.0"）
	Now      time.Time               // 导出时间；零值用当前时间（测试可注入）
	Progress func(done, total int64) // 进度回调：done 是已读进包的字节数，total 是清点出的总字节数
}

// packItem 清点出的一个待打包文件。
type packItem struct {
	rel  string // 相对实例目录的斜杠路径
	size int64
	mod  time.Time
}

// Pack 打包。返回 manifest（调用方可转成日志或写操作记录）。出错时 dest 可能留了半截，调用方负责删。
func Pack(dest string, o PackOptions) (*Manifest, error) {
	if !ValidMode(o.Mode) {
		return nil, fmt.Errorf("打包模式不认识：%s（可用 handover / distributable）", o.Mode)
	}
	if len(o.Instance) == 0 {
		return nil, fmt.Errorf("instance.json 的原文不能为空")
	}
	if o.ModDir == "" {
		o.ModDir = "mods"
	}
	excludes := append([]string{}, ExcludedFor(o.Mode)...)
	excludes = append(excludes, o.Excludes...)
	excludes, bad := sanitizeExcludes(excludes)
	if bad > 0 {
		return nil, fmt.Errorf("额外剔除的 %d 项写法不对（应为实例目录内的相对路径）", bad)
	}

	world := worldDirs(o.Dir)
	mods, _ := modinfo.ReadDir(filepath.Join(o.Dir, o.ModDir)) // 读不了的 jar 也在清单里（带着 Err），打包不因此失败
	items, total, worldSize, err := survey(o.Dir, excludes, world)
	if err != nil {
		return nil, err
	}

	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	m := &Manifest{
		FormatVersion:   FormatVersion,
		Mode:            o.Mode,
		ExportedAt:      now,
		ExporterVersion: o.Exporter,
		Instance:        append([]byte{}, o.Instance...),
		Mods:            mods,
		WorldSize:       worldSize,
		Excluded:        excludes,
	}
	if err := writePack(dest, m, o.Dir, items, total, o.Progress); err != nil {
		return nil, err
	}
	return m, nil
}

// survey 清点要打包的文件：剔除清单之外的全部文件，连同总字节数与世界存档大小。
func survey(dir string, excludes, world []string) (items []packItem, total, worldSize int64, err error) {
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		raw, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		rel := filepath.ToSlash(raw)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if Excluded(excludes, rel, true) {
				return fs.SkipDir // 整个目录都不要（清点时它还在，打包中途也别再扫进来）
			}
			return nil
		}
		if Excluded(excludes, rel, false) || d.Name() == ManifestFile {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		items = append(items, packItem{rel: rel, size: info.Size(), mod: info.ModTime()})
		total += info.Size()
		if anyPrefix(world, rel) {
			worldSize += info.Size()
		}
		return nil
	})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("清点实例文件失败: %w", err)
	}
	return items, total, worldSize, nil
}

// writePack 写出 zip：第一个条目是 manifest（收包时不用解压整个包就能清点），其后是 survey 出的文件。
func writePack(dest string, m *Manifest, dir string, items []packItem, total int64, progress func(done, total int64)) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	entry := func(name string, mt time.Time, r io.Reader, size int64) error {
		h := &zip.FileHeader{Name: name, Modified: mt}
		h.SetMode(0o644)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, r)
		return err
	}
	var werr error
	if mb, jerr := json.MarshalIndent(m, "", "  "); jerr != nil {
		werr = jerr
	} else {
		werr = entry(ManifestFile, m.ExportedAt, strings.NewReader(string(mb)), int64(len(mb)))
	}
	var done int64
	for _, it := range items {
		if werr != nil {
			break
		}
		r, err := os.Open(filepath.Join(dir, filepath.FromSlash(it.rel)))
		if err != nil {
			werr = err
			break
		}
		err = entry(it.rel, it.mod, r, it.size)
		r.Close()
		if err != nil {
			werr = err
			break
		}
		done += it.size
		if progress != nil {
			progress(done, total)
		}
	}
	if cerr := zw.Close(); werr == nil {
		werr = cerr
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("压缩打包失败: %w", werr)
	}
	return nil
}

// worldDirs 返回实例的世界存档目录名（相对实例目录）：按 server.properties 的 level-name 算，
// 连同 _nether、_the_end（bukkit 系的三目录），存在的才算。打包与收包双方都用这一条规则，
// 所以 worldSize 对得上。
func worldDirs(dir string) []string {
	level := "world"
	if b, err := os.ReadFile(filepath.Join(dir, "server.properties")); err == nil {
		if v := propsValue(b, "level-name"); v != "" {
			level = v
		}
	}
	var out []string
	for _, d := range []string{level, level + "_nether", level + "_the_end"} {
		if st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(d))); err == nil && st.IsDir() {
			out = append(out, d)
		}
	}
	return out
}

func anyPrefix(prefixes []string, rel string) bool {
	for _, p := range prefixes {
		if p == rel || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// sanitizeExcludes 把剔除清单里危险或空白的项清掉，返回清理后的清单与拒绝的项数。
// 清单是要写进 manifest、又拿去和文件路径匹配的：..、绝对路径、带盘符的写法在这里挡下。
func sanitizeExcludes(excludes []string) ([]string, int) {
	out := make([]string, 0, len(excludes))
	bad := 0
	for _, e := range excludes {
		e = strings.Trim(strings.TrimSpace(e), "/")
		if e == "" {
			bad++
			continue
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(e)))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") ||
			filepath.IsAbs(e) || strings.Contains(e, ":") {
			bad++
			continue
		}
		out = append(out, clean)
	}
	return out, bad
}
