package inst

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"minedeck/internal/tasks"
)

// 换地图（internal/worldimport）用的维护占位和启动封锁。
//
// 占位（hold）：正在把旧世界移出、新世界移入的那几步。占着的时候这台服务器不能启动（界面、接口、定时重启、
// 崩溃自动重启、AI 和 MCP 都走 Start），不能删除、还原备份、定时备份、打包交接包，也不能改 server.properties。
// 占位只在服务器已停止时才占得上，检查和 Start 用的是同一把 procMu，所以不会出现「看着是停的、转眼被拉起来」。
//
// 封锁（block）：磁盘上有一份没走完的换地图记录（上次换到一半程序退出、断电），或者自动退回失败、要人处理。
// 封锁在加载实例时就按记录设上，比调度器和任何启动入口都早；记录处理完由 worldimport 解除。

// WorldTxDir 换地图的记录、暂存区和恢复点在实例目录里的位置。它在 .minedeck/ 下面：
// .minedeck/ 是控制目录（见 control.go），别人给的交接包、整合包带了也不收，交接包导出也不带。
const WorldTxDir = ".minedeck/world-import"

// worldTxDone 不用封锁启动的状态（和 worldimport 的 Tx* 常量一致）：已提交、已退回、已换回，以及还没碰目标世界的暂存。
var worldTxDone = map[string]bool{"committed": true, "rolled-back": true, "restored": true, "staging": true, "staged": true}

// worldTxPending 没走完、要封锁启动的状态。不在这两张表里的状态（包括空的）按记录坏了算。
var worldTxPending = map[string]bool{"committing": true, "rolling-back": true, "restoring": true}

// WorldTxUnreadable 有换地图的记录读不出来时封锁启动用的说明。
const WorldTxUnreadable = "换地图的记录读不出来，为了不在半成品的世界上启动，已暂停启动。请到「地图」里查看"

// PendingWorldTx 实例目录里有没有没走完（或读不出来）的换地图记录；有的话返回给人看的原因。
// staging / staged 不算：那时候还没碰目标世界，丢下不管也不影响服务器（暂存区由 worldimport 清理）。
// 加载实例时据此封锁启动；worldimport 处理完记录以后也用它再看一遍盘上，没问题了才解除封锁。
func PendingWorldTx(dir string) string {
	ents, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(WorldTxDir)))
	if errors.Is(err, os.ErrNotExist) {
		return "" // 没有这个文件夹：从来没换过地图
	}
	if err != nil {
		// 文件夹在、却读不了（被占用、没权限、盘出错）：里面有没有没走完的记录不知道，按有算。
		// 以前这里当成没有，换图中断以后要是正好读不了，服务器就能在半成品的世界上启动（2026-11-14 Codex 合并复审第 3 条）
		return WorldTxUnreadable
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(WorldTxDir), e.Name(), "journal.json"))
		if errors.Is(err, os.ErrNotExist) {
			continue // 没有记录：还没开始（记录是建暂存区之前头一个写下的）
		}
		if err != nil { // 有记录却读不了（被占用、没权限）：不知道走到哪一步了，按没走完算
			return WorldTxUnreadable
		}
		var j struct {
			ID        string `json:"id"`
			State     string `json:"state"`
			LevelName string `json:"levelName"`
		}
		// 读得出来但内容对不上的（编号和文件夹不一致、世界名不安全、状态不认识）也是坏的：
		// 里面的状态不可信，不能因为它写着「已提交」就放行。步骤里的路径由 worldimport 核对（见 Journal.validate）
		if json.Unmarshal(b, &j) != nil || j.ID != e.Name() || !SafeLevelName(j.LevelName) || !worldTxDone[j.State] && !worldTxPending[j.State] && j.State != "stuck" {
			return WorldTxUnreadable
		}
		if j.State == "stuck" {
			return "上次换地图没能自动恢复，服务器已暂停启动。请到「地图」里查看恢复步骤"
		}
		if worldTxPending[j.State] {
			return "上次换地图没有完成，正在恢复到换图前的世界，完成前不能启动"
		}
	}
	return ""
}

