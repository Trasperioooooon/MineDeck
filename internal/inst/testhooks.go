package inst

// 本文件仅存放供跨包测试（如 internal/web、internal/ops/catalog）替换本包系统探测函数的导出钩子。
// 之所以需要跨包导出，是因为测试运行时与业务隔离在不同 Go 包中，不能通过包内变量直接替换；
// 这些函数均以 ForTest 结尾，正式运行逻辑不应调用它们。

// SetPortProbesForTest 供跨包测试注入固定的监听端口表与系统保留段，不受运行测试那台电脑上真实进程或 Hyper-V 保留段影响。
func SetPortProbesForTest(listeners map[int]string, excluded [][2]int) func() {
	oldL, oldN, oldE, oldM := queryListenPorts, queryProcessName, queryExcludedPorts, listenProbesMocked
	fakeL := map[int]int{}
	fakeNames := map[int]string{}
	nextPID := 1000
	for p, name := range listeners {
		nextPID++
		fakeL[p] = nextPID
		fakeNames[nextPID] = name
	}
	var rs []portRange
	for _, r := range excluded {
		rs = append(rs, portRange{start: r[0], end: r[1]})
	}
	queryListenPorts = func() map[int]int { return fakeL }
	queryProcessName = func(pid int) string { return fakeNames[pid] }
	queryExcludedPorts = func() ([]portRange, []portRange) { return rs, nil }
	listenProbesMocked = true
	return func() {
		queryListenPorts, queryProcessName, queryExcludedPorts, listenProbesMocked = oldL, oldN, oldE, oldM
	}
}

// UseRealPortProbesForTest 供跨包测试临时切回系统真实的监听表探测（仍屏蔽真实 netsh，避免测试受本机保留段影响）。
func UseRealPortProbesForTest() func() {
	oldL, oldN, oldE, oldM := queryListenPorts, queryProcessName, queryExcludedPorts, listenProbesMocked
	queryListenPorts = defaultListenPorts
	queryProcessName = defaultProcessName
	queryExcludedPorts = func() ([]portRange, []portRange) { return nil, nil }
	listenProbesMocked = false
	return func() {
		queryListenPorts, queryProcessName, queryExcludedPorts, listenProbesMocked = oldL, oldN, oldE, oldM
	}
}
