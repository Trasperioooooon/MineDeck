package catalog

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/modcart"
	"minedeck/internal/ops"
)

// 2026-11-10 合并前审计查出来的：往购物车里加东西是读操作，AI 助手不用确认就能做；安装又是照「执行那一刻」的车来装，
// 不管人当时看到的是什么。两样叠起来，写操作要人确认这一层就空了。这里的用例守的是：装的必须是人看过、确认过的那些。

func cartIDs(cart *modcart.Service, inst string) []string {
	var ids []string
	for _, it := range cart.List(inst) {
		ids = append(ids, it.ProjectID)
	}
	return ids
}

func modsIn(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	ents, _ := os.ReadDir(filepath.Join(dir, "mods"))
	for _, e := range ents {
		if e.Name() != "needy.jar" { // 测试环境里原本就有的那个
			out = append(out, e.Name())
		}
	}
	return out
}

// TestModCartInstallBoundToSeen 人在界面上点「安装」：请求里带着他当时看到的项目，和现在的车对不上就不装。
func TestModCartInstallBoundToSeen(t *testing.T) {
	reg, mgr, dir, cart := newCartCatalog(t)
	ai := ops.Caller{Source: ops.SrcAssistant, Session: "s1", Local: true}
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"fapi"}}); err != nil {
		t.Fatal(err)
	}
	seen := cartIDs(cart, "模组服") // 人打开购物车标签时看到的：只有 Fabric API
	// 之后 AI 又往车里加了一项（读操作，直接执行，不出卡片）
	if _, err := cartDo(t, reg, ai, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}}); err != nil {
		t.Fatal(err)
	}
	_, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服", "expect": seen})
	if ops.CodeOf(err) != 409 || !strings.Contains(err.Error(), "不一样") {
		t.Fatalf("车里多了人没看过的东西，安装应报 409：%v", err)
	}
	if got := modsIn(t, dir); len(got) != 0 {
		t.Fatalf("被拒的安装不该落下任何文件：%v", got)
	}
	if len(cart.List("模组服")) != 2 {
		t.Errorf("被拒以后车不该动：%v", cartIDs(cart, "模组服"))
	}
	// 被拒以后占位要放掉：看过现在的车再点，照常装
	m, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服", "expect": cartIDs(cart, "模组服")})
	if err != nil {
		t.Fatalf("看到的和车里的一致，应照常安装：%v", err)
	}
	if s := waitTask(t, mgr, m["taskId"].(string)); s.Err != "" {
		t.Fatalf("安装任务：%+v", s)
	}
	if got := strings.Join(modsIn(t, dir), " "); got != "alpha-1.0.jar fabric-api-1.0.jar" {
		t.Errorf("装上的文件不对：%s", got)
	}
	// 顺序不同、少一项、多一项、重复的：只有同一批才算一致
	for _, c := range []struct {
		items []string
		want  []string
		same  bool
	}{
		{[]string{"a", "b"}, []string{"b", "a"}, true},
		{[]string{"a", "b"}, []string{"a"}, false},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{"a", "b"}, []string{"a", "a"}, false},
		{nil, []string{}, true},
	} {
		var items []modcart.Item
		for _, id := range c.items {
			items = append(items, modcart.Item{ProjectID: id})
		}
		if got := sameProjects(items, c.want); got != c.same {
			t.Errorf("sameProjects(%v, %v) = %v，应为 %v", c.items, c.want, got, c.same)
		}
	}
}

// TestModCartDraftBoundToSeen 新建向导：建服完成后带着草稿车一起装。草稿车里多出人没看过的东西，
// 或者同名的旧购物车里还剩着东西（以前删掉的同名服务器留下的），都不能悄悄装进新服务器。
func TestModCartDraftBoundToSeen(t *testing.T) {
	reg, _, dir, cart := newCartCatalog(t)
	ai := ops.Caller{Source: ops.SrcAssistant, Session: "s1", Local: true}
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"projectIds": []string{"fapi"}, "core": "fabric", "mc": "1.20.1"}); err != nil {
		t.Fatal(err)
	}
	seen := cartIDs(cart, modcart.Draft) // 向导「选模组」那一步上显示的
	// 实例名对应的那辆车里有东西（AI 加的，或者以前同名服务器留下的）：人在向导上一项都没看到
	if _, err := cartDo(t, reg, ai, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服", "includeDraft": true, "expect": seen}); ops.CodeOf(err) != 409 {
		t.Fatalf("实例的车里有向导上没显示的东西，应报 409：%v", err)
	}
	if got := modsIn(t, dir); len(got) != 0 {
		t.Fatalf("被拒的安装不该落下任何文件：%v", got)
	}
}

// TestInstanceDeleteClearsCart 删掉服务器时把它的购物车一起清掉：购物车按实例名存，
// 不清的话以后新建一台同名的，旧车里剩下的东西会被当成新服务器的。
func TestInstanceDeleteClearsCart(t *testing.T) {
	reg, _, _, cart := newCartCatalog(t)
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Do(context.Background(), cartUI, "instance.delete", ops.ArgsOf(map[string]any{"inst": "模组服", "files": false})); err != nil {
		t.Fatalf("删除：%v", err)
	}
	if left := cart.List("模组服"); len(left) != 0 {
		t.Errorf("删掉服务器以后它的购物车应清空：%v", cartIDs(cart, "模组服"))
	}
}

