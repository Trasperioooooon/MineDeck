package catalog

// 音乐共享的操作（第五阶段）。

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/bridge"
	"minedeck/internal/inst"
	"minedeck/internal/musicshare"
	"minedeck/internal/ops"
	"minedeck/internal/tunnel"
)

// newMusicCatalog 在 newCatalog 的基础上再放一个停着的 NeoForge 1.21.1 实例「模组服」（Java 路径是假的，起不来）。
func newMusicCatalog(t *testing.T) (*ops.Registry, *inst.Manager, string) {
	t.Helper()
	newCatalog(t)
	dir := filepath.Join(app.ServersDir, "s2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, _ := json.Marshal(inst.Settings{Name: "模组服", Core: "neoforge", MC: "1.21.1", XmxMB: 2048, XmsMB: 1024,
		JavaPath: filepath.Join(dir, "没有这个", "java.exe")})
	for p, body := range map[string]string{filepath.Join(dir, "instance.json"): string(s), filepath.Join(dir, "server.properties"): "server-port=25565\n"} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	return New(Env{Mgr: mgr, Tun: tunnel.NewManager(), Port: 27333}), mgr, dir
}

// addSVC 在 mods 里放一个只有元数据的 SVC。
func addSVC(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "mods", "voicechat-neoforge-1.21.1-2.6.27.jar")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("META-INF/neoforge.mods.toml")
	_, _ = w.Write([]byte("modLoader = \"javafml\"\n[[mods]]\nmodId = \"voicechat\"\nversion = \"1.21.1-2.6.27\"\n"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// 风险等级：AI 只能看状态、提议装桥接模组和改 voice_host；开始、停止广播这类操作只能由人来，开始广播还只能在本机。
func TestMusicOpsPermissions(t *testing.T) {
	reg, _, _ := newMusicCatalog(t)
	ctx := context.Background()
	avail := map[string]bool{}
	for _, op := range reg.Available(ops.Caller{Source: ops.SrcAssistant, Local: true}) {
		avail[op.Name] = true
	}
	for name, want := range map[string]bool{"music.status": true, "music.bridge.install": true, "music.voicehost.set": true,
		"music.start": false, "music.stop": false, "music.sources": false, "music.settings.set": false, "music.system.confirm": false} {
		if avail[name] != want {
			t.Errorf("助手能不能用 %s：%v，应为 %v", name, avail[name], want)
		}
	}
	ai := ops.Caller{Source: ops.SrcAssistant, Actor: "本机", Local: true}
	if _, err := reg.Do(ctx, ai, "music.start", ops.Args(`{"inst":"模组服","mode":"system"}`)); ops.CodeOf(err) != 403 {
		t.Errorf("AI 开始广播应为 403：%v", err)
	}
	lan := ops.Caller{Source: ops.SrcLAN, Actor: "192.168.1.5"}
	for _, name := range []string{"music.start", "music.sources", "music.system.confirm"} {
		if _, err := reg.Do(ctx, lan, name, ops.Args(`{"inst":"模组服","mode":"system"}`)); ops.CodeOf(err) != 403 {
			t.Errorf("局域网调用 %s 应为 403：%v", name, err)
		}
	}
	// 局域网可以看状态、停止广播（没在广播时报 400）
	if _, err := reg.Do(ctx, lan, "music.status", ops.Args(`{"inst":"模组服"}`)); err != nil {
		t.Errorf("局域网看状态：%v", err)
	}
	if _, err := reg.Do(ctx, lan, "music.stop", ops.Args(`{"inst":"模组服"}`)); ops.CodeOf(err) != 400 || !strings.Contains(err.Error(), "没在给") {
		t.Errorf("没在广播时停止：%v", err)
	}
	if _, err := reg.Do(ctx, lan, "music.status", ops.Args(`{"inst":"没有这个服"}`)); ops.CodeOf(err) != 404 {
		t.Errorf("实例不存在应为 404：%v", err)
	}
	ui := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	if _, err := reg.Do(ctx, ui, "music.start", ops.Args(`{"inst":"模组服","mode":"radio"}`)); ops.CodeOf(err) != 400 {
		t.Errorf("采集方式不对应为 400：%v", err)
	}
	if _, err := reg.Do(ctx, ui, "music.start", ops.Args(`{"inst":"模组服","mode":"process","pid":1234}`)); ops.CodeOf(err) != 400 ||
		!strings.Contains(err.Error(), "没在运行") {
		t.Errorf("服务器没开时开始广播：%v", err)
	}
	if _, err := reg.Do(ctx, ui, "music.settings.set", ops.Args(`{"inst":"模组服","volume":300}`)); ops.CodeOf(err) != 400 {
		t.Errorf("音量超范围应为 400：%v", err)
	}
	st, err := reg.Do(ctx, ui, "music.status", ops.Args(`{"inst":"生存服"}`))
	if err != nil || st.(*musicshare.Status).Supported {
		t.Errorf("Paper 不支持音乐共享：%+v %v", st, err)
	}
}

// AI 提议装桥接模组：没有 SVC 时连提案都不生成；有了以后预览列出要装的文件，人确认后装好、写好配置。
// 开服前（inst 的回调）会补好被删掉的配置。
func TestMusicBridgeInstallProposal(t *testing.T) {
	reg, mgr, dir := newMusicCatalog(t)
	ctx := context.Background()
	ai := ops.Caller{Source: ops.SrcAssistant, Actor: "本机", Session: "s1", Local: true}
	ui := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	if _, err := reg.Do(ctx, ai, "music.bridge.install", ops.Args(`{"inst":"模组服"}`)); ops.CodeOf(err) != 400 || !strings.Contains(err.Error(), "Simple Voice Chat") {
		t.Fatalf("没有 SVC：%v", err)
	}
	addSVC(t, dir)
	res, err := reg.Do(ctx, ai, "music.bridge.install", ops.Args(`{"inst":"模组服"}`))
	if err != nil {
		t.Fatal(err)
	}
	p := res.(*ops.Proposal)
	jar := "mods/" + bridge.JarName(bridge.VariantMain)
	if len(p.Plan.Changes) != 1 || p.Plan.Changes[0].What != jar || !strings.Contains(p.Plan.Summary, "模组服") {
		t.Fatalf("预览：%+v", p.Plan)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(jar))); !os.IsNotExist(err) {
		t.Fatal("没确认就装了")
	}
	out, err := reg.Confirm(ctx, ui, p.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if r := out.(*musicshare.InstallResult); !r.Current || r.Restart {
		t.Fatalf("安装结果：%+v", r)
	}
	c, err := bridge.ReadConfig(dir)
	if err != nil || !c.Enabled || c.Problem() != "" {
		t.Fatalf("配置：%+v %v", c, err)
	}
	// 提案提出时也记一条（Event = propose），查询结果新的在前：先是确认后的执行，再是提出
	recs, _ := reg.Log.Query(ops.Query{})
	if len(recs) != 2 || recs[0].Op != "music.bridge.install" || recs[0].Event != "" || recs[0].Proposal != p.ID || recs[0].Inst != "模组服" ||
		recs[0].ApprovedBy != "本机" || recs[1].Event != ops.EvPropose || recs[1].Proposal != p.ID {
		t.Fatalf("操作记录：%+v", recs)
	}

	// 开服前的回调：配置被删了也会补上（这里 Java 是假的，起不来，但回调已经跑过）
	if err := os.Remove(bridge.ConfigPath(dir)); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Start("模组服"); err == nil || !strings.Contains(err.Error(), "Java") {
		t.Fatalf("假的 Java 应该起不来：%v", err)
	}
	if c, err := bridge.ReadConfig(dir); err != nil || !c.Enabled || c.Problem() != "" {
		t.Fatalf("开服前没有补好配置：%+v %v", c, err)
	}
}

// AI 提议改 voice_host：SVC 的配置还没生成时报「先开一次服」；预览列出改前改后；确认前文件被改过，提案作废。
func TestMusicVoiceHostProposal(t *testing.T) {
	reg, _, dir := newMusicCatalog(t)
	ctx := context.Background()
	ai := ops.Caller{Source: ops.SrcAssistant, Actor: "本机", Session: "s1", Local: true}
	ui := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	args := ops.Args(`{"inst":"模组服","value":"frp-abc.example.com:34567"}`)
	if _, err := reg.Do(ctx, ai, "music.voicehost.set", args); ops.CodeOf(err) != 400 || !strings.Contains(err.Error(), "先开一次服") {
		t.Fatalf("还没有 SVC 的配置：%v", err)
	}
	cfg := filepath.Join(dir, "config", "voicechat", "voicechat-server.properties")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("# Simple Voice Chat server config\nport=24454\nvoice_host=\n")
	if _, err := reg.Do(ctx, ai, "music.voicehost.set", ops.Args(`{"inst":"模组服","value":"不是地址!"}`)); ops.CodeOf(err) != 400 {
		t.Fatalf("地址不对应为 400：%v", err)
	}
	propose := func() *ops.Proposal {
		t.Helper()
		res, err := reg.Do(ctx, ai, "music.voicehost.set", args)
		if err != nil {
			t.Fatal(err)
		}
		return res.(*ops.Proposal)
	}
	p := propose()
	if len(p.Plan.Changes) != 1 || p.Plan.Changes[0] != (ops.Change{What: "voice_host", Before: "（空）", After: "frp-abc.example.com:34567"}) {
		t.Fatalf("预览：%+v", p.Plan)
	}
	write("# Simple Voice Chat server config\nport=24455\nvoice_host=\n") // 确认前有人改了端口
	if _, err := reg.Confirm(ctx, ui, p.ID, false); ops.CodeOf(err) != 409 {
		t.Fatalf("文件变了，提案应作废：%v", err)
	}
	p = propose()
	if _, err := reg.Confirm(ctx, ui, p.ID, false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), "voice_host=frp-abc.example.com:34567") || !strings.Contains(string(b), "port=24455") {
		t.Fatalf("写进去的：%s", b)
	}
	// 再提一次：已经是这个值了，预览说不用改
	if p := propose(); len(p.Plan.Changes) != 0 || !strings.Contains(p.Plan.Summary, "不用改") {
		t.Fatalf("没变时的预览：%+v", p.Plan)
	}
}
