package web

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Markdown 渲染器没有单独的前端构建。这里用本机的 node 跑 md.test.js（Windows 的 CI 镜像带 node）。
// 没有 node 就跳过，不因此失败。
//
// 外面套一个宽松的超时：渲染器是同步的，真在某种输入上停不下来时，md.test.js 里的计时断言根本走不到，
// 以前只能等整个 go test 超时（2026-11-14 Codex 合并复审第 9 条）。直接跑这个文件、不用 node --test：
// 那样测试在另起的子进程里跑，超时只杀得掉外面那个，卡住的子进程会留下来。
func TestMarkdownRenderer(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("没有 node，跳过 Markdown 渲染器测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), mdTestTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "md.test.js")
	cmd.Dir = filepath.Join("ui")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("md.test.js 跑了 %v 还没结束，已经停掉：渲染器多半在某种输入上卡住了\n%s", mdTestTimeout, out)
	}
	if err != nil {
		t.Fatalf("md.test.js：%v\n%s", err, out)
	}
}

// mdTestTimeout 正常跑完不到一秒，CI 上慢的时候十几秒。
const mdTestTimeout = 3 * time.Minute
