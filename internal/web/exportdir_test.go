package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 导出交接包的「新建并导出」（可用性走查 R20）：请求体带 mkdir 时先建导出文件夹，只建最后一级；
// 只许本机（docs/API.md「主机路径仅本机可指定」），局域网会话 403、什么都不建；
// 新建了、导出却没成时把这个空文件夹删掉。mkdir 不是操作层的参数，不会让 instance.transfer.export 报「不认识的参数」。
func TestTransferExportMkdir(t *testing.T) {
	e := newFullEnv(t)
	ip := "/api/instances/%E7%94%9F%E5%AD%98%E6%9C%8D" // 生存服
	base := t.TempDir()
	body := func(dest string, mkdir bool, extra map[string]any) string {
		m := map[string]any{"dest": dest}
		if mkdir {
			m["mkdir"] = true
		}
		for k, v := range extra {
			m[k] = v
		}
		b, _ := json.Marshal(m)
		return string(b)
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	// 没带 mkdir：不在的文件夹照旧报错，说清是找不到
	missing := filepath.Join(base, "新的交接包")
	if rec := e.call(t, "POST", ip+"/transfer/export", body(missing, false, nil), 400); !strings.Contains(rec.Body.String(), "找不到这个文件夹") {
		t.Errorf("不在的文件夹：%s", rec.Body)
	}
	// 局域网会话：403，什么都不建
	if rec := e.call(t, "POST", ip+"/transfer/export", body(missing, true, nil), 403, fromLAN); !strings.Contains(rec.Body.String(), "仅支持在本机操作") {
		t.Errorf("局域网会话：%s", rec.Body)
	}
	if exists(missing) {
		t.Fatal("局域网会话不该建出文件夹")
	}
	// 上一级不在：400，一级都不建
	deep := filepath.Join(base, "还没有", "交接包")
	if rec := e.call(t, "POST", ip+"/transfer/export", body(deep, true, nil), 400); !strings.Contains(rec.Body.String(), "上一级文件夹") {
		t.Errorf("上一级不在：%s", rec.Body)
	}
	if exists(filepath.Join(base, "还没有")) {
		t.Error("上一级不在时不该建出文件夹")
	}
	// 建好了、导出却没成（打包方式不认识）：刚建的空文件夹删掉
	if rec := e.call(t, "POST", ip+"/transfer/export", body(missing, true, map[string]any{"mode": "别的什么"}), 400); !strings.Contains(rec.Body.String(), "不认识") {
		t.Errorf("打包方式不认识：%s", rec.Body)
	}
	if exists(missing) {
		t.Error("导出没成时，刚建的空文件夹应删掉")
	}
	// 正常：建出来、开始打包，包落在新建的文件夹里
	rec := e.call(t, "POST", ip+"/transfer/export", body(missing, true, nil), 200)
	var r struct {
		Data struct {
			TaskID string `json:"taskId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil || r.Data.TaskID == "" {
		t.Fatalf("导出响应：%s (%v)", rec.Body, err)
	}
	if st, err := os.Stat(missing); err != nil || !st.IsDir() {
		t.Fatalf("文件夹没建出来：%v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		b := e.call(t, "GET", "/api/tasks/"+r.Data.TaskID, "", 200).Body.String()
		if strings.Contains(b, `"ended":true`) {
			if !strings.Contains(b, "新的交接包") {
				t.Errorf("交接包应落在新建的文件夹里：%s", b)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("导出任务没在时限内结束")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// 文件夹已经在了：带着 mkdir 再导出一次也照常（不报「已经在」，也不会在导出没成时被删）
	if rec := e.call(t, "POST", ip+"/transfer/export", body(missing, true, map[string]any{"mode": "别的什么"}), 400); !strings.Contains(rec.Body.String(), "不认识") {
		t.Errorf("打包方式不认识：%s", rec.Body)
	}
	if !exists(missing) {
		t.Error("原来就在的文件夹不该被删")
	}
}