// BeginWorldChange 占上「正在换地图」的位，返回放掉它的函数。服务器必须已经停下，没在打包，没有别的换图在进行，
// 也没有被封锁（有没走完的换地图记录）。
//
// 封锁要在这里、和占位同一次加锁里查：以前只看占位，封锁由各个入口自己先查一遍，有的入口（换回旧地图）没查。
// 结果是第二次导入卡在半路时还能把第一次导入换回去，之后重试恢复会把换回来的那张最早的地图当成第二次的新地图清掉
// （2026-11-14 Codex 合并复审第 1 条，用测试在真目录上复现过）。处理没走完的记录本身用 BeginWorldRecovery。
func (m *Manager) BeginWorldChange(name, why string) (release func(), err error) {
	return m.beginWorldChange(name, why, false)
}

// BeginWorldRecovery 同 BeginWorldChange，但被封锁时也占得上：只给「处理没走完的换地图记录」用（重试恢复）。
// 停没停、打包、已经有人占着，这几样照查。
func (m *Manager) BeginWorldRecovery(name, why string) (release func(), err error) {
	return m.beginWorldChange(name, why, true)
}

func (m *Manager) beginWorldChange(name, why string, recovery bool) (release func(), err error) {
	i, err := m.Get(name)
	if err != nil {
		return nil, err
	}
	i.procMu.Lock()
	defer i.procMu.Unlock()
	if i.state != "" && i.state != "stopped" {
		return nil, errors.New("服务器还没停下，没有替换任何文件")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.packing[name] {
		return nil, errors.New("这台服务器正在打包交接包，等打包完成后再换地图")
	}
	if r := m.worldHold[name]; r != "" {
		return nil, errors.New("这台服务器正在换地图或还原备份，请等它完成")
	}
	if r := m.worldBlock[name]; r != "" && !recovery {
		return nil, errors.New(r)
	}
	if who := m.worldUsersLocked(name); who != "" {
		return nil, errors.New("这台服务器正在" + who + "，等它完成后再试")
	}
	if m.worldHold == nil {
		m.worldHold = map[string]string{}
	}
	m.worldHold[name] = why
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			delete(m.worldHold, name)
			m.mu.Unlock()
		})
	}, nil
}

// UseWorld 登记「正在用这台服务器的世界和设置」，返回撤销登记的函数。备份、保存设置这类要读写世界目录或
// server.properties、但不搬动整个世界的操作，开始之前登记、做完撤销。
//
// 它和换地图的占位互相排斥，检查和登记在同一次加锁里：有人占着位（或者有没走完的换地图记录）时登记不上；
// 登记着的时候占位占不上（见 beginWorldChange）。以前这些操作只在开头看一眼 WorldBusy、自己不登记：
// 备份压到一半时换地图照样能占上位、把世界目录移走，打出来的备份前一半是旧图、后一半是新图
// （2026-11-14 Codex 合并复审第 2 条）。
//
// 登记不进 WorldBusy：启动查的是 WorldBusy，热备份的时候服务器本来就开着，崩了要能自动重启。
// 几个操作可以同时登记（备份的时候改设置），互相之间不排斥，和以前一样。
func (m *Manager) UseWorld(name, what string) (release func(), err error) {
	if _, err := m.Get(name); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.worldHold[name]; r != "" {
		return nil, errors.New("这台服务器正在" + r + "，请等它完成")
	}
	if r := m.worldBlock[name]; r != "" {
		return nil, errors.New(r)
	}
	if m.worldUse == nil {
		m.worldUse = map[string]map[string]int{}
	}
	if m.worldUse[name] == nil {
		m.worldUse[name] = map[string]int{}
	}
	m.worldUse[name][what]++
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if u := m.worldUse[name]; u != nil {
				if u[what]--; u[what] <= 0 {
					delete(u, what)
				}
				if len(u) == 0 {
					delete(m.worldUse, name)
				}
			}
		})
	}, nil
}

// WorldUsers 现在有哪些操作登记着在用这台服务器的世界和设置（「备份」「保存设置」，顿号隔开），没有返回空串。
func (m *Manager) WorldUsers(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.worldUsersLocked(name)
}

