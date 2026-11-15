package inst

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/atomicfile"
	"minedeck/internal/dl"
	"minedeck/internal/modpack"
)

// stallTransport 信任下载卡在半路：请求到了就关上 started，release 关上之前不交内容（请求被取消就退出）。
type stallTransport struct {
	content []byte
	started chan struct{}
	release chan struct{}
	once    *sync.Once
}

func (s stallTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	s.once.Do(func() { close(s.started) })
	select {
	case <-s.release:
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(s.content)),
		Header:     http.Header{"Content-Length": []string{strconv.Itoa(len(s.content))}},
	}, nil
}

// 处置没装上的文件时，trust 下载不拿配置锁和设置锁（2026-11-14 合并后复审发现 8）：下载卡着的时候，保存 server.properties
// 和改配置文件都不被挡，服务器文件夹里也不落任何东西。下载完写进去的那一下才拿锁，并在锁里把落点重新查一遍：
// 下载期间世界名被改到 mods，mods/a.jar 就成了「当前世界里的文件」，照样拒绝。
func TestPendingTrustDownloadDoesNotHoldSettingsLocks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level string // 下载卡着时保存设置顺手把 level-name 改成这个；空的不改
		ok    bool
	}{
		{"下载完照常写入", "", true},
		{"下载期间世界改到了 mods：写入前在锁里重查，照样拒绝", "mods", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, dir := importEnv(t)
			task := newImportTask(m, "测试服", dir)
			writeTree(t, dir, map[string]string{"server.properties": "level-name=world\nmotd=old\n", "world/level.dat": "old"})
			body := []byte("pending-jar")
			const url = "https://93.184.216.34/a.jar"
			m.finishImport(task, CreateReq{Name: "测试服"}, dir, []modpack.PendingFile{{Path: "mods/a.jar", Reason: modpack.ReasonHost, SHA1: sha1Of(t, body), URLs: []string{url}}}, nil)
			st := stallTransport{content: body, started: make(chan struct{}), release: make(chan struct{}), once: &sync.Once{}}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(st.release) }) }
			t.Cleanup(release)
			old := modpackHTTP
			modpackHTTP = func() *http.Client { return dl.GuardedClientWithTransport(st) }
			t.Cleanup(func() { modpackHTTP = old })

			type result struct {
				out  []ResolveOutcome
				left int
				err  error
			}
			done := make(chan result, 1)
			go func() {
				out, left, err := m.ResolveImportPending(context.Background(), task.ID(), []PendingAction{{ID: "p1", Act: "trust", URL: url, Ack: true}})
				done <- result{out, len(left), err}
			}()
			select {
			case <-st.started:
			case <-time.After(10 * time.Second):
				t.Fatal("信任下载没有开始")
			}

			// 下载卡着：保存设置（同 instance.props.set：先登记，再按 server.properties 排队读改写）不被挡
			i, err := m.Get("测试服")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			use, err := m.UseWorldInstance(i, "保存设置")
			if err != nil {
				t.Fatalf("下载期间登记「保存设置」被拒：%v", err)
			}
			err = UpdatePropsContext(ctx, i.PropsPath(), func(p *Props) error {
				p.Set("motd", "new")
				if tc.level != "" {
					p.Set("level-name", tc.level)
				}
				return nil
			})
			use()
			if err != nil {
				t.Fatalf("下载期间保存 server.properties 被挡住了：%v", err)
			}
			// 改配置文件（instance.config.write 拿的是同一把配置锁）也不被挡
			unlock, err := LockKeyContext(ctx, "config|测试服")
			if err != nil {
				t.Fatalf("下载期间改配置文件被挡住了：%v", err)
			}
			unlock()
			// 下载在缓存文件夹里，服务器文件夹里什么都还没写
			if ents, _ := os.ReadDir(filepath.Join(dir, "mods")); len(ents) != 0 {
				t.Fatalf("下载期间服务器的 mods 里有了东西：%v", ents)
			}

			release()
			var r result
			select {
			case r = <-done:
			case <-time.After(15 * time.Second):
				t.Fatal("放开下载以后处置没有结束")
			}
			if r.err != nil || len(r.out) != 1 {
				t.Fatalf("处置：%+v %v", r.out, r.err)
			}
			got, readErr := os.ReadFile(filepath.Join(dir, "mods", "a.jar"))
			trusted := app.TrustedHostFunc(app.TrustScopeModpack)("93.184.216.34")
			if tc.ok {
				if !r.out[0].OK || readErr != nil || !bytes.Equal(got, body) || r.left != 0 || !trusted {
					t.Fatalf("下载完应照常写入：%+v 文件=%q %v 剩 %d 项 站点已信任=%v", r.out[0], got, readErr, r.left, trusted)
				}
			} else {
				if r.out[0].OK || !strings.Contains(r.out[0].Note, "当前世界") || readErr == nil || r.left != 1 || trusted {
					t.Fatalf("落点不合规应被拒、不写入、留在清单里、不记信任：%+v 文件=%q 剩 %d 项 站点已信任=%v", r.out[0], got, r.left, trusted)
				}
			}
			// 下载期间的那次保存留着，没有被处置盖掉
			if b, _ := os.ReadFile(filepath.Join(dir, "server.properties")); !strings.Contains(string(b), "motd=new") {
				t.Fatalf("下载期间保存的设置丢了：%s", b)
			}
			// 缓存里的临时下载清掉了，服务器文件夹里也没有留下临时文件
			if ents, _ := os.ReadDir(filepath.Join(app.CacheDir, "pending-download")); len(ents) != 0 {
				t.Fatalf("临时下载没清掉：%v", ents)
			}
			ents, _ := os.ReadDir(filepath.Join(dir, "mods"))
			for _, e := range ents {
				if e.Name() != "a.jar" {
					t.Fatalf("服务器文件夹里留下了临时文件：%s", e.Name())
				}
			}
		})
	}
}

