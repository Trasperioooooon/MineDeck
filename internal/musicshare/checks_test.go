package musicshare

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/bridge"
	"minedeck/internal/events"
	"minedeck/internal/mcsrc"
	"minedeck/internal/tunnel"
)

func TestSupportAndVersions(t *testing.T) {
	cases := []struct {
		core mcsrc.Core
		mc   string
		ok   bool
	}{
		{mcsrc.CoreNeoForge, "1.21.1", true}, {mcsrc.CoreNeoForge, "1.20.5", true}, {mcsrc.CoreNeoForge, "1.20.4", false},
		{mcsrc.CoreForge, "1.20.1", true}, {mcsrc.CoreForge, "1.19.2", false}, {mcsrc.CoreForge, "1.21.4", true},
		{mcsrc.CoreNeoForge, "26.1", true}, {mcsrc.CoreMohist, "1.20.1", true},
		{mcsrc.CoreFabric, "1.21.1", false}, {mcsrc.CorePaper, "1.21.1", false}, {mcsrc.CoreVanilla, "1.21.1", false},
	}
	for _, c := range cases {
		if ok, why := supportOf(c.core, c.mc); ok != c.ok || (!ok && why == "") {
			t.Errorf("%s %s：%v %q", c.core, c.mc, ok, why)
		}
	}
	if !versionLess("1.20.1", "1.20.5") || versionLess("1.21", "1.20.5") || versionLess("1.20.5", "1.20.5") || !versionLess("1.21.11", "26.1") {
		t.Error("版本比较")
	}
	if !Applies(mcsrc.CoreForge) || Applies(mcsrc.CorePaper) {
		t.Error("Applies")
	}
}

