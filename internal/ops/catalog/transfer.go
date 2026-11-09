package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/mcsrc"
	"minedeck/internal/ops"
	"minedeck/internal/transfer"
)

// 服务器打包 / 收包（internal/transfer 是共享引擎，两个操作走 inst 的流水线）。
// 打包要往主机上任意一个位置写文件、收包的上传包也在主机缓存目录里，都和「接管已有服务器」一样：
// 只许人在本机操作，AI 和局域网设备都调不了。收包人拿到的包可能来自另一台电脑，
// 收包操作对包内容照 transfer.Inspect 的校验走，不信任包里写的任何事实之外的东西。

// 返回网页接口上传 .mdpack 落盘的目录（cache/transfer）。运行时求值：cache 目录可以随配置变。
func transferUploadDir() string { return filepath.Join(app.CacheDir, "transfer") }

// transferOps 登记「服务器打包 / 收包」的操作。
func (c *cat) transferOps() {
	const localMsg = "服务器打包 / 收包仅支持在本机操作（要在打包的地方写包文件、读收到的包）"

	modeSchema := ops.String("打包模式：distributable 打包下发（干净包，收包即开服）；handover 服主交接（保留记录文件）").
		OneOf(transfer.ModeDistributable, transfer.ModeHandover)
	fileSchema := ops.String("已上传到缓存目录的 .mdpack 文件")

	c.reg.Register(&ops.Op{
		Name: "instance.transfer.inspect", Title: "清点服务器包", Level: ops.HumanOnly, LocalOnly: true, NoLog: true,
		LocalMsg: localMsg,
		Desc:     "只读地清点一个收到的 .mdpack：服名、核心与版本、世界体量、模组数、导出时间、剔除清单，给接收向导预填。",
		Params:   ops.Object(map[string]*ops.Schema{"file": fileSchema}, "file"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				File string `json:"file"`
			}](a)
			if err != nil {
				return nil, err
			}
			if err := checkTransferFile(p.File); err != nil {
				return nil, err
			}
			return transfer.Inspect(p.File)
		},
	})

	exportParams := map[string]*ops.Schema{
		"inst":     instName,
		"mode":     modeSchema,
		"dest":     ops.String("导出包放在哪：完整路径、或一个目录（自动起名）；仅本机生效"),
		"excludes": ops.Array("额外剔除（实例目录内的相对路径），连同默认清单一起写进包的剔除清单", ops.String("相对路径")),
	}
	c.reg.Register(&ops.Op{
		Name: "instance.transfer.export", Title: "打包服务器", Level: ops.HumanOnly, LocalOnly: true,
		LocalMsg: localMsg,
		Desc:     "把一台停着的服务器打成一个 .mdpack 发给别人（打包下发或服主交接），返回任务编号。剔除清单写进包里，对方收包时看得到。",
		Params:   ops.Object(exportParams, "inst"),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[struct {
				Inst string `json:"inst"`
				Mode string `json:"mode"`
			}](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			st := i.Status()
			if st != "stopped" {
				return nil, ops.Errorf(400, "打包前请先停止服务器")
			}
			sum := fmt.Sprintf("把「%s」（%s %s）打成一个 .mdpack（%s）", p.Inst, i.Core, i.MC, modeName(p.Mode))
			return &ops.Plan{Summary: sum, Pre: map[string]string{"实例状态": st}}, nil
		},
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			req, err := bind[inst.ExportReq](a)
			if err != nil {
				return nil, err
			}
			if _, err := c.get(req.Inst); err != nil {
				return nil, err
			}
			id, err := c.Mgr.ExportPackAsync(req)
			if err != nil {
				return nil, err
			}
			return map[string]string{"taskId": id}, nil
		},
	})

	importParams := map[string]*ops.Schema{
		"file":      fileSchema,
		"name":      ops.String("落地成哪台实例（实例列表里的名字）"),
		"port":      ops.Integer("端口；0 = 用包里的"),
		"xmxMb":     ops.Integer("最大内存（MB）；0 = 用包里的"),
		"javaMajor": ops.Integer("Java 大版本；0 = 包里写的，包里没写按 Minecraft 版本自动选"),
		"eula":      createParams["eula"],
	}
	c.reg.Register(&ops.Op{
		Name: "instance.transfer.import", Title: "收包开服", Level: ops.HumanOnly, LocalOnly: true,
		LocalMsg: localMsg,
		Desc:     "把服主打来的 .mdpack 解开成一台可以直接开服的实例：世界存档（playerdata 里全员进度）、模组、配置原样落地，再适配名字、端口和 Java，返回任务编号。",
		Params:   ops.Object(importParams, "file", "name"),
		Consent: &ops.Consent{Param: "eula", Text: "我已阅读并同意 Minecraft EULA", URL: "https://aka.ms/MinecraftEULA",
			Need: func(a ops.Args) bool {
				p, _ := bind[struct {
					File string `json:"file"`
				}](a)
				sum, err := transfer.Inspect(p.File)
				if err != nil {
					return false
				}
				var s inst.Settings
				if json.Unmarshal(sum.InstanceRaw, &s) != nil {
					return false
				}
				return !sum.EULAAgreed && mcsrc.KindOf(s.Core) != mcsrc.KindProxy // 包里没同意过、而且不是代理端
			}},
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			req, err := bind[inst.ImportPackReq](a)
			if err != nil {
				return nil, err
			}
			if err := checkTransferFile(req.File); err != nil {
				return nil, err
			}
			id, err := c.Mgr.ImportPackAsync(req)
			if err != nil {
				return nil, err
			}
			return map[string]string{"taskId": id}, nil
		},
	})
}

// checkTransferFile 收包文件只能来自网页接口刚存进 cache/transfer 的上传，避免拿它读主机上别的文件。
func checkTransferFile(file string) error {
	if filepath.Dir(filepath.Clean(file)) != transferUploadDir() {
		return fmt.Errorf("包文件不在上传目录里")
	}
	if !strings.EqualFold(filepath.Ext(file), transfer.Ext) {
		return fmt.Errorf("收的包应是 .mdpack：%s", file)
	}
	if _, err := os.Stat(file); err != nil {
		return fmt.Errorf("上传的包已经不在了，请重新选择文件")
	}
	return nil
}

// modeName 模式的中文叫法。
func modeName(mode string) string {
	switch mode {
	case transfer.ModeHandover:
		return "服主交接"
	case transfer.ModeDistributable:
		return "打包下发"
	}
	return mode
}