// 反过来：真正写进服务器文件夹的那一下，配置锁和 server.properties 的路径锁都拿着（local、trust 一样），
// 这时保存设置、改配置文件要等它写完——落点是在这两把锁里查的，查完到写完之间世界名不能变。
func TestPendingWriteHoldsSettingsLocks(t *testing.T) {
	for _, act := range []string{"local", "trust"} {
		t.Run(act, func(t *testing.T) {
			m, dir := importEnv(t)
			task := newImportTask(m, "测试服", dir)
			writeTree(t, dir, map[string]string{"server.properties": "level-name=world\n"})
			body := []byte("jar")
			m.finishImport(task, CreateReq{Name: "测试服"}, dir, []modpack.PendingFile{{Path: "mods/a.jar", Reason: modpack.ReasonHost, SHA1: sha1Of(t, body), URLs: []string{"https://93.184.216.34/a"}}}, nil)
			src := filepath.Join(t.TempDir(), "a.jar")
			if err := os.WriteFile(src, body, 0o644); err != nil {
				t.Fatal(err)
			}
			old := modpackHTTP
			modpackHTTP = func() *http.Client { return dl.GuardedClientWithTransport(fakeTransport{content: body}) }
			t.Cleanup(func() { modpackHTTP = old })
			entered, proceed := make(chan struct{}), make(chan struct{})
			var once sync.Once
			resume := func() { once.Do(func() { close(proceed) }) }
			t.Cleanup(resume)
			atomicfile.Hook = func(path string) {
				if strings.EqualFold(path, filepath.Join(dir, "mods", "a.jar")) {
					close(entered)
					<-proceed
				}
			}
			t.Cleanup(func() { atomicfile.Hook = nil })
			done := make(chan error, 1)
			go func() {
				out, _, err := m.ResolveImportPending(context.Background(), task.ID(), []PendingAction{{ID: "p1", Act: act, Path: src, URL: "https://93.184.216.34/a", Ack: true}})
				if err == nil && (len(out) != 1 || !out[0].OK) {
					err = fmt.Errorf("补装失败: %+v", out)
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("没有进入实际写入")
			}
			i, _ := m.Get("测试服")
			// 两把锁各用各的期限：共用一个的话，前一个等到期限过了，后一个拿锁时一上来就因为期限已过而失败，测不到锁
			ctxP, cancelP := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancelP()
			if err := UpdatePropsContext(ctxP, i.PropsPath(), func(*Props) error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("写入的那一下应拿着 server.properties 的锁：%v", err)
			}
			ctxC, cancelC := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancelC()
			if unlock, err := LockKeyContext(ctxC, "config|测试服"); !errors.Is(err, context.DeadlineExceeded) {
				if err == nil {
					unlock()
				}
				t.Fatalf("写入的那一下应拿着配置锁：%v", err)
			}
			resume()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			atomicfile.Hook = nil
			// 写完以后两把锁都放了
			ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel2()
			if err := UpdatePropsContext(ctx2, i.PropsPath(), func(*Props) error { return nil }); err != nil {
				t.Fatalf("写完以后设置锁没放：%v", err)
			}
		})
	}
}
