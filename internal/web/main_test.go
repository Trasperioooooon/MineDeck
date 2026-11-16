package web

import (
	"os"
	"testing"

	"minedeck/internal/dl/dltest"
)

// 这个包的测试不连外网：版本清单、Java 这些下载一律马上失败，走「没有网」那条路（见 dltest.NoInternet）。
func TestMain(m *testing.M) {
	dltest.NoInternet()
	os.Exit(m.Run())
}
