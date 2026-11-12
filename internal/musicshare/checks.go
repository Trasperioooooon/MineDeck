package musicshare

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"minedeck/internal/bridge"
	"minedeck/internal/mcsrc"
	"minedeck/internal/music"
)

// ---- 核心和版本 ----

// supported 桥接模组只有 Forge 系的元数据（mods.toml、neoforge.mods.toml），Fabric 版还没做。
func supported(core mcsrc.Core) bool {
	return core == mcsrc.CoreNeoForge || core == mcsrc.CoreForge || core == mcsrc.CoreMohist
}

// Applies 这个核心有没有「音乐共享」（界面据此决定显不显示这个标签）。
func Applies(core mcsrc.Core) bool { return supported(core) }

// supportOf 核心和版本支不支持；不支持时给出原因。
func supportOf(core mcsrc.Core, mc string) (bool, string) {
	switch core {
	case mcsrc.CoreNeoForge:
		if versionLess(mc, "1.20.5") {
			return false, "桥接模组要 NeoForge 1.20.5 或更新（这台是 " + mc + "）：更早的 NeoForge 读的是旧的 mods.toml，模组会加载失败"
		}
		return true, ""
	case mcsrc.CoreForge, mcsrc.CoreMohist:
		if versionLess(mc, "1.20.1") {
			return false, "桥接模组要 Forge 1.20.1 或更新（这台是 " + mc + "）：更早的 Forge 没有它用的加载方式"
		}
		return true, ""
	case mcsrc.CoreFabric, mcsrc.CoreBanner:
		return false, "Fabric 版的桥接模组还没做，现在只支持 NeoForge 和 Forge 服"
	}
	return false, "音乐共享要用 Simple Voice Chat 和桥接模组，只支持 NeoForge、Forge 服"
}

// versionLess 比较 Minecraft 版本号（1.20.1、1.21、26.1 这类，按数字逐段比；快照等后缀忽略）。
func versionLess(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < max(len(pa), len(pb)); i++ {
		x, y := 0, 0
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

var leadingNum = regexp.MustCompile(`^\d+`)

func versionParts(v string) []int {
	var out []int
	for _, p := range strings.Split(v, ".") {
		m := leadingNum.FindString(p)
		if m == "" {
			break
		}
		n, _ := strconv.Atoi(m)
		out = append(out, n)
		if len(m) != len(p) {
			break
		}
	}
	return out
}

// ---- Simple Voice Chat ----

// SVCInfo mods 里的 Simple Voice Chat。
type SVCInfo struct {
	Installed bool     `json:"installed"`
	File      string   `json:"file,omitempty"`
	Version   string   `json:"version,omitempty"`  // 模组版本，如 1.21.1-2.6.27
	Loader    string   `json:"loader,omitempty"`   // neoforge / forge / fabric：从哪份元数据认出来的
	Disabled  []string `json:"disabled,omitempty"` // 被停用的（改成了 .disabled）
}

type svcEntry struct {
	size    int64
	mod     time.Time
	isSVC   bool
	version string
	loader  string
}

// detectSVC 读 mods 里每个 jar 的元数据找模组 ID voicechat（不看文件名：整合包里可能改过名）。
// 结果按文件大小和修改时间缓存；名字里带 voice 的先看。
func (s *Service) detectSVC(dir string) SVCInfo {
	var out SVCInfo
	mods := modsDir(dir)
	ents, err := os.ReadDir(mods)
	if err != nil {
		return out
	}
	var first, rest []os.DirEntry
	for _, e := range ents {
		n := strings.ToLower(e.Name())
		switch {
		case e.IsDir():
		case strings.HasSuffix(n, ".jar.disabled") && strings.Contains(n, "voicechat"):
			out.Disabled = append(out.Disabled, e.Name())
		case !strings.HasSuffix(n, ".jar") || strings.HasPrefix(n, "minedeck-voicebridge"):
		case strings.Contains(n, "voice"):
			first = append(first, e)
		default:
			rest = append(rest, e)
		}
	}
	for _, e := range append(first, rest...) {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(mods, e.Name())
		s.mu.Lock()
		ce, ok := s.svcCache[path]
		s.mu.Unlock()
		if !ok || ce.size != fi.Size() || !ce.mod.Equal(fi.ModTime()) {
			ce = svcEntry{size: fi.Size(), mod: fi.ModTime()}
			ce.isSVC, ce.version, ce.loader = scanJarForSVC(path)
			s.mu.Lock()
			s.svcCache[path] = ce
			s.mu.Unlock()
		}
		if ce.isSVC {
			out.Installed, out.File, out.Version, out.Loader = true, e.Name(), ce.version, ce.loader
			return out
		}
	}
	return out
}

// scanJarForSVC 看一个 jar 是不是 Simple Voice Chat（模组 ID voicechat），顺便取版本号。
func scanJarForSVC(path string) (bool, string, string) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, "", ""
	}
	defer zr.Close()
	read := func(name string) []byte {
		for _, f := range zr.File {
			if f.Name == name {
				rc, err := f.Open()
				if err != nil {
					return nil
				}
				defer rc.Close()
				b, _ := io.ReadAll(io.LimitReader(rc, 1<<20))
				return b
			}
		}
		return nil
	}
	manifestVersion := func() string {
		for _, l := range strings.Split(string(read("META-INF/MANIFEST.MF")), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), "Implementation-Version:"); ok {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	for _, m := range []struct{ file, loader string }{{"META-INF/neoforge.mods.toml", "neoforge"}, {"META-INF/mods.toml", "forge"}} {
		if b := read(m.file); b != nil {
			if v, ok := tomlModVersion(b, "voicechat"); ok {
				if strings.Contains(v, "${") {
					v = manifestVersion()
				}
				return true, v, m.loader
			}
		}
	}
	if b := read("fabric.mod.json"); b != nil {
		var fm struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		}
		if json.Unmarshal(b, &fm) == nil && fm.ID == "voicechat" {
			return true, fm.Version, "fabric"
		}
	}
	return false, "", ""
}

