package web

import (
	"os"
	"testing"

	"minedeck/internal/dl/dltest"
	"minedeck/internal/fakeserver"
)

// 这个包的测试不连外网：版本清单、Java 这些下载一律马上失败，走「没有网」那条路（见 dltest.NoInternet）。
// 设了 MINEDECK_FAKE_SERVER=1 时，测试程序自己扮演服务器子进程。
func TestMain(m *testing.M) {
	if os.Getenv(fakeserver.Env) == "1" {
		os.Exit(fakeserver.Run())
	}
	dltest.NoInternet()
	os.Exit(m.Run())
}
