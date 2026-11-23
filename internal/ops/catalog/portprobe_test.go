package catalog

import "minedeck/internal/inst"

// 测试里试绑端口只用 127.0.0.1，免得 Windows 防火墙每次都弹窗（见 inst.PortProbeHost）；
// 默认把 inst 的保留段与监听表换成空替身，避免用例真起 netsh、跟着本机保留端口走。
func init() {
	inst.PortProbeHost = "127.0.0.1"
	inst.SetPortProbesForTest(nil, nil)
}
