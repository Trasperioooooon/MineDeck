package modcart

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"minedeck/internal/dl"
	"minedeck/internal/modinfo"
)

// badName 文件名里不许有的字符（路径分隔符、Windows 不允许的字符、控制字符）。
var badName = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

// Download 把文件装进 destDir（mods 或 plugins）：同名文件已经在了就跳过（skipped）；
// 否则先下载到一个不以 .jar 结尾的临时文件（下载途中服务器重启也不会加载它），核对 SHA-1，再改成正式文件名。
// 用真的 Modrinth 时下载地址只认它的 CDN。
func (s *Service) Download(ctx context.Context, f File, destDir string, prog func(done, total int64)) (path string, skipped bool, err error) {
	fn := f.FileName
	if fn == "" || fn != filepath.Base(fn) || badName.MatchString(fn) || strings.HasPrefix(fn, ".") ||
		!strings.HasSuffix(strings.ToLower(fn), ".jar") || !filepath.IsLocal(fn) { // IsLocal 还挡住 Windows 的 NUL、COM1 这类保留名
		return "", false, errorf(502, "Modrinth 给的文件名不对：%q", fn)
	}
	if len(f.SHA1) != 40 {
		return "", false, errorf(502, "Modrinth 没给 %s 的 SHA-1，不下载", fn)
	}
	u, perr := url.Parse(f.URL)
	if perr != nil || (s.official && (u.Scheme != "https" || u.Hostname() != "cdn.modrinth.com")) {
		return "", false, errorf(502, "Modrinth 给的 %s 的下载地址不对", fn)
	}
	dest := filepath.Join(destDir, fn)
	if _, err := os.Stat(dest); err == nil {
		return dest, true, nil
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", false, errorf(500, "建 %s 目录失败：%v", filepath.Base(destDir), err)
	}
	tmp := filepath.Join(destDir, "."+fn+".download")
	defer os.Remove(tmp)
	// MinSize 防的是代理塞回来的错误页；有的小库 jar 不到 1KB，所以不超过 Modrinth 报的大小（反正还要核对 SHA-1）
	minSize := min(int64(1024), max(f.Size, 1))
	if err := dl.Fetch(ctx, dl.Request{URLs: []string{f.URL}, Dest: tmp, SHA1: f.SHA1, MinSize: minSize}, prog); err != nil {
		return "", false, errorf(502, "下载 %s 失败：%v", fn, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", false, errorf(500, "放进 %s 失败（可能正被占用，停服后再试）：%v", filepath.Base(destDir), err)
	}
	return dest, false, nil
}

// ScanLocal 看服务器目录里已经有什么：jar 文件名、modinfo 读出来的模组 ID、按 SHA-1 在 Modrinth 认出来的项目。
// 认项目要联网，失败时只少了这一样（不报错）：文件名和模组 ID 照样能挡住大部分重复下载。
func (s *Service) ScanLocal(ctx context.Context, dir string) Local {
	l := Local{Projects: map[string]string{}}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return l
	}
	hashOf := map[string]string{} // 文件名 → SHA-1
	var hashes []string
	for _, e := range ents { // ReadDir 按文件名排好序了，下面「同一个项目两份 jar 取第一份」结果是固定的
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(n), ".jar") {
			continue // 停用的（.jar.disabled）不算已有：服务器不加载它
		}
		l.Files = append(l.Files, n)
		if h, err := fileSHA1(filepath.Join(dir, n)); err == nil {
			hashOf[n] = h
			if !slices.Contains(hashes, h) {
				hashes = append(hashes, h)
			}
		}
	}
	if mods, err := modinfo.ReadDir(dir); err == nil {
		for _, m := range mods {
			if m.Disabled || m.ID == "" {
				continue
			}
			l.IDs = append(l.IDs, m.ID)
			l.IDs = append(l.IDs, m.Also...)
		}
	}
	if len(hashes) > 0 {
		if vs, err := s.versionFiles(ctx, hashes); err == nil {
			for _, n := range l.Files {
				v, ok := vs[hashOf[n]]
				if _, seen := l.Projects[v.ProjectID]; ok && v.ProjectID != "" && !seen {
					l.Projects[v.ProjectID] = n
				}
			}
		}
	}
	return l
}

func fileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