// TestModCartProposalBoundAtRun AI 提的安装、人在卡片上确认：确认时会重新预览一遍并比对，但比对和执行之间还隔着一段
// （预览要联网，几秒到几十秒）。这段时间里车变了，或者 Modrinth 上发了新版本，执行时要自己认出来，不装。
func TestModCartProposalBoundAtRun(t *testing.T) {
	reg, mgr, dir, cart := newCartCatalog(t)
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"fapi"}}); err != nil {
		t.Fatal(err)
	}
	op := reg.Op("modcart.install")
	if op == nil {
		t.Fatal("没有 modcart.install")
	}
	args := ops.ArgsOf(map[string]any{"inst": "模组服"})
	plan, err := op.Preview(context.Background(), cartUI, args)
	if err != nil {
		t.Fatal(err)
	}
	confirmed := plan.Pre // 人在卡片上确认的那一份
	// 车在确认之后变了（AI 又加了一项）：执行时读到的车和确认的对不上
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := op.Run(ops.WithConfirmedPre(context.Background(), confirmed), cartUI, args); ops.CodeOf(err) != 409 {
		t.Fatalf("确认之后车变了，执行应报 409：%v", err)
	}
	if got := modsIn(t, dir); len(got) != 0 {
		t.Fatalf("不该落下任何文件：%v", got)
	}
	// 车没变，但要装的文件和卡片上列的不一样了（比如发了新版本）：任务失败，一个文件都不下载
	if _, err := cart.Remove("模组服", "aaa"); err != nil {
		t.Fatal(err)
	}
	stale := map[string]string{"待装清单": confirmed["待装清单"], "本地文件": confirmed["本地文件"], "要装的文件": "和卡片上不一样的指纹"}
	res, err := op.Run(ops.WithConfirmedPre(context.Background(), stale), cartUI, args)
	if err != nil {
		t.Fatalf("车对得上时应先建任务：%v", err)
	}
	s := waitTask(t, mgr, res.(map[string]string)["taskId"])
	if !strings.Contains(s.Err, "不一样") {
		t.Fatalf("文件清单对不上，任务应失败：%+v", s)
	}
	if got := modsIn(t, dir); len(got) != 0 {
		t.Fatalf("文件清单对不上时一个文件都不该下载：%v", got)
	}
	// 都对得上：照常装。任务结束后占位已放掉，这里顺便验证了
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err = op.Run(ops.WithConfirmedPre(context.Background(), confirmed), cartUI, args)
		if err == nil || ops.CodeOf(err) != 409 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond) // 上一个任务刚失败，占位可能还没放掉
	}
	if err != nil {
		t.Fatalf("确认的内容和现在一致，应照常安装：%v", err)
	}
	if s := waitTask(t, mgr, res.(map[string]string)["taskId"]); s.Err != "" {
		t.Fatalf("安装任务：%+v", s)
	}
	if got := strings.Join(modsIn(t, dir), " "); got != "fabric-api-1.0.jar" {
		t.Errorf("装上的应正好是卡片上那一个文件：%s", got)
	}
}

// ---- 本地文件（人从自己电脑上添加的 jar）----
//
// 这一组守的是：本地文件只有人能放、能拿；装进服务器之前一定有人看过（界面带 expectLocal，或者在提案卡片上确认过）；
// 校验在操作层，不信网页层给的路径和文件名。