// 按 jar 里的元数据找 SVC（NeoForge、Forge、Fabric 三种写法；改过名的也认），版本号是 ${file.jarVersion}
// 时取清单里的；停用的另外列出来；没装时 Installed 为 false。
func TestDetectSVC(t *testing.T) {
	e := newEnv(t)
	mods := filepath.Join(e.dir, "mods")
	if svc := e.s.detectSVC(e.dir); svc.Installed {
		t.Fatal("没有 mods 目录时不该认出 SVC")
	}
	writeZip(t, filepath.Join(mods, "jei-1.21.1.jar"), map[string]string{"META-INF/neoforge.mods.toml": "[[mods]]\nmodId=\"jei\"\nversion=\"19.0\"\n"})
	writeZip(t, filepath.Join(mods, "minedeck-voicebridge-0.2.0.jar"), map[string]string{"META-INF/mods.toml": "[[dependencies.minedeck_voicebridge]]\nmodId=\"voicechat\"\n"})
	if err := os.WriteFile(filepath.Join(mods, "voicechat-old.jar.disabled"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if svc := e.s.detectSVC(e.dir); svc.Installed || !slices.Equal(svc.Disabled, []string{"voicechat-old.jar.disabled"}) {
		t.Fatalf("依赖里写着 voicechat 的不算；停用的另列：%+v", svc)
	}
	writeZip(t, filepath.Join(mods, "改过名的语音.jar"), map[string]string{
		"META-INF/mods.toml":   "modLoader=\"javafml\"\n[[mods]]\n  modId = 'voicechat'\n  version = \"${file.jarVersion}\"\n",
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nImplementation-Version: 1.20.1-2.6.27\r\n",
	})
	svc := e.s.detectSVC(e.dir)
	if !svc.Installed || svc.File != "改过名的语音.jar" || svc.Version != "1.20.1-2.6.27" || svc.Loader != "forge" {
		t.Fatalf("Forge 版：%+v", svc)
	}
	os.Remove(filepath.Join(mods, "改过名的语音.jar"))
	writeZip(t, filepath.Join(mods, "voicechat-fabric-1.21.1-2.6.27.jar"), map[string]string{"fabric.mod.json": `{"id":"voicechat","version":"1.21.1-2.6.27"}`})
	if svc := e.s.detectSVC(e.dir); !svc.Installed || svc.Loader != "fabric" {
		t.Fatalf("Fabric 版：%+v", svc)
	}
	if v, ok := tomlModVersion([]byte("[[mods]]\nmodId=\"a\"\nversion=\"1\"\n[[mods]]\nmodId=\"voicechat\"\nversion=\"2\"\n[[dependencies.voicechat]]\nversion=\"3\""), "voicechat"); !ok || v != "2" {
		t.Errorf("tomlModVersion：%q %v", v, ok)
	}
}

// 装桥接模组：没有 SVC 不装（装了服务器会起不来）；装好后配置也写好（打开共享、端口、令牌）；
// 运行中不能替换已经装着的；换备用加载方式时删掉默认的。
func TestInstallBridge(t *testing.T) {
	e := newEnv(t)
	if _, err := e.s.InstallBridge(instName, bridge.VariantMain); codeOf(err) != 400 || !strings.Contains(err.Error(), "先装 Simple Voice Chat") {
		t.Fatalf("没有 SVC：%v", err)
	}
	e.addSVC()
	res, err := e.s.InstallBridge(instName, bridge.VariantMain)
	if err != nil || !res.Current || res.Restart || res.File != "minedeck-voicebridge-"+bridge.JarVersion+".jar" {
		t.Fatalf("安装：%+v %v", res, err)
	}
	c, err := bridge.ReadConfig(e.dir)
	if err != nil || !c.Enabled || c.Problem() != "" {
		t.Fatalf("装好后的配置：%+v %v", c, err)
	}
	st := e.status()
	if !st.Bridge.Current || st.Bridge.Config == nil || st.Bridge.Config.Port != c.Port || !st.SVC.Installed {
		t.Fatalf("状态：%+v", st.Bridge)
	}
	e.inst.setStatus(instName, "running")
	if _, err := e.s.InstallBridge(instName, bridge.VariantFallback); codeOf(err) != 409 {
		t.Errorf("运行中换加载方式应为 409：%v", err)
	}
	if res, err := e.s.InstallBridge(instName, bridge.VariantMain); err != nil || res.Restart {
		t.Errorf("运行中再装同一个：%+v %v", res, err)
	}
	e.inst.setStatus(instName, "stopped")
	res, err = e.s.InstallBridge(instName, bridge.VariantFallback)
	if err != nil || res.Variant != bridge.VariantFallback || len(res.Removed) != 1 {
		t.Fatalf("换备用加载方式：%+v %v", res, err)
	}
	if _, err := e.s.InstallBridge(instName, "bogus"); codeOf(err) != 400 {
		t.Errorf("未知的加载方式：%v", err)
	}
	e.inst.add(InstInfo{Name: "老服", Dir: t.TempDir(), Core: mcsrc.CoreForge, MC: "1.18.2"})
	if _, err := e.s.InstallBridge("老服", bridge.VariantMain); codeOf(err) != 400 || !strings.Contains(err.Error(), "1.20.1") {
		t.Errorf("Forge 1.18.2：%v", err)
	}
}

// 开服前（inst 的回调）：装了桥接模组就保证配置能用；端口避开别的实例已经用了的。
func TestBeforeStartEnsuresBridgeConfig(t *testing.T) {
	e := newEnv(t)
	other := filepath.Join(t.TempDir(), "other")
	e.inst.add(InstInfo{Name: "另一台", Dir: other, Core: mcsrc.CoreForge, MC: "1.20.1"})
	if err := bridge.WriteFileAtomic(bridge.ConfigPath(other), []byte("enabled=true\nport=24470\ntoken=0123456789abcdef0123\n")); err != nil {
		t.Fatal(err)
	}
	e.s.BeforeStart(instName) // 没装桥接模组：什么都不写
	if _, err := os.Stat(bridge.ConfigPath(e.dir)); !os.IsNotExist(err) {
		t.Fatal("没装桥接模组时不该写它的配置")
	}
	e.addSVC()
	if _, _, err := bridge.InstallJar(filepath.Join(e.dir, "mods"), bridge.VariantMain); err != nil {
		t.Fatal(err)
	}
	// 模组自己生成的模板：共享关着、端口是默认的 24470（和另一台撞了）
	if err := bridge.WriteFileAtomic(bridge.ConfigPath(e.dir), []byte("enabled=false\nport=24470\ntoken=fedcba9876543210fedc\nexclude=房主\n")); err != nil {
		t.Fatal(err)
	}
	e.s.BeforeStart(instName)
	c, _ := bridge.ReadConfig(e.dir)
	if !c.Enabled || c.Port == 24470 || c.Port < 1024 || c.Token != "fedcba9876543210fedc" || c.Exclude != "房主" {
		t.Fatalf("开服前补好的配置：%+v", c)
	}
}

// voice_host（§8.8）：语音隧道拿到公网地址后，开服前写进 voice_host（先备份原文件，别的行原样保留）；
// 运行中隧道地址变了（tunnel.up）也写，并提示重启；用户自己填的别的值不覆盖，让用户选；
// 手动填写、改用隧道地址、清掉都可以；SVC 的配置还没生成时说明要先开一次服。
func TestVoiceHostSync(t *testing.T) {
	e := newEnv(t)
	cfg := filepath.Join(e.dir, "config", "voicechat", "voicechat-server.properties")
	if _, err := e.s.SetVoiceHost(instName, "a.example.com", false); codeOf(err) != 400 || !strings.Contains(err.Error(), "先开一次服") {
		t.Fatalf("SVC 的配置还没生成：%v", err)
	}
	if st := e.status(); st.Remote.State != "none" {
		t.Fatalf("没有语音隧道：%+v", st.Remote)
	}
	voice := tunnel.Status{Tunnel: tunnel.Tunnel{ID: "tun3", Provider: "natfrp", Name: "语音", Bound: instName, AutoStart: true, Purpose: tunnel.PurposeVoice}}
	game := tunnel.Status{Tunnel: tunnel.Tunnel{ID: "tun1", Provider: "natfrp", Name: "游戏", Bound: instName, Purpose: tunnel.PurposeGame, PublicAddr: "frp-game.natfrp.cloud:25565"}}
	e.tun.set(game, voice)
	if st := e.status(); st.Remote.State != "waiting" || len(st.Remote.Tunnels) != 1 || st.Remote.Tunnels[0].ID != "tun3" {
		t.Fatalf("语音隧道还没拿到地址：%+v", st.Remote)
	}
	voice.PublicAddr = "frp-abc.natfrp.cloud:12345"
	e.tun.set(game, voice)
	if st := e.status(); st.Remote.State != "pending" || st.Voice.Found {
		t.Fatalf("SVC 的配置还没生成：%+v", st.Remote)
	}
	orig := "# Simple Voice Chat server config v2.6.27\r\nport=24454\r\nbind_address=\r\nvoice_host=\r\nallow_recording=true\r\n"
	if err := bridge.WriteFileAtomic(cfg, []byte(orig)); err != nil {
		t.Fatal(err)
	}
	e.s.BeforeStart(instName)
	got, _ := os.ReadFile(cfg)
	if string(got) != strings.Replace(orig, "voice_host=\r\n", "voice_host=frp-abc.natfrp.cloud:12345\r\n", 1) {
		t.Fatalf("写 voice_host 后：%q", got)
	}
	if bak, _ := os.ReadFile(cfg + ".bak"); string(bak) != orig {
		t.Fatalf("应先备份原文件：%q", bak)
	}
	st := e.status()
	if st.Remote.State != "same" || !st.Remote.Managed || st.Voice.VoiceHost != "frp-abc.natfrp.cloud:12345" || st.Voice.Port != 24454 || len(st.Restart) != 0 {
		t.Fatalf("写好以后：%+v %+v %v", st.Remote, st.Voice, st.Restart)
	}
	if !strings.Contains(st.Guide, "frp-abc.natfrp.cloud:12345") {
		t.Errorf("给好友的说明里应有语音地址：%s", st.Guide)
	}

	// 运行中换了节点：tunnel.up 时写进去，提示重启
	e.inst.setStatus(instName, "running")
	voice.Running = true
	voice.PublicAddr = "frp-new.natfrp.cloud:23456"
	e.tun.set(game, voice)
	e.s.HandleEvent(events.Event{Type: "tunnel.up", Data: map[string]any{"id": "tun3", "tunnel": "语音", "addr": voice.PublicAddr}})
	st = e.status()
	if st.Voice.VoiceHost != "frp-new.natfrp.cloud:23456" || !slices.Equal(st.Restart, []string{"voice_host"}) {
		t.Fatalf("运行中地址变了：%+v %v", st.Voice, st.Restart)
	}
	if con, _ := e.inst.Console(instName); !strings.Contains(strings.Join(replay(con), "\n"), "重启服务器后远程好友才连得上语音") {
		t.Error("控制台里应提示重启")
	}
	e.s.HandleEvent(events.Event{Type: "tunnel.up", Data: map[string]any{"id": "tun1", "addr": game.PublicAddr}}) // 游戏隧道：不管

	// 用户自己填了别的（比如自己的域名）：隧道地址再变也不覆盖，让用户选
	ch, err := e.s.SetVoiceHost(instName, "voice.example.com:24454", false)
	if err != nil || ch.Before != "frp-new.natfrp.cloud:23456" || ch.After != "voice.example.com:24454" || !ch.Restart {
		t.Fatalf("手动填写：%+v %v", ch, err)
	}
	voice.PublicAddr = "frp-third.natfrp.cloud:3456"
	e.tun.set(game, voice)
	e.s.HandleEvent(events.Event{Type: "tunnel.up", Data: map[string]any{"id": "tun3", "addr": voice.PublicAddr}})
	if st := e.status(); st.Voice.VoiceHost != "voice.example.com:24454" || st.Remote.State != "differ" || st.Remote.Managed {
		t.Fatalf("用户填的不该被覆盖：%+v %+v", st.Voice, st.Remote)
	}
	if ch, err := e.s.SetVoiceHost(instName, "", true); err != nil || ch.After != "frp-third.natfrp.cloud:3456" {
		t.Fatalf("改用隧道地址：%+v %v", ch, err)
	}
	if st := e.status(); st.Remote.State != "same" || !st.Remote.Managed {
		t.Fatalf("改用隧道地址后：%+v", st.Remote)
	}
	if ch, err := e.s.SetVoiceHost(instName, "", false); err != nil || ch.After != "" || !ch.Changed {
		t.Fatalf("清掉：%+v %v", ch, err)
	}
	for _, bad := range []string{"a b", "http://x", "x:99999", "x:", "-x.com"} {
		if _, err := e.s.SetVoiceHost(instName, bad, false); codeOf(err) != 400 {
			t.Errorf("%q 应报格式不对：%v", bad, err)
		}
	}
	for _, good := range []string{"1.2.3.4:24454", "[2001:db8::1]:24454", "frp.example.com", "24454"} {
		if err := ValidVoiceHost(good); err != nil {
			t.Errorf("%q 应该能用：%v", good, err)
		}
	}
	voice.PublicAddr = ""
	e.tun.set(voice)
	if _, err := e.s.SetVoiceHost(instName, "", true); codeOf(err) != 400 {
		t.Errorf("隧道还没地址时改用隧道地址：%v", err)
	}
}

// 语音隧道停着（测试报告 G-07）：页面不再说「远程好友连服后就能听到」；开服前隧道既没开、也不会跟着开，
// 就把 MineDeck 上次写进去的地址清回空（不然谁都连不上语音）；用户自己填的不动；勾了跟随实例启动的照旧写。
func TestVoiceHostWhenTunnelStopped(t *testing.T) {
	e := newEnv(t)
	cfg := filepath.Join(e.dir, "config", "voicechat", "voicechat-server.properties")
	orig := "port=24454\r\nvoice_host=\r\n"
	if err := bridge.WriteFileAtomic(cfg, []byte(orig)); err != nil {
		t.Fatal(err)
	}
	voice := tunnel.Status{Tunnel: tunnel.Tunnel{ID: "tun3", Provider: "natfrp", Name: "语音", Bound: instName, Purpose: tunnel.PurposeVoice,
		PublicAddr: "203.0.113.7:34567"}, Running: true} // 203.0.113.0/24 是文档专用的地址段
	e.tun.set(voice)
	e.inst.setStatus(instName, "running")
	e.s.HandleEvent(events.Event{Type: "tunnel.up", Data: map[string]any{"id": "tun3", "addr": voice.PublicAddr}})
	if st := e.status(); st.Voice.VoiceHost != voice.PublicAddr || st.Remote.State != "same" {
		t.Fatalf("隧道开着：%+v %+v", st.Voice, st.Remote)
	}
	// 停隧道：地址还留在 tunnels.json 里，页面要说隧道没开
	voice.Running = false
	e.tun.set(voice)
	if st := e.status(); st.Remote.State != "idle" || !st.Remote.Managed {
		t.Fatalf("隧道停了：%+v", st.Remote)
	}
	e.inst.setStatus(instName, "stopped")
	if st := e.status(); st.Remote.State != "idle" {
		t.Fatalf("服务器也停了、隧道没勾跟随启动：%+v", st.Remote)
	}
	// 下次开服前：清回空
	e.s.BeforeStart(instName)
	if got, _ := os.ReadFile(cfg); string(got) != orig {
		t.Fatalf("隧道没开，voice_host 应清回空：%q", got)
	}
	if st := e.status(); st.Remote.State != "idle" || st.Remote.Managed {
		t.Fatalf("清掉以后：%+v", st.Remote)
	}
	// 勾了跟随实例启动：开服时会跟着拉起，照旧写上次的地址
	voice.AutoStart = true
	e.tun.set(voice)
	if st := e.status(); st.Remote.State != "pending" {
		t.Fatalf("勾了跟随启动、服务器停着：%+v", st.Remote)
	}
	e.s.BeforeStart(instName)
	if st := e.status(); st.Voice.VoiceHost != voice.PublicAddr || st.Remote.State != "same" {
		t.Fatalf("跟随启动的隧道开服前照旧写：%+v %+v", st.Voice, st.Remote)
	}
	// 用户自己填的：隧道没开也不动
	voice.AutoStart = false
	e.tun.set(voice)
	if _, err := e.s.SetVoiceHost(instName, "voice.example.com:24454", false); err != nil {
		t.Fatal(err)
	}
	e.s.BeforeStart(instName)
	if st := e.status(); st.Voice.VoiceHost != "voice.example.com:24454" || st.Remote.State != "differ" {
		t.Fatalf("用户填的不该清：%+v %+v", st.Voice, st.Remote)
	}
}

func replay(con interface {
	Subscribe() ([]string, chan string, func())
}) []string {
	r, _, cancel := con.Subscribe()
	cancel()
	return r
}

// 我在游戏里的名字写进桥接模组配置的 exclude（规范化，写错就近报错）；音量 0–200。
func TestSettings(t *testing.T) {
	e := newEnv(t)
	ex := " Steve，房主 "
	if err := e.s.SetSettings(instName, &ex, nil); err != nil {
		t.Fatal(err)
	}
	if c, err := bridge.ReadConfig(e.dir); err != nil || c.Exclude != "Steve,房主" {
		t.Fatalf("没有配置文件时新建一份：%+v %v", c, err)
	}
	ex = "Alex"
	vol := 80
	if err := e.s.SetSettings(instName, &ex, &vol); err != nil {
		t.Fatal(err)
	}
	if st := e.status(); st.Settings.Exclude != "Alex" || st.Settings.Volume != 80 {
		t.Fatalf("设置：%+v", st.Settings)
	}
	bad := `a\b`
	if err := e.s.SetSettings(instName, &bad, nil); codeOf(err) != 400 {
		t.Errorf("名字里有反斜杠：%v", err)
	}
	big := 201
	if err := e.s.SetSettings(instName, nil, &big); codeOf(err) != 400 {
		t.Errorf("音量超过 200：%v", err)
	}
	if err := e.s.SetSettings("没有的", nil, &vol); codeOf(err) != 404 {
		t.Errorf("没有的实例：%v", err)
	}
	// 记进 data/music/settings.json，重启 MineDeck 后还在
	if st, _ := e.newService().Status(instName); st.Settings.Volume != 80 {
		t.Errorf("音量没存下来：%+v", st.Settings)
	}
}

// 音源列表：去掉游戏本身（javaw），带电平；播放设备列表；Win11 推荐按程序录。
func TestSources(t *testing.T) {
	e := newEnv(t)
	sv := e.s.Sources()
	var exes []string
	for _, s := range sv.Sources {
		exes = append(exes, s.Exe)
	}
	if !slices.Equal(exes, []string{"cloudmusic.exe", "QQMusic.exe"}) || sv.Sources[0].Peak != 0.6 || !sv.Sources[0].Playing {
		t.Fatalf("音源：%+v", sv.Sources)
	}
	if len(sv.Devices) != 2 || !sv.Devices[0].Default || sv.Capability.Mode != "process" || sv.Capability.Warn || !strings.Contains(sv.Capability.OS, "Windows 11") {
		t.Fatalf("设备和能力：%+v %+v", sv.Devices, sv.Capability)
	}
}

// 听众状态：从控制台认出谁连上了语音、谁没装 SVC、谁版本不兼容、谁在排除名单里；SVC 加载完插件却没有
// 桥接模组时提示没加载上。只看这次开服之后的日志。
func TestVoicePlayersFromConsole(t *testing.T) {
	e := newEnv(t)
	e.addSVC()
	if _, err := e.s.InstallBridge(instName, bridge.VariantMain); err != nil {
		t.Fatal(err)
	}
	ex := "Host"
	if err := e.s.SetSettings(instName, &ex, nil); err != nil {
		t.Fatal(err)
	}
	con, _ := e.inst.Console(instName)
	con.Append("[20:00:00] [Server thread/INFO] [voicechat/]: Player Ghost (11111111-1111-1111-1111-111111111111) successfully connected to voice chat") // 上一次开服的
	con.Append("[MineDeck] 启动: java -jar server.jar")
	e.inst.setStatus(instName, "running")
	e.s.HandleEvent(eventOf("instance.start", instName))
	for _, l := range []string{
		"[20:01:00] [main/INFO] [de.maxhenkel.voicechat.Voicechat/]: [voicechat] Loading plugins",
		"[20:01:00] [main/INFO] [de.maxhenkel.voicechat.Voicechat/]: [voicechat] Loaded 1 plugin(s)",
		"[20:01:02] [VoiceChatServerThread/INFO] [voicechat/]: [voicechat] Voice chat server started at port 24454",
		"[20:02:00] [Server thread/INFO] [voicechat/]: [voicechat] Received secret request of Alice (20)",
		"[20:02:01] [VoiceChatPacketProcessingThread/INFO] [voicechat/]: [voicechat] Player Alice (069a79f4-44e9-4726-a5be-fca90e38aaf5) successfully connected to voice chat",
		"[20:02:02] [VoiceChatPacketProcessingThread/INFO] [voicechat/]: [voicechat] Player Host (169a79f4-44e9-4726-a5be-fca90e38aaf5) successfully connected to voice chat",
		"[20:02:03] [Server thread/WARN] [voicechat/]: [voicechat] Connected client Carl has incompatible voice chat version (server=20, client=19)",
		"[20:03:00] [VoiceChatPacketProcessingThread/INFO] [voicechat/]: [voicechat] Player Dave (269a79f4-44e9-4726-a5be-fca90e38aaf5) successfully connected to voice chat",
		"[20:03:30] [Server thread/INFO] [voicechat/]: [voicechat] Disconnecting client Dave",
	} {
		con.Append(l)
	}
	e.inst.setOnline(instName, "Alice", "Bob", "Carl", "Dave", "Host")
	waitFor(t, "认出听众", 2*time.Second, func() bool { return len(e.status().Players.Voice) == 1 })
	st := e.status()
	p := st.Players
	if !p.Known || !slices.Equal(p.Voice, []string{"Alice"}) || !slices.Equal(p.Excluded, []string{"Host"}) ||
		!slices.Equal(p.NoVoice, []string{"Bob", "Dave"}) || len(p.Incompatible) != 1 || p.Incompatible[0] != (IncompatView{"Carl", 19, 20}) {
		t.Fatalf("听众：%+v", p)
	}
	if st.VoicePort != 24454 || st.Bridge.Loaded == nil || *st.Bridge.Loaded {
		t.Fatalf("SVC 加载完插件却没有桥接模组，应提示没加载上：port=%d loaded=%v", st.VoicePort, st.Bridge.Loaded)
	}
	con.Append("[20:01:00] [main/INFO] [de.maxhenkel.voicechat.Voicechat/]: [voicechat] Registering events for 'minedeck_voicebridge'")
	waitFor(t, "认出桥接模组加载了", 2*time.Second, func() bool { l := e.status().Bridge.Loaded; return l != nil && *l })
	// 停服：清掉
	e.inst.setStatus(instName, "stopped")
	e.s.HandleEvent(eventOf("instance.stop", instName))
	if st := e.status(); st.Players.Known || st.Bridge.Loaded != nil || st.VoicePort != 0 {
		t.Fatalf("停服后：%+v", st.Players)
	}
}
