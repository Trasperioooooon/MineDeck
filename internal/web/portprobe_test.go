package web

import "minedeck/internal/inst"

// 测试里试绑端口只用 127.0.0.1，免得 Windows 防火墙每次都弹窗（见 inst.PortProbeHost）。
func init() { inst.PortProbeHost = "127.0.0.1" }
