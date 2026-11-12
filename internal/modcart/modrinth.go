package modcart

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"minedeck/internal/dl"
)

// ---- Modrinth 的接口（公开，不需要 Key）----
//
// 用到的：/search 搜索（关键词留空 + 按下载量排 = 热门清单；offset 翻页，类别走 facets）、/projects?ids=[…] 批量查项目（ID、slug 都认，查不到的直接略过）、
// /project/{id}/version 按加载器和 MC 版本列版本、/version/{id} 查单个版本、
// POST /version_files 按 SHA-1 认出服务器目录里已有的 jar 属于哪个项目。
// 字段名按 2026-11 实际返回的核对过（测试里的假服务照这个形状回）。

// mrProject /projects 返回的项目里用到的字段。
type mrProject struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	IconURL     string `json:"icon_url"`
	ProjectType string `json:"project_type"` // mod / plugin / modpack …
	ServerSide  string `json:"server_side"`  // required / optional / unsupported / unknown
	License     struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"license"`
}

type projectSet []mrProject

// find 按项目 ID 或 slug（不分大小写）找。
func (ps projectSet) find(id string) (mrProject, bool) {
	for _, p := range ps {
		if p.ID == id || strings.EqualFold(p.Slug, id) {
			return p, true
		}
	}
	return mrProject{}, false
}

// mrVersion 版本里用到的字段。
type mrVersion struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	VersionNumber string   `json:"version_number"`
	VersionType   string   `json:"version_type"` // release / beta / alpha
	DatePublished string   `json:"date_published"`
	Loaders       []string `json:"loaders"`
	GameVersions  []string `json:"game_versions"`
	Files         []mrFile `json:"files"`
	Dependencies  []mrDep  `json:"dependencies"`
}

type mrFile struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
	Size     int64  `json:"size"`
	Hashes   struct {
		SHA1 string `json:"sha1"`
	} `json:"hashes"`
}

// mrDep 版本的依赖。version_id、project_id、file_name 都可能是 null（解成空串）。
type mrDep struct {
	VersionID      string `json:"version_id"`
	ProjectID      string `json:"project_id"`
	FileName       string `json:"file_name"`
	DependencyType string `json:"dependency_type"` // required / optional / incompatible / embedded
}

// Hit 搜索结果的一项（字段名同 Modrinth，界面和资源页一样直接用）。
type Hit struct {
	ProjectID   string `json:"project_id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Downloads   int64  `json:"downloads"`
	IconURL     string `json:"icon_url"`
	License     string `json:"license"`               // 搜索结果里是许可证的 ID（字符串），项目详情里才是对象
	ServerSide  string `json:"server_side,omitempty"` // 只用来筛掉只在客户端用的模组
}

// getJSON GET s.api+path，解进 out。404 报成 404，其余失败报成 502（上游的问题）。网络错误重试一次。
func (s *Service) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	u := s.api + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return s.doJSON(ctx, http.MethodGet, u, nil, out)
}

// postJSON POST 一个 JSON 请求体。
func (s *Service) postJSON(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return s.doJSON(ctx, http.MethodPost, s.api+path, b, out)
}

func (s *Service) doJSON(ctx context.Context, method, u string, body []byte, out any) error {
	var last error
	for try := 0; try < 2; try++ {
		if try > 0 { // 429、5xx 多半一会儿就好：等一下再试，别马上又撞上
			select {
			case <-ctx.Done():
				return ctxErr(ctx)
			case <-time.After(retryWait):
			}
		}
		code, b, err := s.roundTrip(ctx, method, u, body)
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return ctxErr(ctx)
			}
			last = errorf(502, "连不上 Modrinth：%v", err)
			continue
		case code == http.StatusNotFound:
			return errorf(404, "Modrinth 上没有这个（HTTP 404）")
		case code == http.StatusTooManyRequests || code >= 500:
			last = errorf(502, "Modrinth 暂时不可用（HTTP %d），稍后再试", code)
			continue
		case code != http.StatusOK:
			return errorf(502, "Modrinth 返回了 HTTP %d", code)
		}
		if err := json.Unmarshal(b, out); err != nil {
			return errorf(502, "Modrinth 的回复看不懂：%v", err)
		}
		return nil
	}
	return last
}

// ctxErr 调用方的 ctx 结束了：取消（比如任务被停）原样返回，不算 Modrinth 的问题；
// 到时间了（操作自己给的时限）是 Modrinth 太慢，报 502。
func ctxErr(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}
	return errorf(502, "连不上 Modrinth：等了太久没有回应")
}

