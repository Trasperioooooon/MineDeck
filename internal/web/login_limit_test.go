package web

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// 登录请求体有上限：没登录的人发一个很大的 JSON，不会被整份读进来（v3.1.0 正式版前检查 B-1）。
func TestLoginBodyLimit(t *testing.T) {
	e := newFullEnv(t)
	big := `{"pad":"` + strings.Repeat("x", 64<<10) + `","password":"x"}`
	e.call(t, "POST", "/api/login", big, http.StatusRequestEntityTooLarge, fromLAN, noToken)
	e.call(t, "POST", "/api/login", `{"password":"`+lanPass+`"}`, 200, fromLAN, noToken)
}

// 同一个来源同时只校验一个登录：第一个还在校验（猜错要等 0.7 秒）时，第二个直接回 429；
// 别的来源不受影响；第一个结束以后，同一个来源又能登录（B-1）。
func TestLoginOneAtATimePerSource(t *testing.T) {
	e := newFullEnv(t)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.call(t, "POST", "/api/login", `{"password":"wrong"}`, 401, fromLAN, noToken)
	}()
	time.Sleep(200 * time.Millisecond) // 让第一个先进到校验里
	e.call(t, "POST", "/api/login", `{"password":"wrong"}`, http.StatusTooManyRequests, fromLAN, noToken)
	other := func(r *http.Request) { r.RemoteAddr = "192.168.1.99:52001" }
	e.call(t, "POST", "/api/login", `{"password":"`+lanPass+`"}`, 200, other, noToken)
	wg.Wait()
	e.call(t, "POST", "/api/login", `{"password":"`+lanPass+`"}`, 200, fromLAN, noToken)
}

// 全部来源加起来同时最多校验 loginMaxConcurrent 个：换着地址来也占不满 CPU。
func TestLoginGlobalLimit(t *testing.T) {
	var releases []func()
	for i := 0; i < loginMaxConcurrent; i++ {
		r, _ := http.NewRequest("POST", "/api/login", nil)
		r.RemoteAddr = "192.168.7." + string(rune('a'+i)) + ":1"
		rel, ok := acquireLogin(r)
		if !ok {
			t.Fatalf("第 %d 个来源应能占到名额", i+1)
		}
		releases = append(releases, rel)
	}
	r, _ := http.NewRequest("POST", "/api/login", nil)
	r.RemoteAddr = "192.168.7.200:1"
	if _, ok := acquireLogin(r); ok {
		t.Fatal("名额满了还能占")
	}
	for _, rel := range releases {
		rel()
	}
	if rel, ok := acquireLogin(r); !ok {
		t.Fatal("放掉以后应能再占")
	} else {
		rel()
	}
}
