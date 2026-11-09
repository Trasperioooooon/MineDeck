package inst

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/java"
	"minedeck/internal/mcsrc"
	"minedeck/internal/tasks"
	"minedeck/internal/transfer"
)

// 服务器打包 / 收包（.mdpack，internal/transfer 是共享引擎）：
//
//   - 打包（instance.transfer.export）：把一台停着的实例打成一个 .mdpack 交给别人。两种模式：
//     distributable（打包下发）面向「收包的人直接开服」，剔除日志与可重建的缓存；
//     handover（服主交接）面向下一任服主，尽量保留记录文件。
//   - 收包（instance.transfer.import）：把服主打来的 .mdpack 解开成一台可以直接开服的实例。
//     世界存档（连同 playerdata 里每个人的进度）、模组、配置原样落地，再按收包人的选择适配
//     实例名、端口和 Java。

// ExportReq 打包请求。
type ExportReq struct {
	Inst     string   // 实例名
	Mode     string   // handover | distributable
	Dest     string   // 目标 .mdpack：以 .mdpack 结尾按文件名算，否则当目录、自动起名
	Excludes []string // 额外剔除（相对实例目录的路径，写进包的剔除清单）
}

// ImportPackReq 收包请求。
type ImportPackReq struct {
	File      string // .mdpack 的路径（操作层保证在缓存目录里）
	Name      string // 落地成哪台实例
	Port      int    // 0 = 用包里的端口
	XmxMB     int    // 0 = 用包里的内存
	JavaMajor int    // 0 = 包里写的 Java；包里没写按 MC 版本自动选
	EULA      bool   // 包内 eula.txt 已同意过时不用勾
}

var (
	packSteps       = []string{"清点实例文件", "压缩打包"}
	importPackSteps = []string{"清点包内容", "解压到实例目录", "适配配置文件", "准备 Java 并登记"}
)

// ExportPackAsync 校验请求并在后台打包，返回任务编号。
func (m *Manager) ExportPackAsync(req ExportReq) (string, error) {
	i, err := m.Get(req.Inst)
	if err != nil {
		return "", err
	}
	if i.Status() != "stopped" {
		return "", fmt.Errorf("打包前请先停止服务器（打的是完整包，运行中的世界随时在变）")
	}
	if !transfer.ValidMode(req.Mode) {
		return "", fmt.Errorf("打包模式不认识：%s（可用 handover / distributable）", req.Mode)
	}
	dest, err := packDest(req.Dest, i)
	if err != nil {
		return "", err
	}
	xmx, _, _ := i.MemSnapshot()
	t, ctx := m.Tasks.New(fmt.Sprintf("打包服务器 %s（%s %s）", req.Inst, i.Core, i.MC), packSteps)
	t.SetKind("pack")
	go func() {
		if err := m.runPack(ctx, t, i, req, dest, xmx); err != nil {
			t.Fail(err)
			os.Remove(dest)
			return
		}
		t.Finish(dest)
	}()
	return t.ID(), nil
}

// packDest 规范目标路径：空的按「导出包放的文件夹」选择处理；给目录就起个名。
// 打进实例自己的目录里绝不行：下一个包会把它带进去。
func packDest(dest string, i *Instance) (string, error) {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", fmt.Errorf("请先选择导出包放在哪")
	}
	if !filepath.IsAbs(dest) {
		return "", fmt.Errorf("请填完整的路径（如 D:\\MinePack\\好友服.mdpack）")
	}
	dest = filepath.Clean(dest)
	if !strings.EqualFold(filepath.Ext(dest), transfer.Ext) {
		dest = filepath.Join(dest, fmt.Sprintf("%s%s", sanitizeLabel(i.Name)+"-"+time.Now().Format("20060102-150405"), transfer.Ext))
	}
	if dir := realPath(filepath.Dir(dest)); dir != "" {
		if within(dir, realPath(i.Dir)) {
			return "", fmt.Errorf("导出包不能放在这台服务器自己的目录里（%s）", i.Dir)
		}
	}
	return dest, nil
}

