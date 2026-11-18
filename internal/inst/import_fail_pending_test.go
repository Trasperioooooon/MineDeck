package inst

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/atomicfile"
	"minedeck/internal/dl"
	"minedeck/internal/mcsrc"
	"minedeck/internal/modpack"
	"minedeck/internal/tasks"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubWaterfallDownloads(t *testing.T) {
	t.Helper()
	jar := bytes.Repeat([]byte("W"), 256*1024)
	sum := sha256.Sum256(jar)
	sumHex := hex.EncodeToString(sum[:])
	metaJSON := fmt.Sprintf(
		`{"id":1,"channel":"DEFAULT","downloads":{"server:default":{"name":"waterfall.jar","url":"https://fill-data.example/waterfall.jar","size":%d,"checksums":{"sha256":%q}}}}`,
		len(jar), sumHex,
	)
	old := dl.Client.Transport
	t.Cleanup(func() { dl.Client.Transport = old })
	dl.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case "https://fill.papermc.io/v3/projects/waterfall/versions/1.20/builds/latest":
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(metaJSON)),
			}, nil
		case "https://fill-data.example/waterfall.jar":
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"application/java-archive"}},
				Body:       io.NopCloser(bytes.NewReader(jar)),
			}, nil
		default:
			return nil, fmt.Errorf("测试不应联网请求：%s", r.URL.String())
		}
	})
}

func waitImportTask(t *testing.T, m *Manager, id string) tasks.Snapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tk := m.Tasks.Get(id)
		if tk != nil {
			if sn := tk.Snapshot(); sn.Ended {
				return sn
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待导入任务 %s 结束超时", id)
	return tasks.Snapshot{}
}

func testPendingPack(t *testing.T) *modpack.Pack {
	t.Helper()
	return &modpack.Pack{
		Format:       modpack.FormatMrpack,
		Name:         "测试整合包",
		Core:         mcsrc.CoreWaterfall,
		MC:           "1.20",
		Loader:       "latest",
		ZipPath:      zipOrdered(t, [][2]string{{"overrides/config/a.txt", "hi"}}),
		OverrideDirs: []string{"overrides"},
		Pending: []modpack.PendingFile{
			{Path: "mods/missing.jar", Reason: modpack.ReasonNoHash, Size: 123},
		},
	}
}

// 整合包导入在设了待决清单以后失败或取消：
//   - 失败 / 取消结束时，任务的 Pending 清单、import-pending 告警和任务关联都要清掉，文件夹也要清掉；
//   - 成功导入时照旧保留待决清单和告警，且照旧能处置。
func TestImportModpackPendingClearedOnFailOrCancel(t *testing.T) {
	stubWaterfallDownloads(t)

	t.Run("收尾失败时清掉待决清单与告警", func(t *testing.T) {
		m, _ := importEnv(t)
		var instDir string
		oldHook := atomicfile.Hook
		t.Cleanup(func() { atomicfile.Hook = oldHook })
		atomicfile.Hook = func(p string) {
			if filepath.Base(p) == PendingReportFile {
				instDir = filepath.Dir(p)
				// 此时 finishImportPack 已设定 Pending 与告警，制造随后的 writeRunBat 失败
				_ = os.Mkdir(filepath.Join(instDir, "run.bat"), 0o755)
			}
		}

		id, err := m.ImportModpackAsync(testPendingPack(t), CreateReq{Name: "失败服"}, "")
		if err != nil {
			t.Fatal(err)
		}
		sn := waitImportTask(t, m, id)
		if sn.Err == "" || sn.Cancelled {
			t.Fatalf("应按失败结束：%+v", sn)
		}
		if len(sn.Pending) != 0 {
			t.Fatalf("失败后任务上不应残留待决清单：%+v", sn.Pending)
		}
		if len(sn.Warnings) != 0 {
			t.Fatalf("失败后不应残留告警：%+v", sn.Warnings)
		}
		if _, err := os.Stat(instDir); !os.IsNotExist(err) {
			t.Fatalf("失败后实例文件夹应已清掉：%s %v", instDir, err)
		}
		if _, _, err := m.ResolveImportPending(context.Background(), id, []PendingAction{{ID: "p1", Act: "skip"}}); err == nil {
			t.Fatal("失败后不应再允许处置待决文件")
		}
	})

	t.Run("设完待决后取消时清掉待决清单与告警", func(t *testing.T) {
		m, _ := importEnv(t)
		idCh := make(chan string, 1)
		var instDir string
		oldHook := atomicfile.Hook
		t.Cleanup(func() { atomicfile.Hook = oldHook })
		atomicfile.Hook = func(p string) {
			if filepath.Base(p) == PendingReportFile {
				instDir = filepath.Dir(p)
				id := <-idCh
				m.Tasks.Cancel(id)
			}
		}

		id, err := m.ImportModpackAsync(testPendingPack(t), CreateReq{Name: "取消服"}, "")
		if err != nil {
			t.Fatal(err)
		}
		idCh <- id
		sn := waitImportTask(t, m, id)
		if !sn.Cancelled {
			t.Fatalf("应按已取消结束：%+v", sn)
		}
		if len(sn.Pending) != 0 {
			t.Fatalf("取消后任务上不应残留待决清单：%+v", sn.Pending)
		}
		if len(sn.Warnings) != 0 {
			t.Fatalf("取消后不应残留告警：%+v", sn.Warnings)
		}
		if _, err := os.Stat(instDir); !os.IsNotExist(err) {
			t.Fatalf("取消后实例文件夹应已清掉：%s %v", instDir, err)
		}
		if _, _, err := m.ResolveImportPending(context.Background(), id, []PendingAction{{ID: "p1", Act: "skip"}}); err == nil {
			t.Fatal("取消后不应再允许处置待决文件")
		}
	})

	t.Run("成功导入时照旧保留待决清单并可处置", func(t *testing.T) {
		m, _ := importEnv(t)
		id, err := m.ImportModpackAsync(testPendingPack(t), CreateReq{Name: "成功服"}, "")
		if err != nil {
			t.Fatal(err)
		}
		sn := waitImportTask(t, m, id)
		if sn.Err != "" || sn.Result != "成功服" {
			t.Fatalf("应成功完成：%+v", sn)
		}
		if len(sn.Pending) != 1 || sn.Pending[0].Path != "mods/missing.jar" {
			t.Fatalf("成功导入后待决清单应仍在：%+v", sn.Pending)
		}
		out, remain, err := m.ResolveImportPending(context.Background(), id, []PendingAction{{ID: "p1", Act: "skip"}})
		if err != nil || len(out) != 1 || !out[0].OK || len(remain) != 0 {
			t.Fatalf("成功导入后应能正常处置待决文件：out=%+v remain=%+v err=%v", out, remain, err)
		}
	})
}
