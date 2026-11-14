package web

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// Markdown 渲染器没有单独的前端构建。这里用本机的 node 跑 md.test.js（Windows 的 CI 镜像带 node）。
// 没有 node 就跳过，不因此失败。
func TestMarkdownRenderer(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("没有 node，跳过 Markdown 渲染器测试")
	}
	cmd := exec.Command(node, "--test", "md.test.js")
	cmd.Dir = filepath.Join("ui")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("md.test.js：%v\n%s", err, out)
	}
}
