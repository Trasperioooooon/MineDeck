package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/java"
	"minedeck/internal/transfer"
)

// 服务器打包 / 收包的网页接口：上传 → 清点 → 收包落地。收包要解析 Java，在缓存里放一个假的
// 本机 Java 17（java.Resolve 先查已缓存），整条链路离线走通。
func TestTransferRoutes(t *testing.T) {
	e := newFullEnv(t)

	// 假的本机 Java 17（收包请求选它，java.Resolve 命中缓存，不联网）
	jdk := filepath.Join(app.CacheDir, "jdk17", "bin", "java.exe")
	if err := os.MkdirAll(filepath.Dir(jdk), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jdk, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	javas, _ := json.Marshal([]java.ScannedJava{{Path: jdk, Major: 17, Version: "17.0.1-test"}})
	if err := os.WriteFile(filepath.Join(app.CacheDir, "javas.json"), javas, 0o644); err != nil {
		t.Fatal(err)
	}

	// 造一个服主打来的包：完整的实例目录 → transfer.Pack（不联网）
	src := t.TempDir()
	writeTree := func(rel, content string) {
		t.Helper()
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := inst.Settings{Name: "朋友的服", Core: "vanilla", MC: "1.21.1", JavaMajor: 21,
		XmxMB: 4096, XmsMB: 1024, LaunchTarget: "jar:server.jar",
		CreatedAt: time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local)}
	sb, _ := json.Marshal(s)
	writeTree("instance.json", string(sb))
	writeTree("server.properties", "server-port=25599\nlevel-name=world\n")
	writeTree("eula.txt", "eula=true\n")
	writeTree("server.jar", "jar-content")
	writeTree("world/level.dat", "level")
	writeTree("world/playerdata/uid.dat", "everyone-progress")
	writeTree("mods/a.jar", "PK")
	pack := filepath.Join(t.TempDir(), "朋友的服.mdpack")
	if _, err := transfer.Pack(context.Background(), pack, transfer.PackOptions{
		Dir: src, Instance: sb, Mode: transfer.ModeDistributable, ModDir: "mods", Exporter: "MineDeck 3.0.0"}); err != nil {
		t.Fatal(err)
	}

	// ---- 上传 + 清点 ----
	rec := e.postFile(t, "/api/transfer/upload", "file", pack)
	if rec.Code != 200 {
		t.Fatalf("上传失败：HTTP %d：%s", rec.Code, rec.Body)
	}
	var up struct {
		OK   bool `json:"ok"`
		Data struct {
			File string `json:"file"`
			Sum  struct {
				Mode      string `json:"mode"`
				Exporter  string `json:"exporter"`
				Port      int    `json:"port"`
				EULAAgree bool   `json:"eulaAgreed"`
				ModCount  int    `json:"modCount"`
				WorldSize int64  `json:"worldSize"`
				Instance  struct {
					Name      string `json:"name"`
					Core      string `json:"core"`
					MC        string `json:"mc"`
					JavaMajor int    `json:"javaMajor"`
				} `json:"instance"`
				Excluded []string `json:"excluded"`
			} `json:"summary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil {
		t.Fatal(err)
	}
	sum := up.Data.Sum
	if up.Data.File == "" || !strings.Contains(up.Data.File, filepath.Join(app.CacheDir, "transfer")) {
		t.Errorf("上传应落在缓存目录里：%q", up.Data.File)
	}
	if sum.Mode != transfer.ModeDistributable || sum.Exporter != "MineDeck 3.0.0" {
		t.Errorf("mode / 导出方不对: %+v", sum)
	}
	if sum.Port != 25599 || !sum.EULAAgree || sum.ModCount != 1 || sum.WorldSize <= 0 {
		t.Errorf("清点不对: port=%d eula=%v mods=%d world=%d", sum.Port, sum.EULAAgree, sum.ModCount, sum.WorldSize)
	}
	if sum.Instance.Name != "朋友的服" || sum.Instance.JavaMajor != 21 {
		t.Errorf("包里实例摘要不对: %+v", sum.Instance)
	}
	if !contains(sum.Excluded, "logs") || !contains(sum.Excluded, "instance.json") {
		t.Errorf("剔除清单应说清剔了什么: %v", sum.Excluded)
	}

	// ---- 收包 ----
	body := `{"file":` + esc(up.Data.File) + `,"name":"寒假续上","port":25000,"javaMajor":17,"eula":true}`
	rec = e.call(t, "POST", "/api/transfer/import", body, 200)
	var imp struct {
		Data struct {
			TaskID string `json:"taskId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &imp); err != nil || imp.Data.TaskID == "" {
		t.Fatalf("收包应返回任务编号：%s (%v)", rec.Body, err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		rec = e.call(t, "GET", "/api/tasks/"+imp.Data.TaskID, "", 200)
		var task struct {
			Data struct {
				Kind   string `json:"kind"`
				Ended  bool   `json:"ended"`
				Err    string `json:"error"`
				Result string `json:"result"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		if task.Data.Ended {
			if task.Data.Err != "" || task.Data.Kind != "unpack" || task.Data.Result != "寒假续上" {
				t.Fatalf("收包任务没成功: %+v (%s)", task.Data, rec.Body)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("收包任务超时: %s", rec.Body)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 落地实例：名字、端口、Java 都按收包选择
	rec = e.call(t, "GET", "/api/instances", "", 200)
	var list struct {
		Data []struct {
			Name  string `json:"name"`
			Port  int    `json:"port"`
			JavaM int    `json:"javaMajor"`
			Dir   string `json:"dir"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	var found *struct {
		Name  string `json:"name"`
		Port  int    `json:"port"`
		JavaM int    `json:"javaMajor"`
		Dir   string `json:"dir"`
	}
	for i := range list.Data {
		if list.Data[i].Name == "寒假续上" {
			found = &list.Data[i]
		}
	}
	if found == nil {
		t.Fatalf("收完包列表里应有「寒假续上」：%s", rec.Body)
	}
	if found.Port != 25000 || found.JavaM != 17 {
		t.Errorf("端口 / Java 不对: %+v", found)
	}
	if b, err := os.ReadFile(filepath.Join(found.Dir, "world", "playerdata", "uid.dat")); err != nil || string(b) != "everyone-progress" {
		t.Errorf("全员进度应原样落地: %v %q", err, string(b))
	}

	// ---- 收包文件只认缓存目录里的 ----
	e.call(t, "POST", "/api/transfer/import", `{"file":"C:\别的\p.mdpack","name":"x"}`, 400)
	// 局域网设备看不到这三件事（上面 routes_test 覆盖过 export / import 的 403；upload 同一个操作）
}

// postFile 发一个 multipart 上传（file 字段带路径里的文件）。
func (e *fullEnv) postFile(t *testing.T, path, field, file string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filepath.Base(file))
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(fw, f); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", path, &buf)
	req.Host = "127.0.0.1"
	req.RemoteAddr = "127.0.0.1:52000"
	req.Header.Set("X-Token", Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func contains(list []string, needle string) bool {
	for _, e := range list {
		if e == needle {
			return true
		}
	}
	return false
}

func esc(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