var tomlKV = regexp.MustCompile(`^\s*([A-Za-z_]+)\s*=\s*["']([^"']*)["']`)

// tomlModVersion 在 mods.toml 的 [[mods]] 里找 modId，返回它的 version（只认这两个键，够用了）。
func tomlModVersion(b []byte, modID string) (string, bool) {
	in, id, ver := false, "", ""
	flush := func() (string, bool) { return ver, in && id == modID }
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			if v, ok := flush(); ok {
				return v, true
			}
			in, id, ver = line == "[[mods]]", "", ""
			continue
		}
		if m := tomlKV.FindStringSubmatch(line); m != nil && in {
			switch m[1] {
			case "modId":
				id = m[2]
			case "version":
				ver = m[2]
			}
		}
	}
	return flush()
}

// ---- SVC 的服务端配置（语音端口、voice_host）----

const svcConfigRel = "config/voicechat/voicechat-server.properties"

// VoiceConfig SVC 服务端配置里和音乐共享有关的两项。
type VoiceConfig struct {
	Found     bool   `json:"found"`     // 文件存在（SVC 第一次启动后才生成）
	Port      int    `json:"port"`      // 语音的 UDP 端口；-1 表示和 MC 服务器同一个端口
	VoiceHost string `json:"voiceHost"` // 告诉客户端连哪个地址（穿透时要填）
	Path      string `json:"path"`      // 相对实例目录
}

func svcConfigPath(dir string) string { return filepath.Join(dir, filepath.FromSlash(svcConfigRel)) }

func readVoiceConfig(dir string) VoiceConfig {
	vc := VoiceConfig{Port: 24454, Path: svcConfigRel}
	p, err := bridge.LoadProps(svcConfigPath(dir))
	if err != nil {
		return vc
	}
	vc.Found = true
	if v, ok := p.Get("port"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			vc.Port = n
		}
	}
	v, _ := p.Get("voice_host")
	vc.VoiceHost = strings.TrimSpace(v)
	return vc
}

// ---- 远程语音：语音隧道和 voice_host ----

