package modcart

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectPageURL(t *testing.T) {
	if got := ProjectPageURL("mod", "sodium"); got != "https://modrinth.com/mod/sodium" {
		t.Fatalf("模组：%s", got)
	}
	if got := ProjectPageURL("plugin", "essentialsx"); got != "https://modrinth.com/plugin/essentialsx" {
		t.Fatalf("插件：%s", got)
	}
	for _, pair := range [][2]string{
		{"", "sodium"},
		{"mod", ""},
		{"modpack", "my pack"},
		{"website", "sodium"},
		{"mod", ".."},
		{"mod", "javascript:alert(1)"},
		{"mod", "a/b"},
	} {
		if got := ProjectPageURL(pair[0], pair[1]); got != "" {
			t.Fatalf("%q %q 不该有网址：%s", pair[0], pair[1], got)
		}
	}
}

func TestSearchProjectURLDoesNotSynthesizeMissingType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_hits": 3,
			"hits": []map[string]any{
				{"project_id": "p1", "slug": "lithium", "project_type": "mod", "title": "Lithium"},
				{"project_id": "p2", "slug": "no-type", "title": "No Type"},
				{"project_id": "p3", "slug": "../bad", "project_type": "mod", "title": "Bad Slug"},
			},
		})
	}))
	defer srv.Close()

	svc := New(t.TempDir(), srv.URL)
	res, err := svc.Search(context.Background(), SearchQuery{Type: "mod", Loaders: []string{"fabric"}, MC: "1.21.1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 3 {
		t.Fatalf("hits = %d", len(res.Hits))
	}
	if res.Hits[0].ProjectURL != "https://modrinth.com/mod/lithium" {
		t.Fatalf("合法项应有项目页：%q", res.Hits[0].ProjectURL)
	}
	if res.Hits[1].ProjectType != "" || res.Hits[1].ProjectURL != "" {
		t.Fatalf("接口没返回 project_type 时不应补填或编网址：%+v", res.Hits[1])
	}
	if res.Hits[2].ProjectURL != "" {
		t.Fatalf("非法 slug 不应生成网址：%+v", res.Hits[2])
	}
}
