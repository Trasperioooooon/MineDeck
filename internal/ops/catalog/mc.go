package catalog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"minedeck/internal/inst"
	"minedeck/internal/java"
	"minedeck/internal/mcsrc"
	"minedeck/internal/ops"
	"minedeck/internal/tasks"
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
			incomplete := java.IncompletePortable()
			return map[string]any{"portable": portable, "scanned": scanned, "incomplete": incomplete}, nil
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

	// 只删 MineDeck 装在 runtimes/java-N 的便携运行时。系统里扫描到的 Java 不在这里，也没有路径参数可传。
	c.reg.Register(&ops.Op{
		Name: "java.remove", Title: "卸载便携 Java", Level: ops.HumanOnly, LocalOnly: true,
		LocalMsg: "卸载便携 Java 仅支持在本机操作",
		Desc: "删除 MineDeck 自己下载的某一版便携 Java（runtimes/java-N）。不删除系统里安装的 Java。" +
			"有服务器还配着它、正在用它启动、或者正在换成它时拒绝。confirm 必须为 true。删到一半会留下「没装完」记号，不会把半截当成已装好。",
		Params: ops.Object(map[string]*ops.Schema{
			"major":   ops.Integer("要卸载的大版本，例如 17、21").Range(8, 64),
			"confirm": ops.Boolean("用户已确认删除这一套便携 Java"),
		}, "major", "confirm"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Major   int  `json:"major"`
				Confirm bool `json:"confirm"`
			}](a)
			if err != nil {
				return nil, err
			}
			if !p.Confirm {
				return nil, ops.Errorf(400, "卸载 Java 需要明确确认")
			}
			err = java.RemovePortable(p.Major, func(dir string) error {
				// 先看有没有正在建的，再拿服务器列表：新建、导入、接管是先登记进列表、再放掉「正在创建」。先拿列表的话，
				// 任务正好在两步之间做完，拿列表时它还没登记，再看时「正在创建」已经放掉，两样都漏
				creating := c.Mgr.HasCreating()
				var configured, running []string
				for _, i := range c.Mgr.List() {
					_, cfgPath := i.JavaSnapshot()
					runPath := i.RunningJavaPath()
					cfgMatch := javaPathInside(cfgPath, dir)
					runMatch := javaPathInside(runPath, dir)
					if cfgMatch {
						configured = append(configured, i.Name)
					}
					if runMatch || (cfgMatch && slices.Contains([]string{"starting", "running", "stopping"}, i.Status())) {
						running = append(running, i.Name)
					}
				}
				if len(running) > 0 {
					return ops.Errorf(409, "服务器正在用 Java %d 运行（%s），请先停服再卸载", p.Major, strings.Join(running, "、"))
				}
				if len(configured) > 0 {
					return ops.Errorf(409, "服务器还在用 Java %d（%s），请先到「设置 → 启动与内存」更换后再卸载", p.Major, strings.Join(configured, "、"))
				}
				if creating {
					return ops.Errorf(409, "有服务器正在创建或导入，可能正在使用便携 Java，请等任务结束再卸载")
				}
				return nil
			})
			if err != nil {
				if errors.Is(err, java.ErrBusy) {
					return nil, ops.Errorf(409, "%s", err.Error())
				}
				return nil, err
			}
			return map[string]any{"major": p.Major, "removed": true}, nil
		},
	})

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