func (m *Manager) runPack(ctx context.Context, t *tasks.Task, i *Instance, req ExportReq, dest string, xmx int) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Minute)
	defer cancel()

	// ---- 步骤 0：清点 ----
	t.StartStep(0)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("导出目录建不起来: %w", err)
	}
	mode := "打包下发（收包即全员进度都在）"
	if req.Mode == transfer.ModeHandover {
		mode = "服主交接（保留服务器怎么跑过的记录）"
	}
	t.Logf("服务器：%s（%s %s），内存 %d MB", i.Name, i.Core, i.MC, xmx)
	t.Logf("模式：%s", mode)
	t.Logf("剔除：%s", strings.Join(transfer.ExcludedFor(req.Mode), "、"))

	// ---- 步骤 1：压缩打包 ----
	t.StartStep(1)
	raw, err := os.ReadFile(filepath.Join(i.Dir, "instance.json"))
	if err != nil {
		return fmt.Errorf("读不了 instance.json: %w", err)
	}
	manifest, err := transfer.Pack(dest, transfer.PackOptions{
		Dir: i.Dir, Instance: raw, Mode: req.Mode, Excludes: req.Excludes,
		ModDir: modsDirName(i), Exporter: "MineDeck " + app.Version,
		Progress: t.ProgressFn("压缩打包"),
	})
	if err != nil {
		return err
	}
	st, _ := os.Stat(dest)
	size := int64(-1)
	if st != nil {
		size = st.Size()
	}
	t.Logf("打包完成 ✔ %s（%.1f GB）", filepath.Base(dest), float64(size)/(1<<30))
	t.Logf("世界 %.1f MB，模组 %d 个", float64(manifest.WorldSize)/(1<<20), len(manifest.Mods))
	return nil
}

// modsDirName 这台实例装模组或插件的目录：插件与代理端是 plugins，其余按 mods
// （和操作层 resourceSpec 的判定一致；目录不存在时清单为空，不影响打包）。
func modsDirName(i *Instance) string {
	switch i.Core {
	case mcsrc.CorePaper, mcsrc.CorePurpur, mcsrc.CoreLeaves, mcsrc.CoreFolia,
		mcsrc.CoreVelocity, mcsrc.CoreWaterfall:
		return "plugins"
	}
	return "mods"
}

// ImportPackAsync 校验请求并在后台收包，返回任务编号。
func (m *Manager) ImportPackAsync(req ImportPackReq) (string, error) {
	sum, err := transfer.Inspect(req.File)
	if err != nil {
		return "", err
	}
	var s Settings
	if err := json.Unmarshal(sum.InstanceRaw, &s); err != nil || s.Name == "" {
		return "", fmt.Errorf("包里的 instance.json 读不出来（%v）。请让服主用 MineDeck 重新打一份", err)
	}
	req.Name = strings.TrimSpace(req.Name)
	if err := validateName(req.Name); err != nil {
		return "", err
	}
	if req.XmxMB < 0 {
		return "", fmt.Errorf("内存数值不对")
	}
	if req.Port < 0 || req.Port > 65535 {
		return "", fmt.Errorf("端口需为 1 ~ 65535 的整数")
	}
	if req.JavaMajor < 0 || req.JavaMajor > 30 {
		return "", fmt.Errorf("Java 版本应在 8 到 30 之间")
	}
	if req.Port == 0 {
		req.Port = sum.Port
	}
	kind := mcsrc.KindOf(s.Core)
	needEULA := kind != mcsrc.KindProxy && !sum.EULAAgreed
	if needEULA && !req.EULA {
		return "", fmt.Errorf("需要同意 Minecraft EULA 才能收包开服")
	}
	if sum.Instance.JavaMajor > 30 {
		return "", fmt.Errorf("包里的 instance.json 写着 Java %d，这不是正常的版本号，请让服主重新打一份", sum.Instance.JavaMajor)
	}

	// 占位 + 目标目录（同 validateCreate 的做法：目录名用 slug，重名自动加序号）
	root := app.ServersRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("存放目录不可用: %w", err)
	}
	m.mu.Lock()
	if _, taken := m.insts[req.Name]; taken {
		m.mu.Unlock()
		return "", fmt.Errorf("实例名已存在: %s", req.Name)
	}
	if m.creating[req.Name] {
		m.mu.Unlock()
		return "", fmt.Errorf("「%s」正在创建中，请稍候", req.Name)
	}
	dir := filepath.Join(root, slugify(req.Name))
	for n := 2; ; n++ {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			break
		}
		if n > 999 {
			m.mu.Unlock()
			return "", fmt.Errorf("无法为实例分配目录（同名目录过多）")
		}
		dir = filepath.Join(root, fmt.Sprintf("%s-%d", slugify(req.Name), n))
	}
	m.creating[req.Name] = true
	m.creating[dirKey(dir)] = true
	m.mu.Unlock()

	title := fmt.Sprintf("收包开服 %s（%s %s）", req.Name, s.Core, s.MC)
	if s.Core == "" {
		title = fmt.Sprintf("收包开服 %s", req.Name)
	}
	t, ctx := m.Tasks.New(title, importPackSteps)
	t.SetKind("unpack")
	go func() {
		defer m.releaseCreating(req.Name)
		defer m.releaseCreating(dirKey(dir))
		if err := m.runImportPack(ctx, t, req, sum, s, dir, needEULA); err != nil {
			t.Fail(err)
			os.RemoveAll(dir) // 收包建的是全新目录，失败整个清掉
			return
		}
		t.Finish(req.Name)
	}()
	return t.ID(), nil
}