// RemoteTunnel 绑定到实例的一条语音隧道。
type RemoteTunnel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Running    bool   `json:"running"`
	AutoStart  bool   `json:"autoStart"`
	PublicAddr string `json:"publicAddr,omitempty"`
	LastError  string `json:"lastError,omitempty"`
	LocalPort  int    `json:"localPort,omitempty"` // 只有自定义 frps 的有意义：应该等于 SVC 的端口
}

// RemoteView 远程好友的语音。
type RemoteView struct {
	Tunnels []RemoteTunnel `json:"tunnels"`        // 绑定到这个实例、用途为语音的隧道
	Addr    string         `json:"addr,omitempty"` // 语音隧道的公网地址
	// State none：没有语音隧道（只有局域网好友就不用管）；waiting：隧道还没拿到公网地址；
	// same：voice_host 已经是隧道地址；pending：SVC 的配置还没生成，开一次服后自动写；
	// differ：voice_host 是别的值（可能是用户自己填的），不自动覆盖，让用户选；
	// idle：有地址，但隧道停着、开服时也不会跟着拉起（地址是上次的），voice_host 空着或者还是 MineDeck 写的那个。
	State   string `json:"state"`
	Managed bool   `json:"managed"` // voice_host 是 MineDeck 写的
}

// voiceTunnels 绑定到实例、用途为语音的隧道。
func (s *Service) voiceTunnels(name string) []RemoteTunnel {
	var out []RemoteTunnel
	if s.d.Tunnels == nil {
		return out
	}
	for _, t := range s.d.Tunnels.List() {
		if t.IsVoice() && t.Bound == name {
			rt := RemoteTunnel{ID: t.ID, Name: t.Name, Provider: t.Provider, Running: t.Running, AutoStart: t.AutoStart, PublicAddr: t.PublicAddr, LastError: t.LastError}
			if t.Provider == "custom" {
				rt.LocalPort = t.LocalPort
			}
			out = append(out, rt)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Running && !out[j].Running })
	return out
}

// liveTunnels 能用的语音隧道：正在运行的；followable（服务器停着、马上要开或者刚开）时，勾了跟随实例启动的也算——会跟着拉起来。
func liveTunnels(ts []RemoteTunnel, followable bool) []RemoteTunnel {
	var out []RemoteTunnel
	for _, t := range ts {
		if t.Running || (followable && t.AutoStart) {
			out = append(out, t)
		}
	}
	return out
}

// tunnelAddr 语音隧道的公网地址（优先正在运行的）。
func tunnelAddr(ts []RemoteTunnel) string {
	for _, t := range ts {
		if t.PublicAddr != "" {
			return t.PublicAddr
		}
	}
	return ""
}

func (s *Service) remoteView(name string, vc VoiceConfig) RemoteView {
	rv := RemoteView{Tunnels: s.voiceTunnels(name)}
	rv.Addr = tunnelAddr(rv.Tunnels)
	s.mu.Lock()
	written := ""
	if is := s.set.Instances[name]; is != nil {
		written = is.VoiceHostWritten
	}
	s.mu.Unlock()
	rv.Managed = vc.VoiceHost != "" && vc.VoiceHost == written
	followable := true // 跟随启动的隧道还会跟着拉起来：服务器停着，或者刚开、隧道还没轮到（开服 2 秒后才拉）
	if info, err := s.d.Instances.Info(name); err == nil {
		followable = info.Status == "stopped" || info.Status == "starting"
	}
	switch {
	case len(rv.Tunnels) == 0:
		rv.State = "none"
	case rv.Addr == "":
		rv.State = "waiting"
	case tunnelAddr(liveTunnels(rv.Tunnels, followable)) == "" && (vc.VoiceHost == "" || rv.Managed):
		rv.State = "idle" // 隧道停着（地址是上次的）：不能说「远程好友连服后就能听到」（测试报告 G-07）
	case !vc.Found:
		rv.State = "pending"
	case vc.VoiceHost == rv.Addr:
		rv.State = "same"
	case vc.VoiceHost == "" || vc.VoiceHost == written:
		rv.State = "pending" // 下次开服前写（运行中 tunnel.up 时就写了；走到这里多半是还没轮到）
	default:
		rv.State = "differ"
	}
	return rv
}

