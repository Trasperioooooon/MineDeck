package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minedeck/internal/java"
	"minedeck/internal/mcsrc"
	"minedeck/internal/ops"
)

// mcOps 核心、版本、构建，以及 Java 运行时。
func (c *cat) mcOps() {
	c.reg.Register(&ops.Op{
		Name: "mc.cores", Title: "查看支持的核心", Level: ops.Read,
		Desc: "12 种服务端核心：id、名称、标签、说明、类型（game / hybrid / proxy）。",
		Run:  func(context.Context, ops.Caller, ops.Args) (any, error) { return mcsrc.Cores(), nil },
	})

	type verArgs struct {
		Core      string `json:"core"`
		Snapshots bool   `json:"snapshots"`
		MC        string `json:"mc"`
	}
	c.reg.Register(&ops.Op{
		Name: "mc.versions", Title: "查看 Minecraft 版本", Level: ops.Read,
		Desc: "某个核心支持的 Minecraft 版本（要联网），新的在前。",
		Params: ops.Object(map[string]*ops.Schema{
			"core":      ops.String("核心 id（见 mc.cores）"),
			"snapshots": ops.Boolean("包含快照版"),
		}),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[verArgs](a)
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			core := mcsrc.Core(p.Core)
			if !mcsrc.ValidCore(core) {
				return nil, fmt.Errorf("未知核心: %s", core)
			}
			vers, err := mcsrc.ListVersions(ctx, core, p.Snapshots)
			if err != nil {
				return nil, ops.Wrap(502, err)
			}
			return vers, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "mc.builds", Title: "查看核心的构建", Level: ops.Read,
		Desc: "某个核心在某个 Minecraft 版本下的构建或加载器版本（要联网）。",
		Params: ops.Object(map[string]*ops.Schema{
			"core": ops.String("核心 id（见 mc.cores）"),
			"mc":   ops.String("Minecraft 版本"),
		}),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[verArgs](a)
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			core := mcsrc.Core(p.Core)
			if !mcsrc.ValidCore(core) || p.MC == "" {
				return nil, fmt.Errorf("参数缺失")
			}
			builds, err := mcsrc.ListBuilds(ctx, core, p.MC)
			if err != nil {
				return nil, ops.Wrap(502, err)
			}
			return builds, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "java.list", Title: "查看 Java", Level: ops.Read,
		Desc: "MineDeck 自带的便携 Java，以及上次扫描到的本机已装 Java。",
		Run: func(context.Context, ops.Caller, ops.Args) (any, error) {
			scanned := java.Cached()
			if scanned == nil {
				scanned = []java.ScannedJava{}
			}
			portable := java.Installed()
			if portable == nil {
				portable = []java.Info{}
			}
			return map[string]any{"portable": portable, "scanned": scanned}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "java.scan", Title: "重新扫描本机 Java", Level: ops.Read,
		Desc: "在常见安装位置找 java.exe 并探测版本（只刷新缓存的列表，不改别的）。",
		Run: func(context.Context, ops.Caller, ops.Args) (any, error) {
			list := java.Scan()
			if list == nil {
				list = []java.ScannedJava{}
			}
			return list, nil
		},
	})

	c.javaSetOp()

	// 仅限本机：path 会被作为可执行文件运行（java -version），远程 / 局域网不得指定主机任意路径
	c.reg.Register(&ops.Op{
		Name: "java.add", Title: "添加本机 Java", Level: ops.HumanOnly, LocalOnly: true,
		LocalMsg: "添加本机 Java 仅支持在本机操作",
		Params:   ops.Object(map[string]*ops.Schema{"path": ops.String("java.exe 或 JDK 目录的路径")}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Path string `json:"path"`
			}](a)
			if err != nil {
				return nil, err
			}
			return java.AddManual(strings.TrimSpace(p.Path))
		},
	})
}

// knownJava 已知的 Java：MineDeck 的便携版和扫描到的本机 Java。返回这个大版本可用的 java.exe；path 给了就必须是其中之一。
func knownJava(major int, path string) (string, bool) {
	var cands []string
	for _, j := range java.Installed() {
		if j.Major == major {
			cands = append(cands, j.Path)
		}
	}
	for _, j := range java.Cached() {
		if j.Major == major {
			cands = append(cands, j.Path)
		}
	}
	for _, p := range cands {
		if path == "" || strings.EqualFold(filepath.Clean(p), filepath.Clean(path)) {
			if _, err := os.Stat(p); err == nil {
				return p, true
			}
		}
	}
	return "", false
}

// javaSetOp 给实例换 Java（§5.2 java_set）：已有这个版本就直接换，没有就在后台下载便携版再换（返回任务编号）。
func (c *cat) javaSetOp() {
	type javaArgs struct {
		Inst  string `json:"inst"`
		Major int    `json:"major"`
		Path  string `json:"path"`
	}
	c.reg.Register(&ops.Op{
		Name: "instance.java.set", Title: "给实例换 Java", Level: ops.Write,
		Desc: "换成指定大版本的 Java，下次启动生效。本机或 MineDeck 已经有这个版本就直接换；没有的话在后台下载便携版（不改系统环境），" +
			"返回任务编号（进度见 task.get）。path 可以指定用哪一个（必须是 java.list 里列出来的）。",
		Params: withInst(map[string]*ops.Schema{
			"major": ops.Integer("Java 大版本，如 8、17、21").Range(8, 30),
			"path":  ops.String("用 java.list 里的哪一个 java.exe；空串自动选").Len(0, 500),
		}, "major"),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[javaArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			major, path := i.JavaSnapshot()
			after := fmt.Sprintf("Java %d", p.Major)
			if jp, ok := knownJava(p.Major, p.Path); ok {
				after += "（" + jp + "）"
			} else if p.Path != "" {
				return nil, ops.Errorf(400, "%s 不是已知的 Java %d（见 java.list）", p.Path, p.Major)
			} else {
				after += "（本机没有，先在后台下载便携版，约 50 MB）"
			}
			plan := &ops.Plan{Summary: fmt.Sprintf("把「%s」换成 Java %d（下次启动生效）", i.Name, p.Major),
				Changes: []ops.Change{{What: "Java", Before: fmt.Sprintf("Java %d（%s）", major, path), After: after}},
				Pre:     map[string]string{"Java": path}}
			if i.Status() != "stopped" {
				plan.Warning = "服务器正在运行：换了 Java 要重启才生效"
			}
			return plan, nil
		},
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[javaArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			if jp, ok := knownJava(p.Major, p.Path); ok {
				if err := c.Mgr.SetJava(i.Name, p.Major, jp); err != nil {
					return nil, err
				}
				return map[string]any{"java": jp}, nil
			}
			if p.Path != "" {
				return nil, ops.Errorf(400, "%s 不是已知的 Java %d（见 java.list）", p.Path, p.Major)
			}
			t, ctx := c.Mgr.Tasks.New(fmt.Sprintf("为「%s」准备 Java %d", i.Name, p.Major), []string{"准备 Java 运行时", "换上新的 Java"})
			go func() {
				t.StartStep(0)
				jp, err := java.Ensure(ctx, p.Major, t.Logf, t.ProgressFn(fmt.Sprintf("下载 Java %d", p.Major)))
				if err != nil {
					t.Fail(err)
					return
				}
				t.StartStep(1)
				if err := c.Mgr.SetJava(i.Name, p.Major, jp); err != nil {
					t.Fail(err)
					return
				}
				t.Logf("已换成 Java %d：%s（下次启动生效）", p.Major, jp)
				t.Finish(i.Name)
			}()
			return map[string]string{"taskId": t.ID()}, nil
		},
	})
}
