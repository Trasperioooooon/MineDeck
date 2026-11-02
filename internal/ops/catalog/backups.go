package catalog

import (
	"context"
	"fmt"

	"minedeck/internal/ops"
)

type backupArgs struct {
	Inst  string `json:"inst"`
	File  string `json:"file"`
	Label string `json:"label"`
}

func (c *cat) backupOps() {
	c.reg.Register(&ops.Op{
		Name: "instance.backups.list", Title: "查看备份列表", Level: ops.Read,
		Desc:   "实例的全部备份（文件名、大小、时间），新的在前。",
		Params: withInst(nil),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			return c.Mgr.ListBackups(p.Inst)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.backup.create", Title: "立即备份", Level: ops.Write,
		Desc:   "备份世界存档；服务器运行中做热备（暂停自动保存 → 保存 → 压缩 → 恢复）。超出保留份数时删掉最旧的。",
		Params: withInst(map[string]*ops.Schema{"label": ops.String("备注，会写进文件名（最多 20 个字）")}),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[backupArgs](a)
			if err != nil {
				return nil, err
			}
			if _, err := c.get(p.Inst); err != nil {
				return nil, err
			}
			return &ops.Plan{Summary: fmt.Sprintf("立即备份「%s」的世界", p.Inst)}, nil
		},
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[backupArgs](a)
			if err != nil {
				return nil, err
			}
			f, err := c.Mgr.CreateBackup(p.Inst, p.Label)
			if err != nil {
				return nil, err
			}
			return map[string]string{"file": f}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.backup.restore", Title: "还原备份", Level: ops.Danger,
		Desc:   "用备份覆盖当前世界。服务器必须已停止；还原前自动再备份一次当前世界。",
		Params: withInst(map[string]*ops.Schema{"file": ops.String("备份文件名（见 instance.backups.list）")}, "file"),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[backupArgs](a)
			if err != nil {
				return nil, err
			}
			_, pre, err := c.statusPre(p.Inst)
			if err != nil {
				return nil, err
			}
			return &ops.Plan{Summary: fmt.Sprintf("把「%s」的世界还原到备份 %s", p.Inst, p.File),
				Warning: "当前世界会被备份覆盖（还原前会自动再备份一次）", Pre: pre}, nil
		},
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[backupArgs](a)
			if err != nil {
				return nil, err
			}
			if err := c.Mgr.RestoreBackup(p.Inst, p.File); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "instance.backup.delete", Title: "删除备份", Level: ops.HumanOnly,
		Params: withInst(map[string]*ops.Schema{"file": ops.String("备份文件名")}, "file"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[backupArgs](a)
			if err != nil {
				return nil, err
			}
			if err := c.Mgr.DeleteBackup(p.Inst, p.File); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})
}
