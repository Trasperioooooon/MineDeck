package inst

import (
	"context"
	"path/filepath"
	"sync"

	"minedeck/internal/app"
	"minedeck/internal/mcsrc"
	"minedeck/internal/tasks"
)

// 交接包（internal/transfer，分支 feat-instance-transfer）用的流水线钩子。
// 具体的导出 / 导入编排在 transfer 包里，这里只把创建流水线里可复用的部件按最小面公开：
// 占位校验、Java 与核心的准备、启动方式探测、收尾注册。公开的名字一旦有人用就算契约，
// 改动前先看还有谁在调（branch feat-instance-distribute 的「下发」场景也按同一套用）。

// ReserveCreate 校验创建请求（名称 / 版本 / 目录去重）并登记「创建中」占位，返回目标目录。
// 调用方负责在任务结束时（无论成败）ReleaseCreate。
func (m *Manager) ReserveCreate(req *CreateReq) (string, error) { return m.validateCreate(req) }

// ReleaseCreate 释放 ReserveCreate 登记的占位（实例名 + 目录两把）。
func (m *Manager) ReleaseCreate(name, dir string) {
	m.releaseCreating(name)
	m.releaseCreating(dirKey(dir))
}

// BeginPacking 给实例占上「正在打包」的位，返回放掉它的函数；已经在打包时 ok=false。
// 占着的时候这台服务器不能启动、不能删除（见 Start 和 Delete）。调用方在打包结束时（无论成败）调一次 release。
func (m *Manager) BeginPacking(name string) (release func(), ok bool) {
	if m.WorldBusy(name) != "" { // 换地图到一半，世界目录正在移动，打出来的包是坏的
		return nil, false
	}
	if !m.tryMarkPacking(name) {
		return nil, false
	}
	var once sync.Once // release 可以调多次（提前放一次、defer 再兜一次），只有第一次算数：不能把之后别人占上的位放掉
	return func() { once.Do(func() { m.unmarkPacking(name) }) }, true
}

// FinalizeCreate 写 run.bat 和 instance.json，记下扫描根，把实例注册进列表。
// s 里的 Name 必须已经过校验（和 dir 对应），JavaPath 应已在本机确认存在。
func (m *Manager) FinalizeCreate(s Settings, dir string) (*Instance, error) {
	i := &Instance{Settings: s, Dir: dir, Console: newConsole()}
	if err := writeRunBat(i); err != nil {
		return nil, err
	}
	if err := m.saveInstance(i); err != nil {
		return nil, err
	}
	app.RememberRoot(filepath.Dir(dir))
	m.mu.Lock()
	m.insts[s.Name] = i
	m.mu.Unlock()
	return i, nil
}

// Log4jNoLookups log4shell 的缓解参数（老版本用；新版本带着也没有影响）。
const Log4jNoLookups = log4jNoLookups

// IsLog4jFlag 这条 JVM 参数是不是 MineDeck 自己为 log4shell 防护加的（不是服主手写的）。
func IsLog4jFlag(arg string) bool { return arg == log4jNoLookups || arg == log4jConfig }

// ApplyLog4jMitigation 按这台电脑的情况给老版本套 log4shell 防护（同新建、接管）：往 s.ExtraJVM 里补上该有的参数，
// 1.12.2 ~ 1.16.5 还会把官方的日志配置下载到 dir。s.ExtraJVM 里已有的同名参数以这里的为准。
func ApplyLog4jMitigation(ctx context.Context, t *tasks.Task, s *Settings, dir string) error {
	i := &Instance{Settings: *s, Dir: dir}
	own := len(i.ExtraJVM)
	err := applyLog4jMitigation(ctx, t, i)
	s.ExtraJVM = withJVMFlags(i.ExtraJVM[:own:own], i.ExtraJVM[own:])
	return err
}

// FetchArtifact 下载核心工件到共享缓存，再复制进实例目录（同创建流水线：换源重试记进任务的网络提示）。
func FetchArtifact(ctx context.Context, t *tasks.Task, art mcsrc.Artifact, cacheKey, dir, destName string) error {
	return fetchToInstance(ctx, t, art, cacheKey, dir, destName)
}

// RunInstaller 以匹配的 Java 运行 Forge / NeoForge 官方安装器（--installServer，含 3 次重试）。
func RunInstaller(ctx context.Context, t *tasks.Task, javaExe, dir, installerJar string) error {
	return runInstaller(ctx, t, javaExe, dir, installerJar)
}

// LocateLaunchTarget 在安装完成的目录里探测启动方式（"args:…" 或 "jar:…"）。
func LocateLaunchTarget(dir string, core mcsrc.Core, ctx context.Context, mc string) (string, error) {
	return locateLaunchTarget(dir, core, ctx, mc)
}

// WorldDirs 返回实例的世界存档目录（vanilla 单目录 / bukkit 三目录，读 server.properties 的 level-name）。
func (i *Instance) WorldDirs() []string { return i.worldDirs() }

// LaunchArgsOf 给出 JVM 启动参数（进程托管与 run.bat 共用的那份），供导入做完后做“到启动参数这一层”的校验。
func LaunchArgsOf(i *Instance) []string { return launchArgs(i) }