// localJar 一个只有 fabric.mod.json 的 jar。
func localJar(t *testing.T, id string, depends map[string]string) []byte {
	t.Helper()
	meta, _ := json.Marshal(map[string]any{"id": id, "name": "Mod " + id, "version": "1.0", "depends": depends})
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("fabric.mod.json")
	_, _ = w.Write(meta)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var uploadSeq int

// uploaded 像网页层那样把上传的内容存进上传目录，返回临时文件的路径。
func uploaded(t *testing.T, body []byte) string {
	t.Helper()
	dir := LocalUploadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	uploadSeq++
	p := filepath.Join(dir, fmt.Sprintf("%d.upload", uploadSeq))
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// addLocalFile 人在界面上添加一个本地文件，返回它的 SHA-1。
func addLocalFile(t *testing.T, reg *ops.Registry, inst, name string, body []byte) string {
	t.Helper()
	m, err := cartDo(t, reg, cartUI, "modcart.local.add", map[string]any{"inst": inst, "file": uploaded(t, body), "fileName": name})
	if err != nil {
		t.Fatalf("添加本地文件 %s：%v", name, err)
	}
	return m["added"].(map[string]any)["sha1"].(string)
}

// countFiles dir 下面（含子目录）一共有几个文件。
func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func localSHAs(cart *modcart.Service, inst string) []string {
	out := []string{}
	for _, lf := range cart.Locals(inst) {
		out = append(out, lf.SHA1)
	}
	return out
}

// TestModCartLocalHumanOnly 添加、移除本地文件只许人：应用内 AI 助手、MCP 客户端、游戏内 AI 都调不了，工具清单里也没有。
// 不限本机（同整合包上传）：局域网里的人可以加。
func TestModCartLocalHumanOnly(t *testing.T) {
	reg, _, _, cart := newCartCatalog(t)
	sum := addLocalFile(t, reg, "", "mine.jar", localJar(t, "mine", nil))
	for _, src := range []ops.Source{ops.SrcAssistant, ops.SrcMCP, ops.SrcGameChat, ops.SrcCompanion, ops.SrcSchedule} {
		c := ops.Caller{Source: src, Session: "s1", Actor: "x", Local: true}
		up := uploaded(t, localJar(t, "byai", nil))
		if _, err := cartDo(t, reg, c, "modcart.local.add", map[string]any{"file": up, "fileName": "byai.jar"}); ops.CodeOf(err) != 403 {
			t.Errorf("%s 不该能添加本地文件：%v", src, err)
		}
		if _, err := cartDo(t, reg, c, "modcart.local.remove", map[string]any{"sha1": sum}); ops.CodeOf(err) != 403 {
			t.Errorf("%s 不该能移除本地文件：%v", src, err)
		}
		for _, op := range reg.Available(c) {
			if strings.HasPrefix(op.Name, "modcart.local.") {
				t.Errorf("%s 的工具清单里不该有 %s", src, op.Name)
			}
		}
	}
	if got := localSHAs(cart, modcart.Draft); len(got) != 1 || got[0] != sum {
		t.Fatalf("被拒的调用不该动清单：%v", got)
	}
	// AI 给安装带 expectLocal 没有用：这个参数只认人给的（AI 的工具说明里也没有）
	if strings.Contains(string(reg.Op("modcart.install").AISchema()), "expectLocal") {
		t.Error("给 AI 的 modcart_install 参数说明里不该有 expectLocal")
	}
	// 局域网里的人可以加、可以移除
	lan := ops.Caller{Source: ops.SrcLAN, Actor: "192.168.1.9"}
	m, err := cartDo(t, reg, lan, "modcart.local.add", map[string]any{"file": uploaded(t, localJar(t, "lan", nil)), "fileName": "lan.jar"})
	if err != nil || m["total"].(float64) != 2 {
		t.Fatalf("局域网添加：%v %v", m, err)
	}
	if m, err = cartDo(t, reg, lan, "modcart.local.remove", map[string]any{"sha1": sum}); err != nil || len(m["local"].([]any)) != 1 {
		t.Fatalf("局域网移除：%v %v", m, err)
	}
}

// TestModCartLocalAddValidates 校验在操作层：只认上传目录里的文件；文件名、扩展名、是不是 zip 都查；
// 没放进清单的临时文件不留；结果里没有路径。
func TestModCartLocalAddValidates(t *testing.T) {
	reg, _, _, cart := newCartCatalog(t)
	// 不在上传目录里的文件：拒绝，而且不能把它删了
	outside := filepath.Join(t.TempDir(), "secret.jar")
	if err := os.WriteFile(outside, localJar(t, "x", nil), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{outside, filepath.Join(LocalUploadDir(), "..", "..", "config.json"), filepath.Join(LocalUploadDir(), "sub", "a.upload"), ""} {
		if _, err := cartDo(t, reg, cartUI, "modcart.local.add", map[string]any{"file": f, "fileName": "x.jar"}); ops.CodeOf(err) != 400 {
			t.Errorf("上传目录外的 %q 应报 400：%v", f, err)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("上传目录外的文件被动了")
	}
	jar := localJar(t, "ok", nil)
	for name, say := range map[string]string{
		"../evil.jar":                     "不能用的字符",
		`..\evil.jar`:                     "不能用的字符",
		"mods/evil.jar":                   "不能用的字符",
		"evil.bat":                        ".jar",
		"run.bat":                         ".jar",
		"instance.json":                   ".jar",
		"a\nb.jar":                        "不能用的字符",
		"a‮b.jar":                         "特殊字符",
		"":                                "没有名字",
		strings.Repeat("长", 120) + ".jar": "太长",
	} {
		up := uploaded(t, jar)
		if _, err := cartDo(t, reg, cartUI, "modcart.local.add", map[string]any{"file": up, "fileName": name}); ops.CodeOf(err) != 400 || !strings.Contains(err.Error(), say) {
			t.Errorf("文件名 %q 应报 400「%s」：%v", name, say, err)
		}
		if _, err := os.Stat(up); !os.IsNotExist(err) {
			t.Errorf("文件名 %q 被拒以后临时文件应删掉", name)
		}
	}
	up := uploaded(t, []byte("MZ 这其实是个 exe"))
	if _, err := cartDo(t, reg, cartUI, "modcart.local.add", map[string]any{"file": up, "fileName": "fake.jar"}); ops.CodeOf(err) != 400 || !strings.Contains(err.Error(), "打不开") {
		t.Errorf("不是 zip 的应报 400：%v", err)
	}
	// 实例不存在、原版核心装不了：不收
	if _, err := cartDo(t, reg, cartUI, "modcart.local.add", map[string]any{"inst": "没有这台", "file": uploaded(t, jar), "fileName": "a.jar"}); ops.CodeOf(err) != 404 {
		t.Errorf("实例不存在应报 404：%v", err)
	}
	if len(cart.Locals(modcart.Draft)) != 0 {
		t.Fatalf("被拒的都不该进清单：%v", localSHAs(cart, modcart.Draft))
	}
	if ents, _ := os.ReadDir(LocalUploadDir()); len(ents) != 0 {
		t.Errorf("上传目录里不该留下临时文件：%v", ents)
	}

	// 正常添加：返回整份清单；同一个文件再加一次不重复；结果里没有路径
	up = uploaded(t, jar)
	m, err := cartDo(t, reg, cartUI, "modcart.local.add", map[string]any{"inst": "模组服", "file": up, "fileName": "ok-1.0.jar"})
	if err != nil {
		t.Fatal(err)
	}
	local := m["local"].([]any)
	if m["count"].(float64) != 0 || m["total"].(float64) != 1 || len(local) != 1 || m["duplicate"] != false || m["items"] == nil {
		t.Fatalf("添加以后的清单：%v", m)
	}
	row := local[0].(map[string]any)
	if row["fileName"] != "ok-1.0.jar" || row["title"] != "Mod ok" || row["modId"] != "ok" || row["loader"] != "fabric" || len(row["sha1"].(string)) != 40 || row["warn"] != nil {
		t.Fatalf("清单里的本地文件：%v", row)
	}
	if b := string(mustJSON(m)); strings.Contains(b, "upload") || strings.Contains(b, filepath.Base(filepath.Dir(LocalUploadDir()))+`\\`) || strings.Contains(b, `:\\`) {
		t.Errorf("结果里不该有路径：%s", b)
	}
	if m, err = cartDo(t, reg, cartUI, "modcart.local.add", map[string]any{"inst": "模组服", "file": uploaded(t, jar), "fileName": "again.jar"}); err != nil || m["duplicate"] != true || m["total"].(float64) != 1 {
		t.Fatalf("同一个文件再加：%v %v", m, err)
	}
	if _, err = cartDo(t, reg, cartUI, "modcart.local.add", map[string]any{"inst": "模组服", "file": uploaded(t, localJar(t, "ok", map[string]string{"z": "*"})), "fileName": "OK-1.0.jar"}); ops.CodeOf(err) != 409 {
		t.Fatalf("同名不同内容应报 409：%v", err)
	}
	if ents, _ := os.ReadDir(LocalUploadDir()); len(ents) != 0 {
		t.Errorf("上传目录里不该留下临时文件：%v", ents)
	}
	// 加载器不对的提醒跟着这台服务器的核心走（模组服是 Fabric）；清单给 AI 看时没有 SHA-1、没有路径
	forge := func() []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("META-INF/mods.toml")
		_, _ = w.Write([]byte("[[mods]]\nmodId = \"forgey\"\nversion = \"1\"\n"))
		_ = zw.Close()
		return buf.Bytes()
	}()
	addLocalFile(t, reg, "模组服", "forgey.jar", forge)
	m, _ = cartDo(t, reg, cartUI, "modcart.list", map[string]any{"inst": "模组服"})
	if w, _ := m["local"].([]any)[1].(map[string]any)["warn"].(string); !strings.Contains(w, "给 Forge 用的") {
		t.Errorf("加载器不对应有提醒：%v", m["local"])
	}
	ai := ops.Caller{Source: ops.SrcAssistant, Session: "s1", Local: true}
	m, err = cartDo(t, reg, ai, "modcart.list", map[string]any{"inst": "模组服"})
	if err != nil || len(m["local"].([]any)) != 2 {
		t.Fatalf("AI 看清单：%v %v", m, err)
	}
	if b := string(mustJSON(m)); strings.Contains(b, "sha1") || strings.Contains(b, `\\`) || !strings.Contains(b, "forgey.jar") {
		t.Errorf("给 AI 的清单应有文件名、没有 SHA-1 和路径：%s", b)
	}
}

// TestModCartClearKeepsLocalForAI AI 调清空只清 Modrinth 项目，人放的本地文件留着；人在界面上清空两样都清，暂存的文件也删。
func TestModCartClearKeepsLocalForAI(t *testing.T) {
	reg, _, _, cart := newCartCatalog(t)
	addLocalFile(t, reg, "模组服", "mine.jar", localJar(t, "mine", nil))
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}}); err != nil {
		t.Fatal(err)
	}
	for _, src := range []ops.Source{ops.SrcAssistant, ops.SrcMCP} {
		m, err := cartDo(t, reg, ops.Caller{Source: src, Session: "s1", Local: true}, "modcart.clear", map[string]any{"inst": "模组服"})
		if err != nil || m["count"].(float64) != 0 || len(m["local"].([]any)) != 1 || !strings.Contains(fmt.Sprint(m["note"]), "只有用户能") {
			t.Fatalf("%s 清空：%v %v", src, m, err)
		}
		if len(cart.Locals("模组服")) != 1 {
			t.Fatalf("%s 清空把人放的本地文件清掉了", src)
		}
		_, _ = cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}})
	}
	// AI 的 modcart.remove 拿不掉本地文件（它只认 Modrinth 项目）
	ai := ops.Caller{Source: ops.SrcAssistant, Session: "s1", Local: true}
	if m, _ := cartDo(t, reg, ai, "modcart.remove", map[string]any{"inst": "模组服", "projectId": localSHAs(cart, "模组服")[0]}); len(m["local"].([]any)) != 1 {
		t.Fatalf("modcart.remove 不该拿掉本地文件：%v", m)
	}
	staged := filepath.Join(app.Base, "data", "modcart", "local")
	if n := countFiles(staged); n != 1 {
		t.Fatalf("暂存目录里应有 1 个文件：%d", n)
	}
	m, err := cartDo(t, reg, cartUI, "modcart.clear", map[string]any{"inst": "模组服"})
	if err != nil || m["count"].(float64) != 0 || len(m["local"].([]any)) != 0 || m["total"].(float64) != 0 || m["note"] != nil {
		t.Fatalf("人清空：%v %v", m, err)
	}
	if n := countFiles(staged); n != 0 {
		t.Fatalf("人清空以后暂存的文件应删掉：还有 %d 个", n)
	}
}

