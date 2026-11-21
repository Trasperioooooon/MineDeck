//go:build !windows

package tunnel

// Mac 版平时不下载 frpc（见 errFrpcNotWindows）；下载、解压、校验那几步和平台无关，测试里照样测。
func init() { frpcDownloadable = true }
