package modcart

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"minedeck/internal/modinfo"
	"minedeck/internal/winname"
	"minedeck/internal/zipguard"
)

// ---- 人从自己电脑上添加的 jar（本地文件）----
//
// 不在 Modrinth 上的、自己下载好的模组或插件，可以直接放进待装清单：先暂存在 <程序目录>/data/modcart/local/<车>/ 里，
// 安装时拷进服务器的 mods / plugins。和 Modrinth 项目列在同一张清单里，两样合计不超过 MaxItems。
//
// 几条规矩：
//   - 暂存的文件按内容的 SHA-1 起名（<sha1>.jar），人给的文件名只登记在 cart.json 里，不拿来拼暂存路径；
//     每辆车一个文件夹，文件夹名是车的键的哈希（实例名里什么字符都可能有，不直接拼进路径）。
//   - 文件名在登记时、拷进服务器时各查一遍（CheckLocalName）：必须是单独一段、以 .jar 结尾、不带控制字符和 Windows 不许的字符。
//   - 同一个文件（哈希相同）再加一次不报错、不重复存；文件名相同内容不同的拒绝。
//   - 给界面和 AI 的结果里只有文件名、大小和读出来的模组信息，没有路径；报错里也不带路径。
//
// 谁能加、谁能删由操作层管（只许人，见 ops/catalog/modcart.go）。

// MaxLocalSize 单个本地文件最大多少字节（256 MiB：模组 jar 一般几 MB，带资源的大型模组也就一两百 MB）。
const MaxLocalSize = 256 << 20

// maxLocalSize 实际用的上限（测试里改小）。
var maxLocalSize int64 = MaxLocalSize

// 文件名长度上限：字数和字节数各卡一道（中文一个字 3 字节）。给 AI 的清单有 16KB 上限，名字不能太长。
const (
	maxLocalNameRunes = 100
	maxLocalNameBytes = 200
)

// LocalFile 清单里的一个本地文件。Title、ModID、Version、Loader 是添加时用 modinfo 从 jar 里读出来的。
type LocalFile struct {
	SHA1     string    `json:"sha1"`     // 文件内容的 SHA-1（小写十六进制），也是它在清单里的编号
	FileName string    `json:"fileName"` // 人给的文件名：装进服务器时用这个名字
	Size     int64     `json:"size"`
	Title    string    `json:"title"`             // 模组或插件的名称；读不出来就是文件名
	ModID    string    `json:"modId,omitempty"`   // 模组 ID（插件是插件名）
	Version  string    `json:"version,omitempty"` // jar 自己写的版本
	Loader   string    `json:"loader"`            // modinfo 认出来的加载器：fabric / forge / neoforge / neoforge+forge / quilt / plugin / unknown
	AddedAt  time.Time `json:"addedAt"`

	inst string // 它在哪辆车里（List 时填上，不存盘）：安装时可能把草稿车的和实例的并在一起，拷完要知道从哪辆车里拿掉
}

// CheckLocalName 本地文件的名字能不能用：登记时、拷进服务器时各查一遍。
func CheckLocalName(name string) error {
	short := name
	if r := []rune(short); len(r) > 40 {
		short = string(r[:40]) + "…"
	}
	switch {
	case name == "":
		return errorf(400, "这个文件没有名字")
	case !utf8.ValidString(name):
		return errorf(400, "文件名里有认不出的字符，改个名字再添加")
	case utf8.RuneCountInString(name) > maxLocalNameRunes || len(name) > maxLocalNameBytes:
		return errorf(400, "文件名太长了（最多 %d 个字），改短一点再添加：%s", maxLocalNameRunes, short)
	case !strings.HasSuffix(strings.ToLower(name), ".jar"):
		return errorf(400, "只能添加 .jar 文件：%s", short)
	case name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || name == "." || name == ".." ||
		strings.HasPrefix(name, ".") || badName.MatchString(name) || !filepath.IsLocal(name) || !winname.OK(name):
		return errorf(400, "文件名里有不能用的字符（比如 / \\ : * ? \" < > |），改个名字再添加：%s", short)
	case shortAlias.MatchString(name):
		// 长得像 Windows 的 8.3 短文件名（PROGRA~1.jar）：开着短文件名的盘上，它会指到目录里另一个长名字的文件
		return errorf(400, "「%s」这种名字（几个字符加 ~ 和数字）在 Windows 上可能指到别的文件，改个名字再添加", short)
	}
	for _, r := range name {
		// 控制字符（含 \x7f 和 C1 区）、看不见的排版字符（如从右到左覆盖符，能把 exe 伪装成 jar 的样子）
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return errorf(400, "文件名里有看不见的特殊字符，改个名字再添加")
		}
	}
	return nil
}

