// Package dltest 测试用：不让 dl.Client 连外网。
package dltest

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"minedeck/internal/dl"
)

// NoInternet 让 dl.Client 只连得上本机，别的地址一拨号就报错。放在包的 TestMain 里，在跑任何测试之前调。
//
// 版本清单、Java 运行时、核心这些下载都走 dl.Client。测试里只要有哪一步没有用假的地址顶替，就会真的连出去：
// 结果跟着网络好坏变，慢的时候一等几十秒，还可能真的下一个两百兆的 Java。挡在拨号这一层以后，这些地方一律
// 马上失败，走的是「没有网」那条路，哪台机器上都一样。
//
// 改的是 dl.Client 现有的那个 Transport，不是换一个新的客户端：别处要是先把 dl.Client 存了一份，也一样管得到。
func NoInternet() {
	tr := dl.Client.Transport.(*http.Transport)
	tr.Proxy = nil // 走代理的话拨的是代理的地址，下面按地址挡就不准了
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, fmt.Errorf("测试里不许连外网：%s", addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
}
