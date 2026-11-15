package catalog

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"minedeck/internal/inst"
	"minedeck/internal/ops"
)

// portOwner 占着端口的一个进程。
type portOwner struct {
	PID      int    `json:"pid"`
	Process  string `json:"process,omitempty"`  // 程序名，如 java.exe
	Instance string `json:"instance,omitempty"` // 是 MineDeck 管着的哪个实例
	MineDeck bool   `json:"minedeck,omitempty"` // 是 MineDeck 自己
}

// portState 一个协议上的检查结果。
type portState struct {
	Free   bool        `json:"free"`
	Owners []portOwner `json:"owners,omitempty"`
	Note   string      `json:"note,omitempty"`
}

func (c *cat) portOps() {
	c.reg.Register(&ops.Op{
		Name: "port.check", Title: "检查端口占用", Level: ops.Read,
		Desc: "看这台电脑上某个端口能不能用；被占了的话，是哪个程序（进程编号、程序名，是不是 MineDeck 管着的实例）。" +
			"也列出 server.properties 里用这个端口的实例。",
		Params: ops.Object(map[string]*ops.Schema{
			"port":  ops.Integer("端口").Range(1, 65535),
			"proto": ops.String("tcp（游戏端口）、udp（语音模组等）；空串两种都查").OneOf("", "tcp", "udp"),
		}, "port"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Port  int    `json:"port"`
				Proto string `json:"proto"`
			}](a)
			if err != nil {
				return nil, err
			}
			pids := map[int]string{} // 进程编号 → 实例名
			var configured []map[string]any
			for _, i := range c.Mgr.List() {
				if pid := i.PID(); pid > 0 {
					pids[pid] = i.Name
				}
				if i.Port() == p.Port {
					configured = append(configured, map[string]any{"instance": i.Name, "status": i.Status()})
				}
			}
			check := func(udp bool) portState {
				var st portState
				// 先查谁在监听：Windows 上只占了 0.0.0.0 的端口，再绑一次 :端口（IPv6 双栈）照样成功，
				// 光靠「试着绑」会把被占说成空闲（测试报告 E-09）
				for _, pid := range portOwners(p.Port, udp) {
					st.Owners = append(st.Owners, portOwner{PID: pid, Process: processName(pid), Instance: pids[pid], MineDeck: pid == os.Getpid()})
				}
				if len(st.Owners) > 0 {
					return st
				}
				// 查不到（没权限，或者 wine 之类查不了进程）：再试着绑一次
				if udp {
					pc, err := net.ListenPacket("udp", fmt.Sprintf("%s:%d", inst.PortProbeHost, p.Port))
					st.Free = err == nil
					if pc != nil {
						pc.Close()
					}
				} else {
					ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", inst.PortProbeHost, p.Port))
					st.Free = err == nil
					if ln != nil {
						ln.Close()
					}
				}
				if !st.Free {
					st.Note = "端口被占用了，但没查到是哪个程序（可能需要管理员权限）"
				}
				return st
			}
			out := map[string]any{"port": p.Port}
			if p.Proto != "udp" {
				out["tcp"] = check(false)
			}
			if p.Proto != "tcp" {
				out["udp"] = check(true)
			}
			if configured != nil {
				out["instances"] = configured
			}
			return out, nil
		},
	})
}

// portSuggestOps 端口默认值（可用性走查 R08）：新建、导入整合包、接管、用已有地图开服的表单从这里拿默认端口，
// 并拿 used 当场提示手填的端口和哪台服务器撞了。
func (c *cat) portSuggestOps() {
	c.reg.Register(&ops.Op{
		// NoLog：只读，表单每打开一次就问一次，记进操作记录只会把真正的修改淹掉
		Name: "port.suggest", Title: "推荐服务器端口", Level: ops.HumanOnly, NoLog: true,
		Desc: "新建、导入、接管服务器时端口填哪个：从 from（默认 25565）起，MineDeck 里别的服务器都没用的最小端口（port）；" +
			"usedBy 是 from 被哪台服务器用着（空 = from 本来就空着）；used 列出各台服务器用的端口（代理端是它自己配置里的端口）。" +
			"只看 MineDeck 管着的服务器，别的程序占着的用 port.check 查。except 填一台服务器的名字时不算它（给它自己换端口时用）。" +
			"只读，但只给界面的表单用，不进 AI 的工具清单（AI 要知道端口用 port.check、instance.list）。",
		Params: ops.Object(map[string]*ops.Schema{
			"from":   ops.Integer("从哪个端口起找，默认 25565").Range(0, 65535),
			"except": ops.String("不算在内的服务器名字，可以不填"),
		}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				From   int    `json:"from"`
				Except string `json:"except"`
			}](a)
			if err != nil {
				return nil, err
			}
			used := c.Mgr.PortsInUse(p.Except)
			port, by := inst.DefaultPort(used, p.From)
			from := p.From
			if from < 1 {
				from = 25565
			}
			out := map[string]any{"port": port, "from": from, "used": used}
			if by != "" {
				out["usedBy"] = by
			}
			return out, nil
		},
	})
}

// baseExe 取路径里的程序名。
func baseExe(p string) string { return filepath.Base(strings.ReplaceAll(p, `\`, "/")) }
