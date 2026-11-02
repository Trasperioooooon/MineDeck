package companion

import (
	"net/http"
	"time"
)

// 给外部测试包（companion_test，它要和 companiontest 一起用）开的几个口子。只在测试里编译。

// WithOptions 在 o 上依次套用 mods。
func WithOptions(o Options, mods ...func(*Options)) Options {
	for _, m := range mods {
		m(&o)
	}
	return o
}

// FastRetry 把重试的间隔缩短（快速重试 20 毫秒、持续 2 秒，之后 50 毫秒）。
func FastRetry() func(*Options) {
	return func(o *Options) {
		SetRetry(o, 20*time.Millisecond, 2*time.Second, 50*time.Millisecond, 50*time.Millisecond)
	}
}

// SetRetry 改重试的几个间隔。
func SetRetry(o *Options, quick, window, slow, busy time.Duration) {
	o.quick, o.quickWindow, o.slow, o.busy = quick, window, slow, busy
}

// SetHeartbeat 改 PING 的间隔和多久收不到数据算断线。
func SetHeartbeat(o *Options, ping, idle time.Duration) { o.pingEvery, o.readIdle = ping, idle }

// SetRequestTimeout 改等回复的时间。
func SetRequestTimeout(o *Options, d time.Duration) { o.request = d }

// SetClientOptions Service 起客户端时改它的参数。
func SetClientOptions(d *Deps, f func(*Options)) { d.clientOpts = f }

// SetTick Service 等服务器开好时多久看一次状态。
func SetTick(d *Deps, t time.Duration) { d.tick = t }

// Client Service 现在用的 HTTP 客户端（测试确认没配的时候用的是 dl.Client）。
func (s *Service) HTTPClient() *http.Client { return s.client() }
