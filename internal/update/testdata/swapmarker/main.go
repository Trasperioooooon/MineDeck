// swapmarker 是换 exe 测试用的小程序（编成窗口程序，不会弹控制台）。
// 带 -sleep 秒数时扮旧版：一直开着，占住自己的 exe；不带参数时扮新版：
// 把当前目录、参数个数、漏进来的 MD_UPDATE_* 环境变量写进 exe 旁边的 started.txt，然后退出。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "-sleep" {
		n, _ := strconv.Atoi(os.Args[2])
		time.Sleep(time.Duration(n) * time.Second)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	wd, _ := os.Getwd()
	var leaked []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(strings.ToUpper(kv), "MD_UPDATE_") {
			leaked = append(leaked, kv)
		}
	}
	out := fmt.Sprintf("wd=%s\nargs=%d\nleaked=%s\n", wd, len(os.Args)-1, strings.Join(leaked, ";"))
	_ = os.WriteFile(filepath.Join(filepath.Dir(exe), "started.txt"), []byte(out), 0o644)
}
