package web

// beta.3 第一层：可用性走查整改 P0（R02）。

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

// 导入整合包表单上的白名单开关要一路带到操作 instance.import：表单是一项一项抄的，漏抄就会悄悄按关导入。
func TestImportFormArgsCarriesWhitelist(t *testing.T) {
	for _, v := range []string{"true", "false", ""} {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("name", "整合包服")
		if v != "" {
			_ = mw.WriteField("whitelist", v)
		}
		_ = mw.Close()
		r := httptest.NewRequest("POST", "/api/import/modpack", &body)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		args := importFormArgs(r, "C:\\cache\\import\\x.zip")
		if got, want := args["whitelist"], v == "true"; got != want {
			t.Fatalf("表单 whitelist=%q：参数里是 %v，应为 %v", v, got, want)
		}
		if args["name"] != "整合包服" || args["file"] != "C:\\cache\\import\\x.zip" {
			t.Fatalf("别的项：%v", args)
		}
	}
}