// syncVoiceHost 把语音隧道的公网地址写进 voice_host：只在 voice_host 为空、或者还是 MineDeck 上次写的值时写；
// 用户自己填了别的就不覆盖（界面上让用户选）。改之前把原文件备份成 .bak。starting 为真表示马上要开服：
// 这时勾了跟随实例启动的隧道也算数；一条能用的都没有，就把 MineDeck 上次写的地址清回空。
func (s *Service) syncVoiceHost(name string, starting bool) {
	info, err := s.d.Instances.Info(name)
	if err != nil {
		return
	}
	addr := tunnelAddr(liveTunnels(s.voiceTunnels(name), starting))
	if addr == "" && !starting {
		return
	}
	path := svcConfigPath(info.Dir)
	p, err := bridge.LoadProps(path)
	if err != nil {
		return // SVC 还没生成配置：开过一次服之后再写
	}
	cur, _ := p.Get("voice_host")
	cur = strings.TrimSpace(cur)
	s.mu.Lock()
	is := s.instSet(name)
	written := is.VoiceHostWritten
	if addr == "" { // 马上开服，语音隧道却没开、也不会跟着开：上次写进去的隧道地址清回空，不然谁都连不上语音（测试报告 G-07）
		managed := cur != "" && cur == written
		if managed {
			is.VoiceHostWritten = ""
			_ = s.saveSettingsLocked()
		}
		s.mu.Unlock()
		if managed {
			if err := writeVoiceHost(path, p, ""); err != nil {
				s.log(name, "清 voice_host 失败：%v", err)
				return
			}
			s.log(name, "语音隧道没开，已把 voice_host 清回空（本机和局域网的语音照常；要远程好友能听，开语音隧道或勾上「跟随服务器启动」）")
		}
		return
	}
	if cur == addr && written != addr {
		is.VoiceHostWritten = addr
		_ = s.saveSettingsLocked()
	}
	s.mu.Unlock()
	if cur == addr || (cur != "" && cur != written) {
		return
	}
	if err := writeVoiceHost(path, p, addr); err != nil {
		s.log(name, "写 voice_host 失败：%v", err)
		return
	}
	s.mu.Lock()
	s.instSet(name).VoiceHostWritten = addr
	_ = s.saveSettingsLocked()
	s.mu.Unlock()
	if running := info.Status != "stopped" && !starting; running {
		s.markRestart(name, restartVoiceHost)
		s.log(name, "语音隧道的公网地址变了：已写进 voice_host=%s，重启服务器后远程好友才连得上语音", addr)
	} else {
		s.log(name, "已把语音隧道的公网地址写进 voice_host=%s", addr)
	}
}

// writeVoiceHost 只改 voice_host 这一行，其余原样保留；改之前把原文件备份成 voicechat-server.properties.bak。
func writeVoiceHost(path string, p *bridge.Props, value string) error {
	if orig, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".bak", orig, 0o644); err != nil {
			return fmt.Errorf("备份原配置失败：%w", err)
		}
	}
	p.Set("voice_host", value)
	return bridge.WriteFileAtomic(path, p.Bytes())
}

var hostRe = regexp.MustCompile(`^(\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?)$`)

// ValidVoiceHost 校验 voice_host：主机名或 IP，可以带端口（example.com:24454、[::1]:24454）；空串表示清掉。
func ValidVoiceHost(v string) error {
	if v == "" {
		return nil
	}
	host, port := v, ""
	if strings.HasPrefix(v, "[") || strings.Count(v, ":") == 1 {
		h, p, err := net.SplitHostPort(v)
		if err != nil || p == "" {
			return fmt.Errorf("语音地址写得不对：%q（应为 域名或 IP，可以带端口，如 frp-xxx.natfrp.cloud:12345）", v)
		}
		host, port = h, p
		if strings.Contains(h, ":") {
			host = "[" + h + "]"
		}
	}
	if !hostRe.MatchString(host) || len(host) > 253 {
		return fmt.Errorf("语音地址写得不对：%q（应为 域名或 IP，可以带端口，如 frp-xxx.natfrp.cloud:12345）", v)
	}
	if port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("语音地址的端口不对：%q", port)
		}
	}
	return nil
}

