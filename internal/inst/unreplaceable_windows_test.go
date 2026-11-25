package inst

import (
	"os"
	"testing"
)

// makeUnreplaceable 让这个文件换不掉，模拟用户把它保护了起来（对应 unreplaceable_darwin_test.go）。
// Windows 上设只读属性：带保护的原子写（atomicfile.WriteProtected）遇到只读的目标就拒绝替换。文件照样读得出来。
// 测试结束时去掉只读，临时目录才删得掉。
func makeUnreplaceable(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
}
