package catalog

import (
	"context"

	"minedeck/internal/ops"
)

func (c *cat) taskCancelOps() {
	c.reg.Register(&ops.Op{
		Name: "task.cancel", Title: "取消创建任务", Level: ops.HumanOnly,
		Desc:   "取消尚未完成的新建或整合包导入任务，等待清理半成品后结束。开始登记实例以后不能取消。",
		Params: ops.Object(map[string]*ops.Schema{"id": ops.String("任务编号")}, "id"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				ID string `json:"id"`
			}](a)
			if err != nil {
				return nil, err
			}
			if c.Mgr.Tasks.Get(p.ID) == nil {
				return nil, ops.Errorf(404, "任务不存在")
			}
			if !c.Mgr.Tasks.Cancel(p.ID) {
				return nil, ops.Errorf(409, "这个任务已经结束或正在完成登记，现在不能取消")
			}
			return "正在取消，清理完成后任务会结束", nil
		},
	})
}