// VoiceHostChange SetVoiceHost 的结果。
type VoiceHostChange struct {
	Before  string `json:"before"`
	After   string `json:"after"`
	Changed bool   `json:"changed"`
	Restart bool   `json:"restart"` // 服务器运行中改的：重启后生效
}

// VoiceHostPlan 算出 SetVoiceHost 会把 voice_host 从什么改成什么（给操作层的预览用），以及配置文件现在的内容指纹。
func (s *Service) VoiceHostPlan(name, value string, fromTunnel bool) (VoiceHostChange, string, error) {
	info, err := s.d.Instances.Info(name)
	if err != nil {
		return VoiceHostChange{}, "", &Error{Code: 404, Err: err}
	}
	if fromTunnel {
		value = tunnelAddr(s.voiceTunnels(name))
		if value == "" {
			return VoiceHostChange{}, "", errorf(400, "语音隧道还没拿到公网地址：先启动隧道，或者手动填写语音地址")
		}
	}
	value = strings.TrimSpace(value)
	if err := ValidVoiceHost(value); err != nil {
		return VoiceHostChange{}, "", &Error{Code: 400, Err: err}
	}
	b, err := os.ReadFile(svcConfigPath(info.Dir))
	if err != nil {
		return VoiceHostChange{}, "", errorf(400, "Simple Voice Chat 的配置还没生成。先开一次服。第一次启动时会生成 %s。", svcConfigRel)
	}
	cur, _ := bridge.ParseProps(b).Get("voice_host")
	cur = strings.TrimSpace(cur)
	sum := sha256.Sum256(b)
	return VoiceHostChange{Before: cur, After: value, Changed: cur != value, Restart: info.Status != "stopped"}, hex.EncodeToString(sum[:]), nil
}

// SetVoiceHost 改 voice_host：fromTunnel 时用语音隧道的公网地址（以后隧道地址变了也会跟着改），
// 否则用 value（手动填的，MineDeck 以后不会自动覆盖；空串清掉，局域网好友不需要）。
func (s *Service) SetVoiceHost(name, value string, fromTunnel bool) (VoiceHostChange, error) {
	ch, _, err := s.VoiceHostPlan(name, value, fromTunnel)
	if err != nil {
		return ch, err
	}
	info, _ := s.d.Instances.Info(name)
	if ch.Changed {
		path := svcConfigPath(info.Dir)
		p, err := bridge.LoadProps(path)
		if err != nil {
			return ch, errorf(500, "读 SVC 的配置失败：%v", err)
		}
		if err := writeVoiceHost(path, p, ch.After); err != nil {
			return ch, errorf(500, "写 voice_host 失败：%v", err)
		}
	}
	managed := ""
	if fromTunnel || (ch.After != "" && ch.After == tunnelAddr(s.voiceTunnels(name))) {
		managed = ch.After
	}
	s.mu.Lock()
	s.instSet(name).VoiceHostWritten = managed
	_ = s.saveSettingsLocked()
	s.mu.Unlock()
	if ch.Changed && ch.Restart {
		s.markRestart(name, restartVoiceHost)
	}
	ch.Restart = ch.Changed && ch.Restart
	return ch, nil
}

// ---- 装桥接模组 ----

// InstallResult InstallBridge 的结果。
type InstallResult struct {
	bridge.JarStatus
	Removed []string `json:"removed,omitempty"`
	Restart bool     `json:"restart"` // 服务器运行中装的：重启后生效
}

// InstallPreview 装之前算出来的：现在装着什么、要装哪个文件、要删掉哪些（给操作层的预览用，InstallBridge 也按它装）。
type InstallPreview struct {
	Current bridge.JarStatus
	File    string   // 要装的文件
	Remove  []string // 要删掉的别的版本
	Noop    bool     // 已经装着同一个、也没有别的版本：什么都不用改
	Running bool     // 服务器没停：装好后重启才生效
	Status  string   // 实例的状态
}

