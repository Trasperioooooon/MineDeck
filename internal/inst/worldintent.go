package inst

import "fmt"

// WorldIntent 在停服前挡住新备份和设置保存，停服后原子升级成世界占位。
// 实例身份和令牌身份都在 Manager.mu 中核对，旧请求不能操作后来同名的新实例。
type WorldIntent struct {
	m    *Manager
	i    *Instance
	why  string
	held bool
}

func (m *Manager) BeginWorldIntent(name, why string) (*WorldIntent, error) {
	i, err := m.Get(name)
	if err != nil {
		return nil, err
	}
	return m.BeginWorldIntentInstance(i, why)
}

func (m *Manager) BeginWorldIntentInstance(i *Instance, why string) (*WorldIntent, error) {
	if i == nil {
		return nil, fmt.Errorf("服务器不存在")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	name := i.Name
	if m.insts[name] != i {
		return nil, fmt.Errorf("服务器已经删除或重新创建，请刷新后重试")
	}
	if m.worldIntent[name] != nil || m.worldHold[name] != "" || m.packing[name] {
		return nil, fmt.Errorf("这台服务器正在换地图或打包，请等它完成")
	}
	if why := m.worldBlock[name]; why != "" {
		return nil, fmt.Errorf("%s", why)
	}
	if who := m.worldUsersLocked(name); who != "" {
		return nil, fmt.Errorf("这台服务器正在%s，等它完成后再试", who)
	}
	if m.worldIntent == nil {
		m.worldIntent = map[string]*WorldIntent{}
	}
	w := &WorldIntent{m: m, i: i, why: why}
	m.worldIntent[name] = w
	return w, nil
}

func (w *WorldIntent) Instance() *Instance { return w.i }

func (w *WorldIntent) Change() error {
	w.i.procMu.Lock()
	defer w.i.procMu.Unlock()
	w.m.mu.Lock()
	defer w.m.mu.Unlock()
	name := w.i.Name
	if w.m.insts[name] != w.i || w.m.worldIntent[name] != w {
		return fmt.Errorf("换地图的准备已结束，请重新导入")
	}
	if w.i.state != "" && w.i.state != "stopped" {
		return fmt.Errorf("服务器还没停下，没有替换任何文件")
	}
	if w.held {
		return nil
	}
	if why := w.m.worldBlock[name]; why != "" {
		return fmt.Errorf("%s", why)
	}
	if w.m.worldHold == nil {
		w.m.worldHold = map[string]string{}
	}
	w.m.worldHold[name] = w.why
	w.held = true
	return nil
}

func (w *WorldIntent) Release() {
	w.m.mu.Lock()
	defer w.m.mu.Unlock()
	name := w.i.Name
	if w.m.worldIntent[name] != w {
		return
	}
	delete(w.m.worldIntent, name)
	if w.held {
		delete(w.m.worldHold, name)
	}
}