// TestModCartInstallBoundToSeenLocal 人点「安装」：清单里有本地文件时，请求必须带着他当时看到的那些（expectLocal），
// 没带、带错、带少了都不装；对得上才装。装好的从清单和暂存里拿掉。
func TestModCartInstallBoundToSeenLocal(t *testing.T) {
	reg, mgr, dir, cart := newCartCatalog(t)
	body := localJar(t, "mine", nil)
	sum := addLocalFile(t, reg, "模组服", "mine-1.0.jar", body)
	other := strings.Repeat("0", 40)
	for name, args := range map[string]map[string]any{
		"没带 expectLocal":  {"inst": "模组服"},
		"只带了 expect":      {"inst": "模组服", "expect": []string{}},
		"expectLocal 是空的": {"inst": "模组服", "expectLocal": []string{}},
		"带的是别的文件":         {"inst": "模组服", "expectLocal": []string{other}},
		"多带了一个":           {"inst": "模组服", "expectLocal": []string{sum, other}},
	} {
		if _, err := cartDo(t, reg, cartUI, "modcart.install", args); ops.CodeOf(err) != 409 || !strings.Contains(err.Error(), "本地文件") {
			t.Errorf("%s：应报 409：%v", name, err)
		}
	}
	// 清单里没有本地文件了，界面却以为有（别的窗口移除了）：也不装
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "生存服", "projectIds": []string{"aaa"}}); err == nil {
		if _, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "生存服", "expectLocal": []string{sum}}); ops.CodeOf(err) != 409 {
			t.Errorf("界面看到的本地文件已经不在了，应报 409：%v", err)
		}
	}
	if got := modsIn(t, dir); len(got) != 0 {
		t.Fatalf("被拒的安装不该落下任何文件：%v", got)
	}
	if len(cart.Locals("模组服")) != 1 {
		t.Fatal("被拒以后清单不该动")
	}
	// 对得上（大写的十六进制也认）：照常装。只有本地文件、没有 Modrinth 项目也能装
	m, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服", "expect": []string{}, "expectLocal": []string{strings.ToUpper(sum)}})
	if err != nil {
		t.Fatalf("看到的和清单一致，应照常安装：%v", err)
	}
	s := waitTask(t, mgr, m["taskId"].(string))
	if s.Err != "" || s.Kind != "mods" || !strings.Contains(strings.Join(s.Log, "\n"), "已放进 mods") || !strings.Contains(strings.Join(s.Log, "\n"), "装好了 1 个文件") {
		t.Fatalf("安装任务：%+v", s)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "mods", "mine-1.0.jar")); err != nil || !bytes.Equal(b, body) {
		t.Fatalf("本地文件没装对：%v", err)
	}
	if got := modsIn(t, dir); len(got) != 1 {
		t.Fatalf("mods 里多了东西（临时文件没清？）：%v", got)
	}
	if len(cart.Locals("模组服")) != 0 || countFiles(filepath.Join(app.Base, "data", "modcart", "local")) != 0 {
		t.Fatal("装好的本地文件应从清单和暂存里拿掉")
	}
}

