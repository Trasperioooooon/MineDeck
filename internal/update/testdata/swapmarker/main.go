// swapmarker 是换 exe 测试用的小程序（编成窗口程序，不会弹控制台）。
// 带 -sleep 秒数时扮旧版：一直开着，占住自己的 exe；不带参数时扮被打开的程序：
// 把当前目录、参数个数、漏进来的 MD_UPDATE_* 环境变量写进 exe 旁边的 started.txt，然后退出。
// 先写临时文件再改名，测试那边看到 started.txt 时内容一定是完整的。
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
	dir := filepath.Dir(exe)
	tmp := filepath.Join(dir, "started.tmp")
	if os.WriteFile(tmp, []byte(out), 0o644) == nil {
		_ = os.Rename(tmp, filepath.Join(dir, "started.txt"))
	}
}
