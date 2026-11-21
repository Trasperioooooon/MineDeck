package java

// Mac 版平时不自动下载 Java（见 autoInstallErr）；下载、解压、「还没装完」记号那几步和平台无关，测试里照样测。
func init() { autoInstallOK = true }