// TestModCartDraftLocalBoundToSeen 新建向导：建服完成后带着草稿清单一起装。草稿清单里上次半途放弃留下的本地文件，
// 这次向导上没看到（没带 expectLocal，或者带的不是它），不能装进新服务器；只选了本地文件、没选 Modrinth 项目时照样要装。
func TestModCartDraftLocalBoundToSeen(t *testing.T) {
	reg, mgr, dir, cart := newCartCatalog(t)
	left := addLocalFile(t, reg, "", "leftover.jar", localJar(t, "leftover", nil)) // 上次向导留下的
	for name, args := range map[string]map[string]any{
		"向导上什么都没看到":   {"inst": "模组服", "includeDraft": true, "expect": []string{}},
		"向导上看到的是空清单":  {"inst": "模组服", "includeDraft": true, "expect": []string{}, "expectLocal": []string{}},
		"向导上看到的是别的文件": {"inst": "模组服", "includeDraft": true, "expect": []string{}, "expectLocal": []string{strings.Repeat("a", 40)}},
	} {
		if _, err := cartDo(t, reg, cartUI, "modcart.install", args); ops.CodeOf(err) != 409 {
			t.Errorf("%s：留在草稿清单里的本地文件不该被装上，应报 409：%v", name, err)
		}
	}
	if got := modsIn(t, dir); len(got) != 0 {
		t.Fatalf("被拒的安装不该落下任何文件：%v", got)
	}
	// 不带 includeDraft 时草稿清单里的东西和这次安装无关：实例自己的清单是空的，照旧报「是空的」
	if _, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服"}); ops.CodeOf(err) != 400 {
		t.Errorf("实例的清单是空的应报 400：%v", err)
	}
	// 这次向导上看到了它，又加了一个：两个都装（没有 Modrinth 项目也装）
	mine := addLocalFile(t, reg, "", "mine.jar", localJar(t, "mine", nil))
	m, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服", "includeDraft": true, "expect": []string{}, "expectLocal": []string{mine, left}})
	if err != nil {
		t.Fatal(err)
	}
	if s := waitTask(t, mgr, m["taskId"].(string)); s.Err != "" {
		t.Fatalf("安装任务：%+v", s)
	}
	if got := strings.Join(modsIn(t, dir), " "); got != "leftover.jar mine.jar" {
		t.Errorf("装上的文件不对：%s", got)
	}
	if len(cart.Locals(modcart.Draft)) != 0 {
		t.Errorf("装好以后草稿清单里的本地文件应拿掉：%v", localSHAs(cart, modcart.Draft))
	}
}