// retryWait 重试前等多久（测试里改短）。
var retryWait = 1500 * time.Millisecond

// validID 能不能拼进接口路径：项目 ID、slug、版本 ID 都只有字母数字和 - _ .，不会是 . 或 ..。
func validID(id string) bool {
	if id == "" || id == "." || id == ".." || len(id) > 100 {
		return false
	}
	return !strings.ContainsFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.')
	})
}

func (s *Service) roundTrip(ctx context.Context, method, u string, body []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("User-Agent", dl.UA)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := dl.Client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, b, err
}

func jsonList(ss []string) string {
	b, _ := json.Marshal(ss)
	return string(b)
}

// projects 批量查项目（每次最多 100 个）。查不到的不在结果里。
func (s *Service) projects(ctx context.Context, ids []string) (projectSet, error) {
	var out projectSet
	for len(ids) > 0 {
		n := min(len(ids), 100)
		var ps []mrProject
		if err := s.getJSON(ctx, "/projects", url.Values{"ids": {jsonList(ids[:n])}}, &ps); err != nil {
			return nil, err
		}
		out = append(out, ps...)
		ids = ids[n:]
	}
	return out, nil
}

// PageSize 搜索一页几条；MaxOffset 最多从第几条开始取（第 5 页）：热门清单往后翻到这里为止，再往后的该用关键词找了。
const (
	PageSize  = 20
	MaxOffset = 80
)

// 类别筛选的白名单（Modrinth 的 category slug，2026-11 对照 /v2/tag/category 核对过：Modrinth 上插件和模组共用 mod 这一套类别）。
// 界面上的筛选片按这个顺序排；别的 slug 一律不认，不拿调用方给的字符串去拼 facets。
var (
	modCategories    = []string{"optimization", "utility", "management", "worldgen", "adventure", "technology", "magic", "mobs", "equipment", "storage", "library"}
	pluginCategories = []string{"management", "utility", "optimization", "economy", "social", "minigame", "game-mechanics", "worldgen", "library"}
)

// Categories 这种项目类型（mod / plugin）能按哪些类别筛。
func Categories(rtype string) []string {
	if rtype == "plugin" {
		return slices.Clone(pluginCategories)
	}
	return slices.Clone(modCategories)
}

