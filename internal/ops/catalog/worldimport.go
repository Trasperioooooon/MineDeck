package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"minedeck/internal/mcsrc"
	"minedeck/internal/ops"
	"minedeck/internal/worldimport"
)

// worldOps 地图导入（internal/worldimport）：把已有的 Java 版世界用于新建服务器，或者替换一台服务器正在用的默认世界。
//
// 风险等级：
//   - 扫描本机文件夹会读主机上的任意路径：仅人工 + 仅本机。局域网上的人只能上传自己的 ZIP。
//   - 生成计划、读说明书不改任何东西，但它们只对发起扫描的人有意义（结果绑定调用方）：仅人工、不记操作记录。
//   - 执行导入、换回旧图、删除恢复点会动世界文件：仅人工，AI 和定时任务都调不了；后端自己核对计划和确认词，
//     不靠界面上的弹窗（ops.allow 对人工来源不再二次确认）。
//   - 世界状态（恢复点列表）是读操作，AI 也能看；给 AI 的结果去掉本机路径。
func (c *cat) worldOps() {
	if c.World == nil {
		return
	}
	w := c.World
	owner := func(cl ops.Caller) string { return string(cl.Source) + "|" + cl.Actor }

	c.reg.Register(&ops.Op{
		Name: "world.import.scanpath", Title: "检测本机的地图", Level: ops.HumanOnly, LocalOnly: true, NoLog: true,
		LocalMsg: "选本机的地图文件夹只能在这台电脑上操作；在别的设备上请上传地图的 ZIP 压缩包",
		Desc:     "读本机的一个世界文件夹（或 ZIP 文件），认出里面的地图，返回候选和检查结果。",
		Params:   ops.Object(map[string]*ops.Schema{"path": ops.String("本机的文件夹或 ZIP 文件路径")}, "path"),
		Run: func(_ context.Context, cl ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Path string `json:"path"`
			}](a)
			if err != nil {
				return nil, err
			}
			return w.ScanPath(owner(cl), p.Path)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "world.import.scan", Title: "检测上传的地图", Level: ops.HumanOnly, NoLog: true,
		Desc: "读已经上传到缓存目录的地图文件，返回候选和检查结果。",
		Params: ops.Object(map[string]*ops.Schema{
			"file": ops.String("上传后在缓存目录里的文件"),
			"name": ops.String("上传时的文件名（只用来显示和认扩展名）"),
		}, "file"),
		Run: func(_ context.Context, cl ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				File string `json:"file"`
				Name string `json:"name"`
			}](a)
			if err != nil {
				return nil, err
			}
			if _, err := os.Stat(p.File); err != nil {
				return nil, ops.Errorf(404, "上传的文件不在了，请重新上传")
			}
			return w.ScanUpload(owner(cl), p.File, p.Name)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "world.import.readme", Title: "查看地图说明书", Level: ops.HumanOnly, NoLog: true,
		Params: ops.Object(map[string]*ops.Schema{
			"scanId": ops.String("检测编号"), "candidate": ops.String("候选地图"), "file": ops.String("说明文件（相对来源）"),
		}, "scanId", "candidate", "file"),
		Run: func(_ context.Context, cl ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				ScanID    string `json:"scanId"`
				Candidate string `json:"candidate"`
				File      string `json:"file"`
			}](a)
			if err != nil {
				return nil, err
			}
			text, err := w.Readme(owner(cl), p.ScanID, p.Candidate, p.File)
			if err != nil {
				return nil, err
			}
			return map[string]string{"text": text}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "world.import.plan", Title: "预览地图导入", Level: ops.HumanOnly, NoLog: true,
		Desc: "按选中的地图和目标服务器（或要新建的核心和版本）生成导入计划：版本关系、要替换的世界、会保留的东西、检查结果。不改任何东西。",
		Params: ops.Object(map[string]*ops.Schema{
			"scanId":    ops.String("检测编号"),
			"candidate": ops.String("候选地图编号"),
			"inst":      ops.String("要替换地图的服务器"),
			"newInst":   ops.Boolean("新建服务器时用这张地图"),
			"core":      ops.String("新建时的核心"),
			"mc":        ops.String("新建时的 Minecraft 版本"),
		}, "scanId", "candidate"),
		Run: func(ctx context.Context, cl ops.Caller, a ops.Args) (any, error) {
			req, err := bind[worldimport.PlanReq](a)
			if err != nil {
				return nil, err
			}
			if !req.NewInst && req.Inst == "" {
				return nil, fmt.Errorf("请选择要替换地图的服务器")
			}
			return w.MakePlan(ctx, owner(cl), req)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "world.import.run", Title: "导入并替换当前地图", Level: ops.HumanOnly,
		Desc: "按导入计划替换服务器的地图：复制到暂存区，停服（选了的话），把旧世界整体移进恢复点，换上新地图。" +
			"confirm 必须是 replace；计划里有要确认的项时 acknowledge 为 true。返回任务编号。",
		Params: ops.Object(map[string]*ops.Schema{
			"planId":      ops.String("导入计划编号"),
			"confirm":     ops.String("明确确认：replace"),
			"acknowledge": ops.Boolean("已确认预览里要确认的项"),
			"settings":    ops.Array("要改的服务器设置（计划里列出的键）", ops.String("键名")),
			"stopFirst":   ops.Boolean("服务器在运行时先停服（等它自己退出，不强杀）"),
		}, "planId", "confirm"),
		Run: func(_ context.Context, cl ops.Caller, a ops.Args) (any, error) {
			req, err := bind[worldimport.ExecReq](a)
			if err != nil {
				return nil, err
			}
			id, err := w.Execute(owner(cl), req)
			if err != nil {
				return nil, err
			}
			return map[string]string{"taskId": id}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "world.import.cancel", Title: "取消地图导入", Level: ops.HumanOnly,
		Desc:   "取消正在复制的地图导入（已经开始换地图的会做完或退回再结束）。",
		Params: ops.Object(map[string]*ops.Schema{"taskId": ops.String("任务编号")}, "taskId"),
		Run: func(_ context.Context, cl ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				TaskID string `json:"taskId"`
			}](a)
			if err != nil {
				return nil, err
			}
			if !w.Cancel(owner(cl), p.TaskID) { // 只有发起导入的人能取消
				return nil, fmt.Errorf("这个任务已经不能取消了")
			}
			return "ok", nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "world.import.discard", Title: "放弃地图检测结果", Level: ops.HumanOnly, NoLog: true,
		Params: ops.Object(map[string]*ops.Schema{"scanId": ops.String("检测编号")}, "scanId"),
		Run: func(_ context.Context, cl ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				ScanID string `json:"scanId"`
			}](a)
			if err != nil {
				return nil, err
			}
			w.Discard(owner(cl), p.ScanID)
			return "ok", nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "world.status", Title: "查看地图与恢复点", Level: ops.Read,
		Desc:   "服务器当前的世界名（level-name）、是否正在换地图，以及以前导入地图留下的恢复点（能不能换回导入前的地图）。",
		Params: withInst(nil),
		Run: func(_ context.Context, cl ops.Caller, a ops.Args) (any, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			st, err := w.Status(p.Inst)
			if err != nil {
				return nil, err
			}
			if cl.Source.AI() || !cl.Local { // 本机路径不给 AI，也不给别的设备
				for k := range st.Points {
					if n := len(st.Points[k].Stuck); n > 0 {
						st.Points[k].Stuck = []string{fmt.Sprintf("（%d 个文件夹，在这台电脑上查看）", n)}
					}
				}
			}
			return st, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "world.restore", Title: "恢复导入前的地图", Level: ops.HumanOnly,
		Desc: "把服务器换回某次导入之前的地图（服务器要停着）。导入以后服务器改写过的那份地图会留在旁边，不删。confirm 必须是 restore。",
		Params: withInst(map[string]*ops.Schema{
			"id": ops.String("导入记录编号"), "confirm": ops.String("明确确认：restore"),
		}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Inst    string `json:"inst"`
				ID      string `json:"id"`
				Confirm string `json:"confirm"`
			}](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			if i.Status() != "stopped" {
				return nil, fmt.Errorf("请先停止服务器再换回旧地图")
			}
			id, err := w.Restore(p.Inst, p.ID, p.Confirm)
			if err != nil {
				return nil, err
			}
			return map[string]string{"taskId": id}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "world.point.delete", Title: "删除地图恢复点", Level: ops.HumanOnly,
		Desc: "删掉一次导入留下的恢复点（导入前的旧地图，或换回时留在旁边的导入地图），删了就不能再换回。confirm 必须是 delete。",
		Params: withInst(map[string]*ops.Schema{
			"id": ops.String("导入记录编号"), "confirm": ops.String("明确确认：delete"),
		}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Inst    string `json:"inst"`
				ID      string `json:"id"`
				Confirm string `json:"confirm"`
			}](a)
			if err != nil {
				return nil, err
			}
			if err := w.DeletePoint(p.Inst, filepath.Base(p.ID), p.Confirm); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "world.recover.retry", Title: "重试恢复地图", Level: ops.HumanOnly,
		Desc:   "换地图没完成、自动恢复失败时，处理掉占用的程序以后再试一次自动恢复。",
		Params: withInst(nil),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			if err := w.Retry(p.Inst); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})
}

// worldCreateCheck 新建服务器带着地图时，核对计划（由 instance.create 调）。
func (c *cat) worldCreateCheck(cl ops.Caller, plan string, core mcsrc.Core, mc string, ack bool) error {
	if c.World == nil {
		return fmt.Errorf("地图导入功能没有准备好")
	}
	if cl.Source.AI() {
		return ops.Errorf(403, "用已有地图开服只能由人操作")
	}
	return c.World.CheckCreatePlan(string(cl.Source)+"|"+cl.Actor, plan, core, mc, ack)
}
