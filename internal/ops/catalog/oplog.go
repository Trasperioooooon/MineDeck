package catalog

import (
	"context"

	"minedeck/internal/ops"
)

func (c *cat) logOps() {
	c.reg.Register(&ops.Op{
		Name: "ops.log", Title: "查看操作记录", Level: ops.Read,
		Desc: "谁在什么时候做了哪些修改（读操作不记），新的在前。来源：ui 本机界面、api 本机程序、lan 局域网设备、" +
			"assistant 应用内助手、gamechat 游戏内 AI、companion AI 伙伴、schedule 定时任务和崩溃自动重启。",
		Params: ops.Object(map[string]*ops.Schema{
			"inst":  ops.String("只看这个实例"),
			"src":   ops.String("只看这个来源"),
			"op":    ops.String("只看这个操作（按前缀匹配，如 instance.players.）"),
			"limit": ops.Integer("最多几条，默认 100，最多 1000"),
		}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			q, err := bind[struct {
				Inst  string     `json:"inst"`
				Src   ops.Source `json:"src"`
				Op    string     `json:"op"`
				Limit int        `json:"limit"`
			}](a)
			if err != nil {
				return nil, err
			}
			return c.reg.Log.Query(ops.Query{Inst: q.Inst, Src: q.Src, Op: q.Op, Limit: q.Limit})
		},
	})
}