// AllCategories 两种类型的类别合在一起（去重），给参数说明用。
func AllCategories() []string {
	out := slices.Clone(modCategories)
	for _, c := range pluginCategories {
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

// SearchQuery 一次搜索。Q 留空就是「最热门的」：结果始终按下载量排。
type SearchQuery struct {
	Q        string
	Type     string   // mod / plugin
	Loaders  []string // Modrinth 的加载器
	MC       string   // 空串不按版本过滤（插件兼容面广，现有的资源页也不卡版本）
	Category string   // 空串不按类别筛；否则必须在 Categories(Type) 里
	Offset   int      // 从第几条开始，0 到 MaxOffset；按 PageSize 对齐（Modrinth 自己也是按页取的）
}

// SearchResult 一页结果。Hits 最多 PageSize 条，不会是 nil。
type SearchResult struct {
	Hits   []Hit `json:"hits"`
	Offset int   `json:"offset"` // 这一页实际从第几条开始
	Total  int   `json:"total"`  // Modrinth 上一共有多少条符合
	More   bool  `json:"more"`   // 还能不能往后翻（没到头，也没到 MaxOffset）
}

// Search 在 Modrinth 搜索，按下载量排序，一页 PageSize 条。只在客户端用的模组不列出来：装到服务器上没用
// （让 Modrinth 那边就筛掉，这样每页条数是齐的、翻页不会重复或漏；回来以后再筛一遍兜底）。
func (s *Service) Search(ctx context.Context, q SearchQuery) (*SearchResult, error) {
	if q.Category != "" && !slices.Contains(Categories(q.Type), q.Category) {
		return nil, errorf(400, "没有「%s」这个类别", q.Category)
	}
	offset := min(max(q.Offset, 0), MaxOffset) / PageSize * PageSize
	loaderFacet := make([]string, 0, len(q.Loaders))
	for _, l := range q.Loaders {
		loaderFacet = append(loaderFacet, "categories:"+l)
	}
	facets := []any{[]string{"project_type:" + q.Type}, loaderFacet}
	if q.MC != "" {
		facets = append(facets, []string{"versions:" + q.MC})
	}
	if q.Type == "mod" {
		facets = append(facets, []string{"server_side!=unsupported"})
	}
	if q.Category != "" {
		facets = append(facets, []string{"categories:" + q.Category})
	}
	fb, _ := json.Marshal(facets)
	var resp struct {
		Hits      []Hit `json:"hits"`
		TotalHits int   `json:"total_hits"`
	}
	err := s.getJSON(ctx, "/search", url.Values{"query": {q.Q}, "limit": {strconv.Itoa(PageSize)}, "offset": {strconv.Itoa(offset)},
		"index": {"downloads"}, "facets": {string(fb)}}, &resp)
	if err != nil {
		return nil, err
	}
	hits := slices.DeleteFunc(resp.Hits, func(h Hit) bool { return q.Type == "mod" && h.ServerSide == "unsupported" })
	if len(hits) > PageSize {
		hits = hits[:PageSize]
	}
	for i := range hits {
		hits[i].ServerSide = ""
	}
	if hits == nil {
		hits = []Hit{}
	}
	return &SearchResult{Hits: hits, Offset: offset, Total: resp.TotalHits,
		More: len(resp.Hits) > 0 && offset+PageSize < resp.TotalHits && offset < MaxOffset}, nil
}

// versions 一个项目在这些加载器（和 MC 版本）下的全部版本。
func (s *Service) versions(ctx context.Context, project string, loaders []string, mc string) ([]mrVersion, error) {
	if !validID(project) {
		return nil, errorf(404, "项目 ID「%s」不对", project)
	}
	q := url.Values{"loaders": {jsonList(loaders)}}
	if mc != "" {
		q.Set("game_versions", jsonList([]string{mc}))
	}
	var vs []mrVersion
	err := s.getJSON(ctx, "/project/"+url.PathEscape(project)+"/version", q, &vs)
	return vs, err
}

// version 查单个版本（依赖只写了版本 ID 时用）。
func (s *Service) version(ctx context.Context, id string) (mrVersion, error) {
	var v mrVersion
	if !validID(id) {
		return v, errorf(404, "版本 ID「%s」不对", id)
	}
	err := s.getJSON(ctx, "/version/"+url.PathEscape(id), nil, &v)
	return v, err
}

// versionFiles 按 SHA-1 查文件属于哪个版本（Modrinth 上没有的不在结果里）。
func (s *Service) versionFiles(ctx context.Context, sha1s []string) (map[string]mrVersion, error) {
	out := map[string]mrVersion{}
	if len(sha1s) == 0 {
		return out, nil
	}
	err := s.postJSON(ctx, "/version_files", map[string]any{"hashes": sha1s, "algorithm": "sha1"}, &out)
	return out, err
}

// compatible 版本能不能用在这些加载器、这个 MC 版本上（mc 空串不看版本），且有文件。
func compatible(v mrVersion, loaders []string, mc string) bool {
	if len(v.Files) == 0 || !slices.ContainsFunc(v.Loaders, func(l string) bool { return slices.Contains(loaders, l) }) {
		return false
	}
	return mc == "" || slices.Contains(v.GameVersions, mc)
}

// pickVersion 挑要装的版本：加载器、MC 版本对得上、有文件；正式版优先，同一种里取发布最晚的（同 musicshare 的 pickSVC）。
func pickVersion(vs []mrVersion, loaders []string, mc string) (mrVersion, bool) {
	rank := func(t string) int {
		switch t {
		case "release":
			return 0
		case "beta":
			return 1
		}
		return 2
	}
	var best *mrVersion
	var bestAt time.Time
	for i := range vs {
		v := &vs[i]
		if !compatible(*v, loaders, mc) {
			continue
		}
		at, _ := time.Parse(time.RFC3339, v.DatePublished)
		if best == nil || rank(v.VersionType) < rank(best.VersionType) ||
			(rank(v.VersionType) == rank(best.VersionType) && at.After(bestAt)) {
			best, bestAt = v, at
		}
	}
	if best == nil {
		return mrVersion{}, false
	}
	return *best, true
}

// primaryFile 版本的主文件；没标主文件的取第一个。
func primaryFile(v mrVersion) mrFile {
	for _, f := range v.Files {
		if f.Primary {
			return f
		}
	}
	return v.Files[0]
}

// describe 「fabric 1.20.1」这样的说法，报「没有能用的版本」时用。
func describe(loaders []string, mc string) string {
	s := strings.Join(loaders, "/")
	if mc != "" {
		s += " " + mc
	}
	return s
}
