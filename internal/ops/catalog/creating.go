package catalog

import (
	"context"
	"errors"
	"strings"

	"minedeck/internal/inst"
	"minedeck/internal/ops"
)

// 上次创建没走完的文件夹。只列、只删带创建记号且还没登记的目录。
// 不自动删，confirm 必须为 true。接管进来的目录没有这个记号。
func (c *cat) creatingOps() {
	c.reg.Register(&ops.Op{
		Name: "instance.creating.list", Title: "查看没建完的文件夹", Level: ops.HumanOnly, LocalOnly: true, NoLog: true,
		LocalMsg: "查看没建完的文件夹仅支持在本机操作",
		Desc: "列出服务器根目录下、带有创建记号、还没有 instance.json 的文件夹。有登记的服务器只去掉记号，不出现在这里。" +
			"没有记号的目录（接管、自己拷进来的）不会列出。",
		Run: func(context.Context, ops.Caller, ops.Args) (any, error) {
			return map[string]any{"items": c.Mgr.CreatingLeftovers()}, nil
		},
	})
	c.reg.Register(&ops.Op{
		Name: "instance.creating.discard", Title: "删除没建完的文件夹", Level: ops.HumanOnly, LocalOnly: true,
		LocalMsg: "删除没建完的文件夹仅支持在本机操作",
		Desc: "按 instance.creating.list 给出的 id 删除一个没建完的文件夹。confirm 必须为 true。" +
			"正在创建、已经登记、没有记号或是链接的，都不删。里面如果有世界文件，调用方要先告诉用户。",
		Params: ops.Object(map[string]*ops.Schema{
			"id":      ops.String("instance.creating.list 里的 id"),
			"confirm": ops.Boolean("用户已确认删除这个没建完的文件夹，包括里面的文件"),
		}, "id", "confirm"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				ID      string `json:"id"`
				Confirm bool   `json:"confirm"`
			}](a)
			if err != nil {
				return nil, err
			}
			if err := c.Mgr.DiscardCreatingLeftover(p.ID, p.Confirm); err != nil {
				msg := strings.TrimPrefix(err.Error(), inst.ErrCreatingConflict.Error()+"：")
				if errors.Is(err, inst.ErrCreatingConflict) {
					return nil, ops.Errorf(409, "%s", msg)
				}
				return nil, ops.Errorf(400, "%s", msg)
			}
			return map[string]any{"removed": p.ID}, nil
		},
	})
}