// TestModCartProposalCoversLocal AI 发起安装：卡片上逐个列出要拷进去的本地文件，本地文件算进前置条件。
// 预览之后清单里多了、少了本地文件，确认时作废；确认和执行之间变了，执行时自己认出来不装。
func TestModCartProposalCoversLocal(t *testing.T) {
	reg, mgr, dir, cart := newCartCatalog(t)
	ai := ops.Caller{Source: ops.SrcAssistant, Session: "s1", Local: true}
	body := localJar(t, "mine", map[string]string{"ghostlib": "*"})
	addLocalFile(t, reg, "模组服", "mine-1.0.jar", body)
	// AI 就算自己带上 expectLocal 也没用：照样只出提案
	res, err := reg.Do(context.Background(), ai, "modcart.install", ops.ArgsOf(map[string]any{"inst": "模组服", "expectLocal": localSHAs(cart, "模组服")}))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := res.(*ops.Proposal)
	if !ok {
		t.Fatalf("AI 发起的安装应生成提案：%T", res)
	}
	if len(p.Plan.Changes) != 1 || !strings.Contains(p.Plan.Changes[0].What, "mine-1.0.jar") || !strings.Contains(p.Plan.Changes[0].After, "mods/mine-1.0.jar") ||
		!strings.Contains(p.Plan.Changes[0].After, "本地文件") || !strings.Contains(p.Plan.Summary, "1 个用户从电脑上添加的本地文件") ||
		!strings.Contains(p.Plan.Warning, "ghostlib") {
		t.Fatalf("卡片上应列出要拷进去的本地文件和要自己找的前置：%+v %+v", p.Plan, p.Plan.Changes)
	}
	if strings.Contains(string(p.Args), "expectLocal") {
		t.Errorf("AI 给的 expectLocal 应被去掉：%s", p.Args)
	}
	if got := modsIn(t, dir); len(got) != 0 {
		t.Fatalf("没确认之前不该落下任何文件：%v", got)
	}
	// 预览之后人又加了一个本地文件：确认时作废
	second := addLocalFile(t, reg, "模组服", "second.jar", localJar(t, "second", nil))
	if _, err := reg.Confirm(context.Background(), cartUI, p.ID, false); ops.CodeOf(err) != 409 {
		t.Fatalf("清单里的本地文件变了，确认应报 409：%v", err)
	}
	if got := modsIn(t, dir); len(got) != 0 {
		t.Fatalf("作废的提案不该落下任何文件：%v", got)
	}
	// 确认和执行之间变了：执行时读到的和确认的那一份对不上，不装
	op := reg.Op("modcart.install")
	args := ops.ArgsOf(map[string]any{"inst": "模组服"})
	plan, err := op.Preview(context.Background(), ai, args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cartDo(t, reg, cartUI, "modcart.local.remove", map[string]any{"inst": "模组服", "sha1": second}); err != nil {
		t.Fatal(err)
	}
	if _, err := op.Run(ops.WithConfirmedPre(context.Background(), plan.Pre), ai, args); ops.CodeOf(err) != 409 {
		t.Fatalf("确认之后本地文件变了，执行应报 409：%v", err)
	}
	// 同一个名字换了内容（移除再加一份不一样的）：指纹按内容算，也认得出来
	plan, _ = op.Preview(context.Background(), ai, args)
	_, _ = cartDo(t, reg, cartUI, "modcart.local.remove", map[string]any{"inst": "模组服", "sha1": localSHAs(cart, "模组服")[0]})
	addLocalFile(t, reg, "模组服", "mine-1.0.jar", localJar(t, "swapped", nil))
	if _, err := op.Run(ops.WithConfirmedPre(context.Background(), plan.Pre), ai, args); ops.CodeOf(err) != 409 {
		t.Fatalf("同名文件换了内容，执行应报 409：%v", err)
	}
	if got := modsIn(t, dir); len(got) != 0 {
		t.Fatalf("不该落下任何文件：%v", got)
	}
	// 都对得上：确认以后照常装（走提案的不用 expectLocal）
	res, err = reg.Do(context.Background(), ai, "modcart.install", args)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Confirm(context.Background(), cartUI, res.(*ops.Proposal).ID, false)
	if err != nil {
		t.Fatalf("确认：%v", err)
	}
	if s := waitTask(t, mgr, out.(map[string]string)["taskId"]); s.Err != "" {
		t.Fatalf("安装任务：%+v", s)
	}
	if got := strings.Join(modsIn(t, dir), " "); got != "mine-1.0.jar" {
		t.Errorf("装上的应正好是卡片上那一个文件：%s", got)
	}
}

// TestModCartInstallLocalCollisions 目标位置已有同名文件：内容相同跳过，内容不同不覆盖（任务提示里说明，文件留在清单里）；
// 服务器里已经有一模一样的文件（换了名字）也跳过，不拷出第二份。和 Modrinth 项目、自动补的前置一起装。
func TestModCartInstallLocalCollisions(t *testing.T) {
	reg, mgr, dir, cart := newCartCatalog(t)
	mods := filepath.Join(dir, "mods")
	sameBody, clashBody, dupBody := localJar(t, "same", nil), localJar(t, "clash", nil), localJar(t, "dup", nil)
	if err := os.WriteFile(filepath.Join(mods, "same.jar"), sameBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mods, "clash.jar"), []byte("服务器上原来那一份"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mods, "dup-on-server.jar"), dupBody, 0o644); err != nil {
		t.Fatal(err)
	}
	addLocalFile(t, reg, "模组服", "same.jar", sameBody)
	clash := addLocalFile(t, reg, "模组服", "clash.jar", clashBody)
	addLocalFile(t, reg, "模组服", "dup-renamed.jar", dupBody)
	addLocalFile(t, reg, "模组服", "fresh.jar", localJar(t, "fresh", map[string]string{"nowhere": "*"}))
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}}); err != nil {
		t.Fatal(err)
	}
	// 检查前置：本地文件逐个列出，要自己找的前置单独列
	d, err := cartDo(t, reg, cartUI, "modcart.deps", map[string]any{"inst": "模组服"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d["local"].([]any)) != 4 || len(d["manual"].([]any)) != 1 || !strings.Contains(string(mustJSON(d["manual"])), "nowhere") ||
		len(d["files"].([]any)) != 2 || !strings.Contains(string(mustJSON(d["conflicts"])), "clash.jar") {
		t.Fatalf("检查前置：%s", mustJSON(d))
	}
	m, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服", "expect": cartIDs(cart, "模组服"), "expectLocal": localSHAs(cart, "模组服")})
	if err != nil {
		t.Fatal(err)
	}
	s := waitTask(t, mgr, m["taskId"].(string))
	if s.Err != "" {
		t.Fatalf("安装任务：%+v", s)
	}
	kinds := map[string]string{}
	for _, w := range s.Warnings {
		kinds[w.Kind] = fmt.Sprint(w.Items)
	}
	if !strings.Contains(kinds["local-not-overwritten"], "clash.jar") || !strings.Contains(kinds["manual-deps"], "nowhere") || !strings.Contains(kinds["conflicts"], "clash.jar") {
		t.Fatalf("任务的提示：%+v", s.Warnings)
	}
	if b, _ := os.ReadFile(filepath.Join(mods, "clash.jar")); string(b) != "服务器上原来那一份" {
		t.Fatal("同名不同内容的文件被覆盖了")
	}
	got := modsIn(t, dir)
	slices.Sort(got)
	if strings.Join(got, " ") != "alpha-1.0.jar clash.jar dup-on-server.jar fabric-api-1.0.jar fresh.jar same.jar" {
		t.Fatalf("mods 里的文件：%v", got)
	}
	// 没覆盖的那个留在清单里，其余的（装上的、跳过的）都拿掉了；Modrinth 项目也装上拿掉了
	if left := localSHAs(cart, "模组服"); len(left) != 1 || left[0] != clash {
		t.Fatalf("清单里应只剩没覆盖的那个：%v", left)
	}
	if len(cart.List("模组服")) != 0 {
		t.Fatalf("装上的 Modrinth 项目应拿掉：%v", cartIDs(cart, "模组服"))
	}
	if n := countFiles(filepath.Join(app.Base, "data", "modcart", "local")); n != 1 {
		t.Fatalf("暂存目录里应只剩 1 个文件：%d", n)
	}
	log := strings.Join(s.Log, "\n")
	for _, want := range []string{"一模一样的文件（dup-on-server.jar），跳过", "没有覆盖", "已放进 mods", "其中 1 个是从电脑上添加的本地文件"} {
		if !strings.Contains(log, want) {
			t.Errorf("任务日志里应有「%s」：\n%s", want, log)
		}
	}
}

