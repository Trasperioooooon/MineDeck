package web

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"

	"minedeck/internal/dl/dltest"
)

// 这个包的测试不连外网：版本清单、Java 这些下载一律马上失败，走「没有网」那条路（见 dltest.NoInternet）。
// 设了 MINEDECK_FAKE_SERVER=1 时，测试程序自己扮演服务器子进程。
func TestMain(m *testing.M) {
	if os.Getenv("MINEDECK_FAKE_SERVER") == "1" {
		fmt.Println(`[12:00:00] [Server thread/INFO]: Done (1.0s)! For help, type "help"`)
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == "stop" {
				fmt.Println("[12:00:01] [Server thread/INFO]: Stopping the server")
				os.Exit(0)
			}
		}
		os.Exit(0)
	}
	dltest.NoInternet()
	os.Exit(m.Run())
}