func (m *Manager) runImportPack(ctx context.Context, t *tasks.Task, req ImportPackReq, sum *transfer.Summary, s Settings, dir string, needEULA bool) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Minute)
	defer cancel()

	// ---- 步骤 0：清点 ----
	t.StartStep(0)
	packSize := float64(sum.PackSize) / (1 << 20)
	t.Logf("包：%s（%s，%.1f MB，%d 个文件）", s.Name, sum.Mode, packSize, sum.Entries)
	t.Logf("服主 %s 打的包，导出于 %s", exporterName(sum.Exporter), sum.ExportedAt.Local().Format("2006-01-02 15:04"))
	t.Logf("核心：%s %s；世界 %.1f MB（全员进度在 playerdata 里，原样带来）；模组 %d 个", s.Core, s.MC, float64(sum.WorldSize)/(1<<20), sum.ModCount)
	t.Logf("剔除：%s", strings.Join(sum.Excluded, "、"))

	// ---- 步骤 1：解压 ----
	t.StartStep(1)
	if _, err := transfer.Unpack(req.File, dir, t.ProgressFn("解压包内容")); err != nil {
		return err
	}
	t.Logf("解压完成，落在 %s", dir)

	// ---- 步骤 2：适配配置 ----
	t.StartStep(2)
	if req.Port > 0 && req.Port != sum.Port {
		if err := patchProps(dir, "server-port", fmt.Sprintf("%d", req.Port)); err != nil {
			return fmt.Errorf("改端口失败: %w", err)
		}
		t.Logf("端口按你的选择改成 %d（包里原来是 %d）", req.Port, sum.Port)
	}
	if needEULA { // 包里没同意过、这次勾了才写
		if err := os.WriteFile(filepath.Join(dir, "eula.txt"),
			[]byte("# 用户已在 MineDeck 中确认同意 Minecraft EULA (https://aka.ms/MinecraftEULA)\neula=true\n"), 0o644); err != nil {
			return err
		}
		t.Logf("已写入 eula.txt（你在上一步同意了 Minecraft EULA）")
	}
	if _, err := os.Stat(filepath.Join(dir, launchFile(s.LaunchTarget))); err != nil {
		return fmt.Errorf("包里的启动文件不见了：%s（请让服主重新打一份）", launchFile(s.LaunchTarget))
	}
	inst := &Instance{
		Settings: s,
		Dir:      dir, Console: newConsole(),
	}
	inst.Name = req.Name
	if req.XmxMB > 0 && req.XmxMB != s.XmxMB {
		inst.XmxMB, inst.XmsMB = req.XmxMB, min(1024, req.XmxMB)
	}
	if inst.XmxMB < 512 {
		inst.XmxMB, inst.XmsMB = 2048, 1024
	}
	inst.CreatedAt = time.Now()

	// ---- 步骤 3：准备 Java 并登记 ----
	t.StartStep(3)
	major := req.JavaMajor
	if major == 0 {
		major = sum.Instance.JavaMajor // 包里写的 Java 保真优先：魔改模组可能要的就是它
	}
	if major == 0 {
		if mcsrc.KindOf(inst.Core) == mcsrc.KindProxy {
			major = mcsrc.FixedJavaOf(inst.Core)
		} else {
			if meta, err := mcsrc.GetVersionMeta(ctx, inst.MC); err == nil {
				major = meta.JavaMajor
			} else {
				t.Logf("没拿到 MC %s 的官方元数据（%v）", inst.MC, err)
			}
		}
	}
	if major < 8 {
		return fmt.Errorf("没法按版本「%s」自动选 Java（官方清单里没有这个版本，或者现在连不上网）。请在收包向导的「Java」里手动选一个版本再试", inst.MC)
	}
	javaPath, err := java.Resolve(ctx, major, t.Logf, t.ProgressFn(fmt.Sprintf("下载 Java %d", major)))
	if err != nil {
		return err
	}
	inst.JavaMajor, inst.JavaPath = major, javaPath
	if err := writeRunBat(inst); err != nil {
		return err
	}
	if err := m.saveInstance(inst); err != nil {
		return err
	}
	app.RememberRoot(filepath.Dir(dir))
	m.mu.Lock()
	m.insts[inst.Name] = inst
	m.mu.Unlock()
	t.Logf("收包完成 ✔ 存档、模组和配置都齐了，随时可以启动")
	return nil
}

// patchProps 只改 server.properties 的一行，保序保注释（props.go）。
func patchProps(dir, key, value string) error {
	path := filepath.Join(dir, "server.properties")
	p, err := LoadProps(path)
	if err != nil {
		return err
	}
	p.Set(key, value)
	return p.Save(path)
}

// exporterName manifest 里导出方的标识：如 "MineDeck 3.0.0" 原样展示；空则称「对方」。
func exporterName(exporter string) string {
	if strings.TrimSpace(exporter) == "" {
		return "对方"
	}
	return exporter
}