// javaPathInside path 是不是 dir 里面的文件（便携 Java 的 java.exe）。不把目录本身或外面的路径算进去。
// 优先按真实路径比对，兼容 8.3 短名和目录联接；路径现在打不开时退回规范化后的词法相对路径。
func javaPathInside(path, dir string) bool {
	path, dir = strings.TrimSpace(path), strings.TrimSpace(dir)
	if path == "" || dir == "" {
		return false
	}
	if rp, err1 := inst.RealPathStrict(path); err1 == nil {
		if rd, err2 := inst.RealPathStrict(dir); err2 == nil {
			return rp != rd && inst.InsideReal(rd, rp)
		}
	}
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

// setJava 给服务器换上 jp。jp 是 MineDeck 的便携 Java 时和卸载互斥（A34）：这套 Java 正在卸载时换不上（409），
// 换的途中卸载报忙。本机装的 Java 不经 MineDeck 卸载，直接换。
func (c *cat) setJava(name string, major int, jp string) error {
	set := func() error { return c.Mgr.SetJava(name, major, jp) }
	if !javaPathInside(jp, java.Dir(major)) {
		return set()
	}
	err := java.UsePortable(major, set)
	if errors.Is(err, java.ErrRemoving) {
		return ops.Errorf(409, "%s", err.Error())
	}
	return err
}

// javaSetOp 给实例换 Java（§5.2 java_set）：已有这个版本就直接换，没有就在后台下载便携版再换（返回任务编号）。
func (c *cat) javaSetOp() {
	type javaArgs struct {
		Inst  string `json:"inst"`
		Major int    `json:"major"`
		Path  string `json:"path"`
	}
	downloads := &javaDownloads{}
	c.reg.Register(&ops.Op{
		Name: "instance.java.set", Title: "给服务器换 Java", Level: ops.Write,
		Desc: "换成指定大版本的 Java，下次启动生效。本机或 MineDeck 已经有这个版本就直接换；没有的话在后台下载便携版（不改系统环境），" +
			"返回任务编号（进度见 task.get），这台服务器同一个版本的下载还在跑时返回那个任务的编号。" +
			"path 可以指定用哪一个（必须是 java.list 里列出来的）。那套便携 Java 正在卸载时拒绝，换成已卸载或没装完整的那套也会被拒。",
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
				if err := c.setJava(i.Name, p.Major, jp); err != nil {
					return nil, err
				}
				return map[string]any{"java": jp}, nil
			}
			if p.Path != "" {
				return nil, ops.Errorf(400, "%s 不是已知的 Java %d（见 java.list）", p.Path, p.Major)
			}
			return map[string]string{"taskId": downloads.start(c, i.Name, p.Major)}, nil
		},
	})
}

// ensureJava 下载、部署便携 Java；测试里换成不联网、能停住的函数。
var ensureJava = java.Ensure

// javaDownloads 正在跑的「下载便携 Java、下好了换上」任务，按服务器和大版本记（A36）。下载还没完时重新进页再保存一次，
// 返回已有任务的编号，不再建一个：安装锁本来就不会重复下载文件，但任务列表里会多一个。别的服务器要同一个版本照样建自己的任务：
// 下好了要换上的是它自己。
type javaDownloads struct {
	mu      sync.Mutex
	running map[javaDownloadKey]string // 值是任务编号
}

type javaDownloadKey struct {
	inst  string
	major int
}

// start 给服务器 name 起一个下载 Java major 再换上的任务，返回任务编号；同一台、同一个版本的还在跑就返回那一个。
func (d *javaDownloads) start(c *cat, name string, major int) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := javaDownloadKey{name, major}
	if id, ok := d.running[key]; ok {
		return id
	}
	t, ctx := c.Mgr.Tasks.New(fmt.Sprintf("为「%s」准备 Java %d", name, major), []string{"准备 Java 运行时", "换上新的 Java"})
	t.SetKind("java")
	if d.running == nil {
		d.running = map[javaDownloadKey]string{}
	}
	d.running[key] = t.ID()
	go func() {
		err := c.downloadAndSetJava(ctx, t, name, major)
		d.mu.Lock()
		delete(d.running, key) // 先去掉再报结果：任务一显示结束，再保存就是新的一次
		d.mu.Unlock()
		if err != nil {
			t.Fail(err)
			return
		}
		t.Finish(name)
	}()
	return t.ID()
}

// downloadAndSetJava 「下载便携 Java、下好了换上」任务的两步。
func (c *cat) downloadAndSetJava(ctx context.Context, t *tasks.Task, name string, major int) error {
	t.StartStep(0)
	jp, err := ensureJava(ctx, major, t.Logf, t.ProgressFn(fmt.Sprintf("下载 Java %d", major)))
	if err != nil {
		return err
	}
	t.StartStep(1)
	if err := c.setJava(name, major, jp); err != nil {
		return err
	}
	t.Logf("已换成 Java %d：%s（下次启动生效）", major, jp)
	return nil
}