// TestInstanceDeleteClearsStagedLocal 删除服务器时连它暂存的本地文件一起清掉。
// 这次只有本地文件要放、而且一个都没放进去（服务器里有同名但内容不同的文件）：任务算失败，不写「安装完成」；
// 文件留在清单里，服务器上原来那一份没被动。
func TestModCartInstallOnlyLocalAllKept(t *testing.T) {
	reg, mgr, dir, cart := newCartCatalog(t)
	mods := filepath.Join(dir, "mods")
	if err := os.WriteFile(filepath.Join(mods, "clash.jar"), []byte("服务器上原来那一份"), 0o644); err != nil {
		t.Fatal(err)
	}
	clash := addLocalFile(t, reg, "模组服", "clash.jar", localJar(t, "clash", nil))
	m, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服", "expect": []string{}, "expectLocal": localSHAs(cart, "模组服")})
	if err != nil {
		t.Fatal(err)
	}
	s := waitTask(t, mgr, m["taskId"].(string))
	if s.Err == "" || !strings.Contains(s.Err, "一个都没放进") {
		t.Fatalf("一个都没放进去应当算失败：err=%q log=%v", s.Err, s.Log)
	}
	kinds := map[string]bool{}
	for _, w := range s.Warnings {
		kinds[w.Kind] = true
	}
	if !kinds["local-not-overwritten"] {
		t.Fatalf("失败时原因仍应列在提示里：%+v", s.Warnings)
	}
	if b, _ := os.ReadFile(filepath.Join(mods, "clash.jar")); string(b) != "服务器上原来那一份" {
		t.Fatal("同名不同内容的文件被覆盖了")
	}
	if left := localSHAs(cart, "模组服"); len(left) != 1 || left[0] != clash {
		t.Fatalf("没放进去的文件应留在清单里：%v", left)
	}
}