// PreviewInstall 检查能不能装：核心和版本、加载方式、有没有 SVC（桥接模组声明了必须有它，没有的话服务器会起不来）、
// 服务器运行中要不要替换已经装着的（Windows 上加载器占着旧 jar，删不掉）。不改任何文件。
func (s *Service) PreviewInstall(name string, v bridge.Variant) (*InstallPreview, error) {
	if v == "" {
		v = bridge.VariantMain
	}
	info, err := s.d.Instances.Info(name)
	if err != nil {
		return nil, &Error{Code: 404, Err: err}
	}
	if ok, why := supportOf(info.Core, info.MC); !ok {
		return nil, errorf(400, "%s", why)
	}
	if v != bridge.VariantMain && v != bridge.VariantFallback {
		return nil, errorf(400, "加载方式只能是 main（默认）或 javafml（备用）")
	}
	st, err := bridge.InspectJar(modsDir(info.Dir))
	if err != nil {
		return nil, errorf(500, "读 mods 目录失败：%v", err)
	}
	if svc := s.detectSVC(info.Dir); !svc.Installed {
		return nil, errorf(400, "先装 Simple Voice Chat。桥接模组需要它。没装的话服务器起不来。")
	}
	p := &InstallPreview{Current: st, File: bridge.JarName(v), Running: info.Status != "stopped", Status: info.Status}
	p.Remove = append(p.Remove, st.Others...)
	if st.File != "" && st.File != p.File {
		p.Remove = append(p.Remove, st.File)
	}
	sort.Strings(p.Remove)
	p.Noop = st.File == p.File && len(p.Remove) == 0
	if p.Running && len(p.Remove) > 0 {
		return nil, errorf(409, "服务器运行中不能替换已经装着的桥接模组（文件被服务器占用）：先停服再安装")
	}
	return p, nil
}

// InstallBridge 把内置的桥接模组装进实例的 mods，删掉别的版本，写好它的配置。
// 没有 SVC 时不装；服务器运行中不能替换旧版本，只能装新的，重启后生效（见 PreviewInstall）。
func (s *Service) InstallBridge(name string, v bridge.Variant) (*InstallResult, error) {
	if v == "" {
		v = bridge.VariantMain
	}
	p, err := s.PreviewInstall(name, v)
	if err != nil {
		return nil, err
	}
	if p.Running && p.Noop {
		return &InstallResult{JarStatus: p.Current}, nil
	}
	info, err := s.d.Instances.Info(name)
	if err != nil {
		return nil, &Error{Code: 404, Err: err}
	}
	nst, removed, err := bridge.InstallJar(modsDir(info.Dir), v)
	if err != nil {
		return nil, errorf(500, "%v", err)
	}
	if _, _, err := bridge.EnsureConfig(info.Dir, bridge.EnsureOptions{Avoid: s.portsInUse(name)}); err != nil {
		return nil, errorf(500, "写桥接模组的配置失败：%v", err)
	}
	res := &InstallResult{JarStatus: nst, Removed: removed, Restart: p.Running}
	if p.Running {
		s.markRestart(name, restartBridge)
	}
	s.log(name, "已安装桥接模组 %s%s", nst.File, map[bool]string{true: "，重启服务器后生效", false: ""}[p.Running])
	return res, nil
}

// ---- 设置、音源 ----

