package tunnel

import (
	"os"
	"testing"

	"minedeck/internal/dl/dltest"
)

// 这个包的测试不连外网：下 frpc、查平台接口的都用本机的假服务器（dl.Client 只连得上本机）。
func TestMain(m *testing.M) {
	dltest.NoInternet()
	os.Exit(m.Run())
}
