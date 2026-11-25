package tunnel

import "net/http"

// 本文件仅存放供跨包测试（internal/web）替换穿透账户 HTTP 客户端的导出钩子，
// 让路由测试能将请求导往本地 httptest 假平台而不改写全局 http.DefaultTransport。
// 所有函数仅限测试使用，以 ForTest 结尾。

// SetHTTPClientForTest 替换账户服务使用的 HTTP 客户端并返回原客户端，供测试注入独立 Transport 并在结束后还原。
func (a *Accounts) SetHTTPClientForTest(c *http.Client) *http.Client {
	prev := a.client
	a.client = c
	return prev
}
