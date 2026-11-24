package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"minedeck/internal/ai"
	"minedeck/internal/ops"
)

// 报错链上有错误另给了一句人话（Hint()）时，响应里另放 hint；error 照旧是完整原文。没有就不带 hint。
func TestWriteErrHint(t *testing.T) {
	ae := &ai.APIError{Status: 402, Type: "insufficient_quota", Message: "You exceeded your current quota"}
	rec := httptest.NewRecorder()
	writeErr(rec, 502, ops.Wrap(502, fmt.Errorf("gpt-5：%w", ae)))
	var body struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 502 || body.OK || body.Error != "gpt-5："+ae.Error() || body.Hint != "账户余额不足" {
		t.Fatalf("%d %+v", rec.Code, body)
	}

	rec = httptest.NewRecorder()
	writeErr(rec, 400, errors.New("请先停止服务器再删除"))
	var plain map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &plain); err != nil {
		t.Fatal(err)
	}
	if _, ok := plain["hint"]; ok || plain["error"] != "请先停止服务器再删除" {
		t.Fatalf("没有人话时不该带 hint：%v", plain)
	}

	// 认不出的状态码：Hint() 是空串，也不带 hint
	rec = httptest.NewRecorder()
	writeErr(rec, 502, ops.Wrap(502, &ai.APIError{Status: 418}))
	plain = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &plain)
	if _, ok := plain["hint"]; ok {
		t.Fatalf("空的 Hint 不该带出去：%v", plain)
	}
}

// platformCreatedErr 报错链上说「平台上已经建好」的错误（同 tunnel 账户层创建隧道时那种，那个类型不导出）。
type platformCreatedErr struct{ yes bool }

func (e platformCreatedErr) Error() string {
	return "平台上的隧道已经建好，但没能导入 MineDeck"
}
func (e platformCreatedErr) PlatformCreated() bool { return e.yes }

// 报错链上有错误说平台上已经建好了（PlatformCreated() 为 true）时，响应里加 created: true，界面据此锁住「创建并导入」（A57）；
// 没有这个方法、或者它说 false 时不带。别的字段照旧。
func TestWriteErrPlatformCreated(t *testing.T) {
	read := func(err error) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		writeErr(rec, 400, ops.Wrap(400, fmt.Errorf("创建：%w", err)))
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	body := read(platformCreatedErr{yes: true})
	if body["created"] != true || body["ok"] != false || body["error"] != "创建：平台上的隧道已经建好，但没能导入 MineDeck" {
		t.Fatalf("平台上已建好时应带 created: true：%v", body)
	}
	for _, err := range []error{platformCreatedErr{yes: false}, errors.New("平台拒绝创建")} {
		if body := read(err); body["created"] != nil {
			t.Fatalf("没说平台上已建好时不该带 created：%v", body)
		}
	}
}