func (m *Manager) worldUsersLocked(name string) string {
	u := m.worldUse[name]
	if len(u) == 0 {
		return ""
	}
	names := make([]string, 0, len(u))
	for what := range u {
		names = append(names, what)
	}
	sort.Strings(names)
	return strings.Join(names, "、")
}

// WorldBusy 这台服务器现在能不能动世界：正在换地图（占位）或有没走完的记录（封锁）时返回原因，否则返回空串。
func (m *Manager) WorldBusy(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.worldHold[name]; r != "" {
		return "这台服务器正在" + r + "，请等它完成"
	}
	return m.worldBlock[name]
}

// SetWorldBlock 封锁这台服务器的启动（reason 是给人看的原因）；reason 为空时解除。
func (m *Manager) SetWorldBlock(name, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if reason == "" {
		delete(m.worldBlock, name)
		return
	}
	if m.worldBlock == nil {
		m.worldBlock = map[string]string{}
	}
	m.worldBlock[name] = reason
}

// WorldBlocked 有封锁的实例名 → 原因（启动时 worldimport 据此处理没走完的记录）。
func (m *Manager) WorldBlocked() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.worldBlock))
	for k, v := range m.worldBlock {
		out[k] = v
	}
	return out
}

// WorldPlacer 新建服务器时把地图放进目录的回调（worldimport 挂上来）：owner 是发起的人，planID 是导入计划。
type WorldPlacer func(ctx context.Context, t *tasks.Task, owner, planID, dir string) error

// SetWorldPlacer 挂上新建时放地图的回调。release 在新建任务结束时调（不管成没成），
// 放掉建服请求占着的导入计划和上传的文件。
func (m *Manager) SetWorldPlacer(fn WorldPlacer, release func(owner, planID string)) {
	m.mu.Lock()
	m.worldPlacer = fn
	m.worldRelease = release
	m.mu.Unlock()
}

func (m *Manager) releaseWorldPlan(owner, planID string) {
	m.mu.Lock()
	fn := m.worldRelease
	m.mu.Unlock()
	if fn != nil && planID != "" {
		fn(owner, planID)
	}
}

// ErrStopTimeout 停服超时：没有强杀，服务器还在自己关。
var ErrStopTimeout = errors.New("服务器在规定时间内没有停下")

// StopAndWait 换地图用的停服：发 stop，等 Java 进程自己退出；超时就返回 ErrStopTimeout，不强杀
// （普通的「停止」按钮照旧在 30 秒后强杀，见 Stop）。服务器本来就停着时直接返回 nil。
func (m *Manager) StopAndWait(name string, timeout time.Duration) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	i.procMu.Lock()
	p := i.proc
	if p == nil || i.state == "stopped" || i.state == "" {
		i.procMu.Unlock()
		return nil
	}
	prevState, prevUser := i.state, i.userStop
	i.state = "stopping"
	i.userStop = true
	i.procMu.Unlock()
	i.Console.Append("[MineDeck] 换地图：正在保存世界并停止服务器（不会强制结束）...")
	if err := writeLine(p, "stop"); err != nil {
		// 命令没发出去，服务器还在跑：改回原来的状态，不然它之后崩了会被当成有人要停，崩溃自动重启也不会触发
		i.procMu.Lock()
		if i.proc == p && i.state == "stopping" {
			i.state, i.userStop = prevState, prevUser
		}
		i.procMu.Unlock()
		return fmt.Errorf("没能给服务器发停止命令：%w", err)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		i.procMu.Lock()
		gone := i.proc != p
		i.procMu.Unlock()
		if gone {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	i.Console.Append("[MineDeck] 换地图：服务器在规定时间内没有停下，已放弃换地图；服务器会继续自己关闭")
	return ErrStopTimeout
}

// LastExit 上一次进程是怎么结束的："clean"（有人要停、正常退出）、"killed"（被强制结束）、"crash"（没人要停却退出了）；
// 这次程序运行以来没起过为 ""。换地图前用它提醒：被强杀的旧世界最后几分钟可能没存上。
func (i *Instance) LastExit() string {
	i.procMu.Lock()
	defer i.procMu.Unlock()
	return i.lastExit
}
