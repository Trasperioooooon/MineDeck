package inst

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// pendingWorldTx 实例目录里有没有没走完的换地图记录；有的话返回给人看的原因。
// staging / staged 不算：那时候还没碰目标世界，丢下不管也不影响服务器（暂存区由 worldimport 清理）。
func pendingWorldTx(dir string) string {
	ents, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(WorldTxDir)))
	if err != nil {
		return ""
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(WorldTxDir), e.Name(), "journal.json"))
		if err != nil {
			continue
		}
		var j struct {
			State string `json:"state"`
		}
		if json.Unmarshal(b, &j) != nil {
			return "换地图的记录读不出来，为了不在半成品的世界上启动，已暂停启动。请到「地图 / 世界」里查看"
		}
		if j.State == "stuck" {
			return "上次换地图没能自动恢复，服务器已暂停启动。请到「地图 / 世界」里查看恢复步骤"
		}
		if !worldTxDone[j.State] {
			return "上次换地图没有完成，正在恢复到换图前的世界，完成前不能启动"
		}
	}
	return ""
}

// BeginWorldChange 占上「正在换地图」的位，返回放掉它的函数。服务器必须已经停下，并且没在打包、没有别的换图在进行。
func (m *Manager) BeginWorldChange(name, why string) (release func(), err error) {
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

// SetWorldPlacer 挂上新建时放地图的回调。
func (m *Manager) SetWorldPlacer(fn WorldPlacer) {
	m.mu.Lock()
	m.worldPlacer = fn
	m.mu.Unlock()
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
	i.state = "stopping"
	i.userStop = true
	i.procMu.Unlock()
	i.Console.Append("[MineDeck] 换地图：正在保存世界并停止服务器（不会强制结束）...")
	if err := writeLine(p, "stop"); err != nil {
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
