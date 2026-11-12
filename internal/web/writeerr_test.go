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