// SetSettings 改「我在游戏里的名字」（写进桥接模组配置的 exclude，2 秒内生效）和音量（正在广播时立刻生效）。
func (s *Service) SetSettings(name string, exclude *string, volume *int) error {
	info, err := s.d.Instances.Info(name)
	if err != nil {
		return &Error{Code: 404, Err: err}
	}
	if volume != nil && (*volume < 0 || *volume > maxVolume) {
		return errorf(400, "音量要在 0 到 %d 之间", maxVolume)
	}
	if exclude != nil {
		ex, err := bridge.NormalizeExclude(*exclude)
		if err != nil {
			return &Error{Code: 400, Err: err}
		}
		if _, err := bridge.SetExclude(info.Dir, ex); os.IsNotExist(err) {
			_, _, err = bridge.EnsureConfig(info.Dir, bridge.EnsureOptions{Exclude: &ex, Avoid: s.portsInUse(name)})
			if err != nil {
				return errorf(500, "写桥接模组的配置失败：%v", err)
			}
		} else if err != nil {
			return errorf(500, "写桥接模组的配置失败：%v", err)
		}
	}
	if volume != nil {
		s.mu.Lock()
		v := *volume
		s.instSet(name).Volume = &v
		err := s.saveSettingsLocked()
		if s.cur != nil && s.cur.inst == name {
			s.cur.volume.Store(int32(v))
		}
		s.mu.Unlock()
		if err != nil {
			return errorf(500, "保存设置失败：%v", err)
		}
	}
	return nil
}

// ConfirmSystem 记下「这台电脑确认过整机录音的提醒」（每台电脑一次）。
func (s *Service) ConfirmSystem() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.set.SystemConfirmed = true
	if err := s.saveSettingsLocked(); err != nil {
		return errorf(500, "保存设置失败：%v", err)
	}
	return nil
}

// CapView 这台电脑能怎么录。
type CapView struct {
	OS      string `json:"os"`      // 如「Windows 11（10.0.26300）」
	Mode    string `json:"mode"`    // 推荐的采集方式：process / system；不能录时为空
	Warn    bool   `json:"warn"`    // 只能录整机，要先提醒
	Warning string `json:"warning"` // 提醒的原文
}

func (s *Service) capability() CapView {
	c := s.d.Detect()
	cv := CapView{Warn: c.Warn, Warning: ErrNeedConfirm.Error()}
	if c.Mode != 0 {
		cv.Mode = modeName(c.Mode)
		cv.OS = c.String()
		if i := strings.Index(cv.OS, " → "); i >= 0 {
			cv.OS = cv.OS[:i]
		}
	}
	return cv
}

// SourceView 一个可选的音源。
type SourceView struct {
	PID     uint32   `json:"pid"`
	Exe     string   `json:"exe"`
	Name    string   `json:"name,omitempty"`
	Playing bool     `json:"playing"`
	Peak    float32  `json:"peak"` // 0–1
	Devices []string `json:"devices,omitempty"`
}

// SourcesView 音源列表（按程序录时选哪个）和播放设备（整机录时选哪个）。
type SourcesView struct {
	Capability      CapView        `json:"capability"`
	SystemConfirmed bool           `json:"systemConfirmed"`
	Sources         []SourceView   `json:"sources"`
	Devices         []music.Device `json:"devices"`
	Device          string         `json:"device"` // 上次选的播放设备；空为系统默认
	Error           string         `json:"error,omitempty"`
}

// hiddenExe 不列出来的程序：游戏本身（java、javaw），录进去会有回声。MineDeck 自己在 ListSources 里已经去掉了。
var hiddenExe = map[string]bool{"java.exe": true, "javaw.exe": true}

// Sources 列出正在用声音的程序（带当时的电平）和播放设备。
func (s *Service) Sources() SourcesView {
	s.mu.Lock()
	sv := SourcesView{SystemConfirmed: s.set.SystemConfirmed, Device: s.set.Device, Sources: []SourceView{}, Devices: []music.Device{}}
	s.mu.Unlock()
	sv.Capability = s.capability()
	list, err := s.d.ListSources()
	for _, src := range list {
		if hiddenExe[strings.ToLower(src.Exe)] {
			continue
		}
		sv.Sources = append(sv.Sources, SourceView{PID: src.PID, Exe: src.Exe, Name: src.Name, Playing: src.Playing, Peak: src.Peak, Devices: src.Devices})
	}
	if err != nil {
		sv.Error = err.Error()
	}
	if devs, err := s.d.ListDevices(); err == nil {
		sv.Devices = append(sv.Devices, devs...)
	} else if sv.Error == "" {
		sv.Error = err.Error()
	}
	return sv
}
