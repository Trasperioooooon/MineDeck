package inst

// 测试里试绑端口只用 127.0.0.1，免得 Windows 防火墙每次都弹窗（见 PortProbeHost）；
// 默认用空替身隔开运行测试这台电脑上的真实监听表与 netsh 保留段（测解析缓存的用例自己注入 runNetshExcluded）。
func init() {
	PortProbeHost = "127.0.0.1"
	SetPortProbesForTest(nil, nil)
	runNetshExcluded = func(bool) (string, error) { return "", nil }
}