func TestInstanceDeleteClearsStagedLocal(t *testing.T) {
	reg, _, _, cart := newCartCatalog(t)
	addLocalFile(t, reg, "模组服", "mine.jar", localJar(t, "mine", nil))
	keep := addLocalFile(t, reg, "", "draft.jar", localJar(t, "draft", nil))
	staged := filepath.Join(app.Base, "data", "modcart", "local")
	if n := countFiles(staged); n != 2 {
		t.Fatalf("暂存目录里应有 2 个文件：%d", n)
	}
	if _, err := reg.Do(context.Background(), cartUI, "instance.delete", ops.ArgsOf(map[string]any{"inst": "模组服", "files": false})); err != nil {
		t.Fatalf("删除：%v", err)
	}
	if len(cart.Locals("模组服")) != 0 || countFiles(staged) != 1 {
		t.Errorf("删掉服务器以后它暂存的本地文件应清掉：还登记着 %d 个，暂存目录里 %d 个文件", len(cart.Locals("模组服")), countFiles(staged))
	}
	if got := localSHAs(cart, modcart.Draft); len(got) != 1 || got[0] != keep {
		t.Errorf("草稿清单不该受影响：%v", got)
	}
}

// TestModCartSearchParams 热门清单：关键词留空、类别按白名单、offset 有上下限，单次 20 条以内。
func TestModCartSearchParams(t *testing.T) {
	reg, _, _, _ := newCartCatalog(t)
	m, err := cartDo(t, reg, cartUI, "modcart.search", map[string]any{"core": "fabric", "mc": "1.20.1", "category": "optimization", "offset": 20})
	if err != nil || m["hits"] == nil || m["more"] == nil || m["total"] == nil || m["offset"].(float64) != 20 {
		t.Fatalf("热门清单：%v %v", m, err)
	}
	for name, args := range map[string]map[string]any{
		"类别":          {"core": "fabric", "mc": "1.20.1", "category": "nsfw"},
		"没有「economy」": {"core": "fabric", "mc": "1.20.1", "category": "economy"}, // 插件才有的类别
		"没有「magic」":   {"core": "paper", "category": "magic"},                    // 模组才有的类别
		"offset":      {"core": "fabric", "mc": "1.20.1", "offset": 100},
		"参数 offset":   {"core": "fabric", "mc": "1.20.1", "offset": -1},
	} {
		if _, err := cartDo(t, reg, cartUI, "modcart.search", args); ops.CodeOf(err) != 400 {
			t.Errorf("%s：应报 400：%v", name, err)
		}
	}
	if _, err := cartDo(t, reg, cartUI, "modcart.search", map[string]any{"core": "paper", "category": "economy"}); err != nil {
		t.Errorf("插件的类别：%v", err)
	}
	// 没有本地文件时 deps 的 local、manual 也是 []，清单的 local 也是 []
	d, _ := cartDo(t, reg, cartUI, "modcart.deps", map[string]any{"core": "fabric", "mc": "1.20.1"})
	l, _ := cartDo(t, reg, cartUI, "modcart.list", map[string]any{})
	for _, js := range []string{string(mustJSON(d)), string(mustJSON(l))} {
		if strings.Contains(js, "null") || !strings.Contains(js, `"local":[]`) {
			t.Errorf("空的列表应是 []：%s", js)
		}
	}
}