// shortAlias 长得像 8.3 短文件名的名字：最多 6 个字符，接 ~ 和数字（PROGRA~1.jar）。
var shortAlias = regexp.MustCompile(`(?i)^[^.~]{1,6}~[0-9]{1,3}\.jar$`)

// noPath 去掉系统错误里的路径（*PathError、*LinkError 的 Error() 带着完整路径），只留原因。
func noPath(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	return err
}

// bucket 一辆车的暂存文件夹名：草稿车是 draft，实例的车是实例名的哈希。
func bucket(inst string) string {
	if inst == Draft {
		return "draft"
	}
	sum := sha256.Sum256([]byte(inst))
	return "i-" + hex.EncodeToString(sum[:12])
}

func (s *Service) stagedRoot() string           { return filepath.Join(s.dir, "local") }
func (s *Service) stagedDir(inst string) string { return filepath.Join(s.stagedRoot(), bucket(inst)) }

// stagedPath 暂存文件的路径。sha1 进来之前都验过是 40 位十六进制。
func (s *Service) stagedPath(inst, sha1 string) string {
	return filepath.Join(s.stagedDir(inst), sha1+".jar")
}

func validSHA1(h string) bool {
	if len(h) != 40 {
		return false
	}
	return !strings.ContainsFunc(h, func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') })
}

// checkedLocals 读回 cart.json 里登记的本地文件：登记得不对的（哈希、文件名）、暂存文件已经不在的，不要。
func (s *Service) checkedLocals(in map[string][]LocalFile) map[string][]LocalFile {
	out := map[string][]LocalFile{}
	for inst, list := range in {
		var keep []LocalFile
		for _, lf := range list {
			if !validSHA1(lf.SHA1) || CheckLocalName(lf.FileName) != nil ||
				slices.ContainsFunc(keep, func(x LocalFile) bool { return x.SHA1 == lf.SHA1 }) {
				continue
			}
			if st, err := os.Stat(s.stagedPath(inst, lf.SHA1)); err != nil || !st.Mode().IsRegular() {
				continue
			}
			keep = append(keep, lf)
		}
		if len(keep) > 0 {
			out[inst] = keep
		}
	}
	return out
}

// sweepStaged 启动时清一遍暂存目录：没有登记的文件（添加到一半程序退了、车已经清了）和不属于任何一辆车的文件夹删掉。
// 只动 <dir>/local/ 里面的东西。
func (s *Service) sweepStaged() {
	ents, err := os.ReadDir(s.stagedRoot())
	if err != nil {
		return
	}
	want := map[string]map[string]bool{} // 文件夹名 → 该留的文件名
	for inst, list := range s.locals {
		keep := map[string]bool{}
		for _, lf := range list {
			keep[lf.SHA1+".jar"] = true
		}
		want[bucket(inst)] = keep
	}
	for _, e := range ents {
		dir := filepath.Join(s.stagedRoot(), e.Name())
		keep, ok := want[e.Name()]
		if !ok || !e.IsDir() {
			_ = os.RemoveAll(dir)
			continue
		}
		files, _ := os.ReadDir(dir)
		for _, f := range files {
			if !keep[f.Name()] {
				_ = os.RemoveAll(filepath.Join(dir, f.Name()))
			}
		}
	}
}

// Locals 一辆车里的本地文件（按加入的先后）。
func (s *Service) Locals(inst string) []LocalFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.localsLocked(inst)
}

func (s *Service) localsLocked(inst string) []LocalFile {
	out := slices.Clone(s.locals[inst])
	for i := range out {
		out[i].inst = inst
	}
	return out
}

// cleanText 模组自己写的名称、版本这类文字：去掉控制字符和看不见的排版字符，截到 max 个字。
func cleanText(v string, max int) string {
	v = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, v))
	if r := []rune(v); len(r) > max {
		v = string(r[:max])
	}
	return v
}

