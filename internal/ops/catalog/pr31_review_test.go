package catalog

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/java"
	"minedeck/internal/mcsrc"
	"minedeck/internal/ops"
	"minedeck/internal/tunnel"
)

func newCatalogMgr(t *testing.T) (*ops.Registry, *inst.Manager) {
	t.Helper()
	newCatalog(t)
	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	return New(Env{Mgr: mgr, Tun: tunnel.NewManager(), Port: 27333}), mgr
}

func TestJavaRemoveBlocksCreatingAndAliasPath(t *testing.T) {
	reg, mgr := newCatalogMgr(t)
	ui := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}

	if err := os.MkdirAll(filepath.Join(java.Dir(21), "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(java.Exe(21), []byte("jvm21"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. 有服务器正在创建/导入时拒绝卸载便携 Java（409）
	creq := inst.CreateReq{Name: "建服中", Core: mcsrc.CorePaper, MC: "1.21.1", EULA: true}
	cdir, err := mgr.ReserveCreate(&creq)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Do(context.Background(), ui, "java.remove", ops.Args(`{"major":21,"confirm":true}`))
	var oe *ops.Error
	if !errors.As(err, &oe) || oe.Code != 409 || !strings.Contains(err.Error(), "正在创建") {
		t.Fatalf("创建进行中应回 409 拒绝卸载：%v", err)
	}
	mgr.ReleaseCreate(creq.Name, cdir)

	// 2. 实例通过目录联接别名指向便携 Java 时，javaPathInside 仍能识别并回 409 拒绝
	if runtime.GOOS == "windows" {
		alias := filepath.Join(t.TempDir(), "rt-link")
		if _, jerr := exec.Command("cmd", "/c", "mklink", "/J", alias, java.Dir(21)).CombinedOutput(); jerr == nil {
			t.Cleanup(func() { _ = os.Remove(alias) })
			srvDir := filepath.Join(app.ServersDir, "alias-srv")
			if err := os.MkdirAll(srvDir, 0o755); err != nil {
				t.Fatal(err)
			}
			_, err := mgr.FinalizeCreate(inst.Settings{
				Name: "别名服", Core: mcsrc.CorePaper, MC: "1.21.1",
				JavaMajor: 21, JavaPath: filepath.Join(alias, "bin", "java.exe"),
				XmxMB: 1024, XmsMB: 512, LaunchTarget: "jar:server.jar",
			}, srvDir)
			if err != nil {
				t.Fatal(err)
			}
			_, rerr := reg.Do(context.Background(), ui, "java.remove", ops.Args(`{"major":21,"confirm":true}`))
			if !errors.As(rerr, &oe) || oe.Code != 409 || !strings.Contains(rerr.Error(), "别名服") {
				t.Fatalf("联接别名指向便携 Java 应回 409 拒绝：%v", rerr)
			}
			_ = mgr.Delete("别名服", true)
		}
	}

	// 3. 无人占用后可正常卸载
	if _, err := reg.Do(context.Background(), ui, "java.remove", ops.Args(`{"major":21,"confirm":true}`)); err != nil {
		t.Fatalf("无人占用时应能卸载：%v", err)
	}
}

func TestCreatingListNoLogAndDiscardStatusCodes(t *testing.T) {
	reg, mgr := newCatalogMgr(t)
	ui := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}

	if _, err := reg.Do(context.Background(), ui, "instance.creating.list", nil); err != nil {
		t.Fatal(err)
	}
	if recs, _ := reg.Log.Query(ops.Query{Op: "instance.creating.list"}); len(recs) != 0 {
		t.Fatalf("instance.creating.list 是只读轮询，不应写操作日志：%+v", recs)
	}

	// 正在创建时删除半成品应回 409
	creq := inst.CreateReq{Name: "半成品", Core: mcsrc.CorePaper, MC: "1.21.1", EULA: true, Root: app.ServersDir}
	cdir, err := mgr.ReserveCreate(&creq)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.ReleaseCreate(creq.Name, cdir)
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := inst.WriteCreatingMarker(cdir, creq.Name); err != nil {
		t.Fatal(err)
	}
	_, err = reg.Do(context.Background(), ui, "instance.creating.discard", ops.Args(`{"id":"0/`+filepath.Base(cdir)+`","confirm":true}`))
	var oe *ops.Error
	if !errors.As(err, &oe) || oe.Code != 409 {
		t.Fatalf("正在创建的目录应回 409：%v", err)
	}
}
