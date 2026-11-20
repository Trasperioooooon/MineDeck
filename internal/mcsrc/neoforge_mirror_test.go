package mcsrc

import "testing"

func TestParseSHA1Sidecar(t *testing.T) {
	if got, ok := parseSHA1Sidecar([]byte("0a096663e7e11717b695f8fdab463f37327bfa72\n")); !ok || got != "0a096663e7e11717b695f8fdab463f37327bfa72" {
		t.Fatalf("官方 .sha1：%q %v", got, ok)
	}
	if got, ok := parseSHA1Sidecar([]byte("0A096663E7E11717B695F8FDAB463F37327BFA72  neoforge.jar\n")); !ok || got != "0a096663e7e11717b695f8fdab463f37327bfa72" {
		t.Fatalf("带文件名：%q %v", got, ok)
	}
	for _, bad := range []string{"", "<html>0a096663e7e11717b695f8fdab463f37327bfa72</html>", "abc", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"} {
		if _, ok := parseSHA1Sidecar([]byte(bad)); ok {
			t.Fatalf("不该认：%q", bad)
		}
	}
}
