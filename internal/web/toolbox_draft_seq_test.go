package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"minedeck/internal/toolbox"
)

// A65：HTTP 接口 PUT /api/toolbox/mrpack/draft 支持通过请求头 X-Draft-Client / X-Draft-Seq
// （或 JSON 体里的 clientId / seq）按字段过滤乱序晚到的旧序号请求，同时保持正常 JSON 请求体原样。
func TestToolboxDraftSeqHTTP(t *testing.T) {
	e := newFullEnv(t)
	send := func(body string, cid, seq string) toolbox.DraftConfig {
		t.Helper()
		var opts []reqOpt
		if cid != "" {
			opts = append(opts, func(r *http.Request) {
				r.Header.Set("X-Draft-Client", cid)
				r.Header.Set("X-Draft-Seq", seq)
			})
		}
		rec := e.call(t, "PUT", "/api/toolbox/mrpack/draft", body, http.StatusOK, opts...)
		var resp struct {
			Data toolbox.DraftConfig `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.Data
	}

	got := send(`{"name":"较新的名称"}`, "tab-a", "2")
	if got.Name != "较新的名称" {
		t.Fatalf("seq=2 后 Name = %q", got.Name)
	}
	got = send(`{"name":"较早发出的名称","summary":"补充简介"}`, "tab-a", "1")
	if got.Name != "较新的名称" || got.Summary != "补充简介" {
		t.Fatalf("乱序 seq=1 后应保留较新 Name 并保存未更新过的 Summary：%+v", got)
	}
}