// AddLocal 把 src 这个 jar 放进 inst 的车里，名字记成 name。src 是调用方准备好的临时文件：
// 放进去以后它被挪走；没放进去（报错，或者同一个文件已经在车里）时它还在原处，由调用方删。
// 查的东西：文件名（CheckLocalName）、大小（不超过 MaxLocalSize）、是不是能打开的 zip、车里放不放得下。
// 同一个文件（哈希相同）已经在车里：dup=true，返回车里那一条，不重复存。文件名相同内容不同：409。
func (s *Service) AddLocal(inst, src, name string) (lf LocalFile, dup bool, err error) {
	if err := CheckLocalName(name); err != nil {
		return lf, false, err
	}
	st, err := os.Stat(src)
	if err != nil || !st.Mode().IsRegular() {
		return lf, false, errorf(400, "没有收到「%s」这个文件，再添加一次", name)
	}
	switch {
	case st.Size() == 0:
		return lf, false, errorf(400, "「%s」是空文件（可能没下载完整），换一份再添加", name)
	case st.Size() > maxLocalSize:
		return lf, false, errorf(413, "「%s」超过了 %d MB 的上限，放不进清单。可以等服务器建好以后，自己把它拷进服务器的文件夹", name, MaxLocalSize>>20)
	}
	zr, err := zipguard.Open(src, zipguard.Default)
	if err != nil {
		return lf, false, errorf(400, "「%s」打不开：它不是一个完整的 jar 文件（可能没下载完整，或者其实是别的格式）。重新下载一份再添加", name)
	}
	zr.Close()
	sum, err := fileSHA1(src)
	if err != nil {
		return lf, false, errorf(500, "读「%s」失败：%v", name, noPath(err))
	}
	info := modinfo.Read(src)
	lf = LocalFile{SHA1: sum, FileName: name, Size: st.Size(), Title: cleanText(orID(info.Name, info.ID), 80),
		ModID: cleanText(info.ID, 100), Version: cleanText(info.Version, 60), Loader: info.Loader, AddedAt: time.Now().UTC(), inst: inst}
	if lf.Title == "" {
		lf.Title = name
	}
	if lf.Loader == "" {
		lf.Loader = "unknown"
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.locals[inst]
	for _, x := range list {
		if x.SHA1 == sum {
			x.inst = inst
			return x, true, nil
		}
	}
	for _, x := range list {
		if strings.EqualFold(x.FileName, name) { // Windows 上文件名不分大小写
			return lf, false, errorf(409, "清单里已经有一个叫「%s」的文件，内容和这个不一样。想换成这一份，先把清单里那个移除，再添加一次", x.FileName)
		}
	}
	if n := len(s.carts[inst]) + len(list); n >= MaxItems {
		return lf, false, errorf(400, "待装清单最多放 %d 项，现在已经有 %d 项", MaxItems, n)
	}
	if err := os.MkdirAll(s.stagedDir(inst), 0o755); err != nil {
		return lf, false, errorf(500, "暂存「%s」失败：%v", name, noPath(err))
	}
	dest := s.stagedPath(inst, sum)
	_ = os.Remove(dest) // 上次添加到一半留下的
	if err := moveFile(src, dest); err != nil {
		return lf, false, errorf(500, "暂存「%s」失败：%v", name, noPath(err))
	}
	if err := s.putLocalsLocked(inst, append(slices.Clone(list), lf)); err != nil {
		os.Remove(dest)
		return lf, false, err
	}
	return lf, false, nil
}

// moveFile 把文件挪过去：同一个盘直接改名，不在一个盘就拷过去再删原来的。
func moveFile(src, dest string) error {
	if os.Rename(src, dest) == nil {
		return nil
	}
	if _, err := copyFile(src, dest); err != nil {
		os.Remove(dest)
		return err
	}
	os.Remove(src)
	return nil
}

// copyFile 拷一个文件，返回拷过去的内容的 SHA-1。dest 已经存在时报错（不覆盖）。
func copyFile(src, dest string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	h := sha1.New()
	_, err = io.Copy(io.MultiWriter(out, h), in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// RemoveLocal 从车里拿掉一个本地文件（按 SHA-1），暂存的文件一起删。车里没有它也不报错。返回车里剩下的本地文件。
func (s *Service) RemoveLocal(inst, sum string) ([]LocalFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := slices.DeleteFunc(slices.Clone(s.locals[inst]), func(x LocalFile) bool { return x.SHA1 == sum })
	if len(list) == len(s.locals[inst]) {
		return s.localsLocked(inst), nil
	}
	if err := s.putLocalsLocked(inst, list); err != nil {
		return nil, err
	}
	if validSHA1(sum) {
		_ = os.Remove(s.stagedPath(inst, sum))
	}
	return s.localsLocked(inst), nil
}

// ClearAll 把一辆车整个清掉：Modrinth 项目、本地文件的登记和暂存的文件。人在界面上点「清空」、删除服务器时用。
func (s *Service) ClearAll(inst string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.carts[inst]) > 0 || len(s.locals[inst]) > 0 {
		carts, locals := without(s.carts, inst), without(s.locals, inst)
		if err := s.write(carts, locals); err != nil {
			return err
		}
		s.carts, s.locals = carts, locals
	}
	_ = os.RemoveAll(s.stagedDir(inst)) // 登记先清，文件后删：中途断了也只是多出几个没登记的文件，下次启动会清
	return nil
}

// without 复制一份去掉 key 的 map。
func without[V any](m map[string]V, key string) map[string]V {
	out := make(map[string]V, len(m))
	for k, v := range m {
		if k != key {
			out[k] = v
		}
	}
	return out
}

// LocalFingerprint 清单里有哪些本地文件的指纹（按内容，不看顺序）。和 Fingerprint 一样给安装提案当前置条件。
func LocalFingerprint(files []LocalFile) string {
	ids := make([]string, 0, len(files))
	for _, f := range files {
		ids = append(ids, f.SHA1)
	}
	slices.Sort(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(sum[:8])
}

// MergeLocal 合并几辆车的本地文件（同一个文件只留先出现的）：装实例的车时可以把草稿车一起带上。
func MergeLocal(lists ...[]LocalFile) []LocalFile {
	var out []LocalFile
	for _, l := range lists {
		for _, f := range l {
			if !slices.ContainsFunc(out, func(x LocalFile) bool { return x.SHA1 == f.SHA1 }) {
				out = append(out, f)
			}
		}
	}
	return out
}

// 把本地文件拷进服务器目录的结果。
const (
	LocalCopied  = "copied"  // 拷进去了
	LocalSame    = "same"    // 目录里已经有同名文件，内容一样：不用拷
	LocalDiffers = "differs" // 目录里已经有同名文件，内容不一样：不覆盖
)

// InstallLocal 把一个暂存的本地文件拷进 destDir（mods 或 plugins），文件名用登记的那个。
// 目录里已经有同名文件：内容一样算装好了（LocalSame），不一样不覆盖（LocalDiffers）。
// 拷的时候先写到不以 .jar 结尾的临时文件，核对内容的 SHA-1 和登记的一致，再改成正式文件名。
func (s *Service) InstallLocal(lf LocalFile, destDir string) (status string, err error) {
	if err := CheckLocalName(lf.FileName); err != nil {
		return "", err
	}
	if !validSHA1(lf.SHA1) {
		return "", errorf(500, "「%s」的登记信息不对，从清单里移除后重新添加", lf.FileName)
	}
	src := s.stagedPath(lf.inst, lf.SHA1)
	if st, err := os.Stat(src); err != nil || !st.Mode().IsRegular() {
		return "", errorf(404, "暂存的「%s」找不到了，重新添加一次", lf.FileName)
	}
	dest := filepath.Join(destDir, lf.FileName)
	// 已经有同名文件：内容一样算装好了，不一样不覆盖
	existing := func() string {
		if have, herr := fileSHA1(dest); herr == nil && have == lf.SHA1 {
			return LocalSame
		}
		return LocalDiffers
	}
	if _, err := os.Lstat(dest); err == nil {
		return existing(), nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		// 看不清那里有没有同名文件（权限、占用）：不往下走，免得把已有的文件盖掉
		return "", errorf(500, "看不了 %s 里有没有叫「%s」的文件，没有装：%v", filepath.Base(destDir), lf.FileName, noPath(err))
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", errorf(500, "创建 %s 文件夹失败：%v", filepath.Base(destDir), noPath(err))
	}
	tmp := filepath.Join(destDir, "."+lf.FileName+".copy")
	_ = os.Remove(tmp)
	defer os.Remove(tmp)
	got, err := copyFile(src, tmp)
	if err != nil {
		return "", errorf(500, "把「%s」拷进 %s 失败：%v", lf.FileName, filepath.Base(destDir), noPath(err))
	}
	if got != lf.SHA1 {
		return "", errorf(500, "暂存的「%s」内容变了（和添加时不一样），没有装。从清单里移除后重新添加", lf.FileName)
	}
	taken, err := placeNoReplace(tmp, dest)
	if err != nil {
		return "", errorf(500, "把「%s」放进 %s 失败（可能正被占用，停服后再试）：%v", lf.FileName, filepath.Base(destDir), noPath(err))
	}
	if taken { // 拷的这会儿，那里冒出来一个同名文件（别的安装、人手放的）：照「已经有同名文件」办，不覆盖
		return existing(), nil
	}
	return LocalCopied, nil
}

// placeNoReplace 把 tmp 改名成 dest，dest 已经存在时不动它、返回 taken=true。
// 不能直接 os.Rename：Windows 上它会把已有的 dest 换掉，而前面查「有没有同名文件」和这里之间隔着一整次拷贝。
// 用硬链接来放：链接是原子的，目标已经存在就失败。盘不支持硬链接（FAT、exFAT、有的网络盘）时退回改名，改名前再看一眼。
func placeNoReplace(tmp, dest string) (taken bool, err error) {
	if err := os.Link(tmp, dest); err == nil {
		_ = os.Remove(tmp)
		return false, nil
	} else if errors.Is(err, fs.ErrExist) {
		return true, nil
	}
	if _, err := os.Lstat(dest); err == nil {
		return true, nil
	}
	return false, os.Rename(tmp, dest)
}

// DropLocal 装好以后把这个本地文件从它所在的那辆车里拿掉（暂存的文件一起删）。
func (s *Service) DropLocal(lf LocalFile) error {
	_, err := s.RemoveLocal(lf.inst, lf.SHA1)
	return err
}

// readStaged 用 modinfo 读暂存的文件（依赖、模组 ID 都以文件里现在写的为准）。
func (s *Service) readStaged(lf LocalFile) modinfo.Mod {
	return modinfo.Read(s.stagedPath(lf.inst, lf.SHA1))
}

// 加载器的叫法（给人看）。
var loaderNames = map[string]string{"fabric": "Fabric", "quilt": "Quilt", "forge": "Forge", "neoforge": "NeoForge",
	"paper": "Paper", "spigot": "Spigot", "bukkit": "Bukkit", "purpur": "Purpur", "folia": "Folia",
	"velocity": "Velocity", "waterfall": "Waterfall", "bungeecord": "BungeeCord"}

func loaderName(l string) string {
	if n, ok := loaderNames[l]; ok {
		return n
	}
	return l
}

// isModLoaders 这组 Modrinth 加载器是不是模组服的（否则是插件服、代理端）。
func isModLoaders(loaders []string) bool {
	return slices.ContainsFunc(loaders, func(l string) bool { return l == "fabric" || l == "forge" || l == "neoforge" || l == "quilt" })
}

// serverName 「Forge」「插件服」「Velocity 代理端」这样的说法：这台服务器是什么。
func serverName(loaders []string) string {
	switch {
	case isModLoaders(loaders):
		return loaderName(loaders[0])
	case loaders[0] == "velocity" || loaders[0] == "waterfall":
		return loaderName(loaders[0]) + " 代理端"
	}
	return "插件服"
}

// LoaderWarn 按 jar 自己的描述文件看它能不能用在这台服务器上（loaders、mc 同 Search）：不合适时返回一句给人看的提醒，合适返回空串。
// 只是提醒，不拦：有的模组一个 jar 通吃几种加载器，有的服务端能兼容别家的模组。
func LoaderWarn(modLoader string, loaders []string, mc string) string {
	if len(loaders) == 0 {
		return ""
	}
	target := serverName(loaders)
	switch modLoader {
	case "", "unknown":
		return "没读出这个文件是什么（里面没有模组或插件的说明），没法帮你检查前置。照样会放进服务器。"
	case "plugin":
		if isModLoaders(loaders) {
			return "这是插件，这台服务器装的是 " + target + " 模组，放进去多半不会生效。"
		}
		return ""
	}
	// 剩下的都是模组
	if !isModLoaders(loaders) {
		return "这是 " + modLoaderName(modLoader) + " 模组，这台服务器是" + target + "，放进去不会生效。"
	}
	ok := false
	switch loaders[0] {
	case "fabric":
		ok = modLoader == "fabric"
	case "forge":
		ok = modLoader == "forge" || modLoader == "neoforge+forge"
	case "neoforge":
		// NeoForge 1.20.5 起才只认 neoforge.mods.toml，之前的版本读的也是 mods.toml，分不出来就不提醒
		ok = modLoader == "neoforge" || modLoader == "neoforge+forge" ||
			(modLoader == "forge" && (mc == "" || compareVersion(mc, "1.20.5") < 0))
	case "quilt":
		ok = modLoader == "quilt" || modLoader == "fabric"
	}
	if ok {
		return ""
	}
	return "这个文件是给 " + modLoaderName(modLoader) + " 用的，这台服务器是 " + target + "，装上多半不能用。"
}

func modLoaderName(l string) string {
	if l == "neoforge+forge" {
		return "Forge / NeoForge"
	}
	return loaderName(l)
}
