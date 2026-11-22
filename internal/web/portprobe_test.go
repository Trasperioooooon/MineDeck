package web

import "minedeck/internal/inst"

// 测试里试绑端口只用 127.0.0.1，免得 Windows 防火墙每次都弹窗（见 inst.PortProbeHost）；
// 新建、导入表单的端口复查默认不看运行测试这台电脑上碰巧开着的别的程序和系统保留段（专门测真实监听表的用例用 UseRealPortProbesForTest）。
func init() {
	inst.PortProbeHost = "127.0.0.1"
	inst.SetPortProbesForTest(nil, nil)
}
