package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"minedeck/internal/app"
	"minedeck/internal/ops"
	"minedeck/internal/transfer"
)

// transferOps 实例交接包（.mdpack）：把一台不再想开的服务器完整地交给另一台电脑续服。
//
// 风险等级沿既有惯例：
//   - export 是写：只往磁盘多写一个 zip（导出的包删掉就恢复原状），影响小、可撤销——同 instance.backup.create（立即备份）；
//   - import 是仅人工：解包会把包里的任意文件落到磁盘并注册成实例，同 instance.import（导入整合包）的先例，
//     AI 和定时任务都不能调；EULA 由人在界面上勾选。
func (c *cat) transferOps() {
	c.reg.Register(&ops.Op{
		Name: "instance.transfer.scan", Title: "读交接包摘要", Level: ops.Read, NoLog: true,
		Desc:   "读上传到缓存目录的 .mdpack 交接包，返回包里是什么：核心、版本、世界大小、模组/插件数量、导出时间。导入向导用。",
		Params: ops.Object(map[string]*ops.Schema{"file": ops.String("已上传到缓存目录的交接包文件")}, "file"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				File string `json:"file"`
			}](a)
			if err != nil {
				return nil, err
			}
			// 只认上传目录里的文件，不能拿它去读主机上别的文件
			if err := inUploadDir(p.File); err != nil {
				return nil, err
			}
			sum, err := transfer.Summarize(p.File)
			if err != nil {
				return nil, err
			}
			// file 一起带回去：向导提交导入时要凭这个路径找包（上传响应直接复用本操作的返回）
			return struct {
				File string `json:"file"`
				*transfer.Summary
			}{File: p.File, Summary: sum}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.transfer.export", Title: "导出交接包", Level: ops.Write,
		Desc: "把实例打包成 .mdpack 交接包发给接手的人（存档、配置、模组全保留，默认不含核心 jar）：世界 + server.properties + mods/plugins + config + 白名单/OP 名单 + eula.txt。" +
			"默认瘦身模式，对方导入时自动重新下载核心；full 为 true 时连核心一起带。返回任务编号（进度见 task.get）。",
		Params: withInst(map[string]*ops.Schema{
			"full": ops.Boolean("完整模式：连服务端核心一起打包（对方导入时就不用再下载）"),
			"dest": ops.String("导出到哪个文件夹（空 = MineDeck 数据缓存目录）；仅本机的请求能指定"),
		}),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[transferArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			if i.Status() != "stopped" {
				return nil, ops.Errorf(400, "交接前请先停止服务器")
			}
			mode := "瘦身模式（核心导入时重新下载）"
			if p.Full {
				mode = "完整模式（核心一并打包）"
			}
			return &ops.Plan{
				Summary: fmt.Sprintf("把「%s」打包成交接包（%s %s，%s）", p.Inst, i.Core, i.MC, mode),
				Changes: []ops.Change{{What: "交接包内容", After: "世界、server.properties、mods/plugins、config、白名单/OP 名单、eula.txt；不含 logs 和崩溃报告"}},
				Warning: "交接包含玩家数据（存档、白名单）。交接前请征得服务器成员同意",
				Pre:     map[string]string{"实例状态": i.Status()},
			}, nil
		},
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[transferArgs](a)
			if err != nil {
				return nil, err
			}
			dest := p.Dest
			if !caller.Local || caller.Source.AI() {
				dest = "" // 远程 / 局域网请求和 AI 不能决定写主机哪个目录，落缓存目录
			}
			id, err := transfer.ExportAsync(c.Mgr, transfer.ExportReq{Inst: p.Inst, Full: p.Full, Dest: dest})
			if err != nil {
				return nil, err
			}
			return map[string]string{"taskId": id}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.transfer.import", Title: "导入交接包", Level: ops.HumanOnly,
		Desc: "从已上传的 .mdpack 交接包创建实例：存档、配置、模组全保留。瘦身的包按清单重新下载核心，" +
			"完整模式（包里带核心）不再联网下载。返回任务编号（进度见 task.get）。",
		Params: ops.Object(map[string]*ops.Schema{
			"file":  ops.String("已上传到缓存目录的交接包文件"),
			"name":  createParams["name"],
			"xmxMb": ops.Integer("最大内存 MB；0 = 沿用包里原来分配的"),
			"port":  ops.Integer("端口；0 = 沿用包内 server.properties 的端口；给了且与包内不同则改写"),
			"eula":  ops.Boolean("Minecraft EULA（包里已同意过时不用再勾）"),
			"root":  ops.String("自定义存放根目录（仅本机生效）"),
		}, "file", "name"),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[transferArgs](a)
			if err != nil {
				return nil, err
			}
			if err := inUploadDir(p.File); err != nil {
				return nil, err
			}
			root := p.Root
			if !caller.Local {
				root = "" // 同 instance.import：远程 / 局域网请求不得指定主机任意路径
			}
			sum, err := transfer.Summarize(p.File)
			if err != nil {
				return nil, err
			}
			id, err := transfer.ImportAsync(c.Mgr, p.File, transfer.ImportReq{
				Name: p.Name, XmxMB: p.XmxMB, Port: p.Port, EULA: p.EULA, Root: root,
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"taskId": id,
				"pack": map[string]any{
					"name": sum.Name, "core": sum.Core, "mc": sum.MC, "build": sum.Build,
					"mods": sum.Mods, "plugins": sum.Plugins, "full": sum.Full,
				},
			}, nil
		},
	})
}

type transferArgs struct {
	Inst  string `json:"inst"`
	File  string `json:"file"`
	Name  string `json:"name"`
	Full  bool   `json:"full"`
	Dest  string `json:"dest"`
	XmxMB int    `json:"xmxMb"`
	Port  int    `json:"port"`
	EULA  bool   `json:"eula"`
	Root  string `json:"root"`
}

// inUploadDir 校验交接包文件在网页接口的上传目录里（否则拒绝读它——参数里的路径不可信）。
func inUploadDir(file string) error {
	if filepath.Dir(filepath.Clean(file)) != filepath.Join(app.CacheDir, "transfer", "import") {
		return fmt.Errorf("交接包文件不在上传目录里")
	}
	if _, err := os.Stat(file); err != nil {
		return ops.Errorf(404, "交接包文件不在了，请重新上传")
	}
	if !strings.EqualFold(filepath.Ext(file), ".mdpack") && !strings.EqualFold(filepath.Ext(file), ".zip") {
		return fmt.Errorf("只支持 .mdpack 交接包")
	}
	return nil
}
