package tunnel

import (
	"strings"
	"testing"
)

// 下面的日志行都是合成的，用来锁住提取规则。不是用户贴出来的原文，也没有真实域名。
// 53981 只是反馈里提到过的端口位数；主机名用 example.com / 文档地址段。

func TestPublicAddrCandidatesSynthetic(t *testing.T) {
	const host = "synth.example.com:53981"
	cases := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "golib 调用方前缀在真正的域名前面",
			line: "2006-01-02 15:04:05.000 [I] [client/control_ext.go:37] TCP 类型隧道启动成功 使用 [" + host + "] 来连接到你的隧道",
			want: []string{host},
		},
		{
			name: "斜杠日期、没有毫秒",
			line: "2026/11/15 15:04:05 [I] [client/control_ext.go:37] 启动成功 " + host,
			want: []string{host},
		},
		{
			name: "只有文件名、带下划线",
			line: "[control_ext.go:37] 使用 " + host + " 连接",
			want: []string{host},
		},
		{
			name: "反斜杠路径",
			line: `client\control_ext.go:37 使用 ` + host + " 连接",
			want: []string{host},
		},
		{
			name: "Windows 盘符路径",
			line: `D:\src\client\control.go:168 start proxy success ` + host,
			want: []string{host},
		},
		{
			name: "正斜杠盘符路径",
			line: "D:/src/client/control.go:168 remote " + host,
			want: []string{host},
		},
		{
			name: "proxy_manager 从下划线处被切成 manager.go",
			line: "[proxy/proxy_manager.go:173] proxy added: [mc] remote " + host,
			want: []string{host},
		},
		{
			name: "没有真正地址时，源码位置不是候选",
			line: "2006-01-02 15:04:05.000 [I] [client/control.go:168] [run] start proxy success",
			want: nil,
		},
		{
			name: "方括号里的 file.go:行号",
			line: "[control.go:37] 启动成功",
			want: nil,
		},
		{
			name: "樱花原来的域名行",
			line: "2026/11/02 15:31:27 [I] [游戏] 使用 >>frp-abc.natfrp.cloud:12345<< 连接你的隧道",
			want: []string{"frp-abc.natfrp.cloud:12345"},
		},
		{
			name: "樱花行前面再加调用方前缀",
			line: "2026/11/02 15:31:27 [I] [client/control.go:100] [游戏] 使用 >>frp-abc.natfrp.cloud:12345<< 连接你的隧道",
			want: []string{"frp-abc.natfrp.cloud:12345"},
		},
		{
			name: "IPv4",
			line: "或使用节点 IP 连接（不推荐）>>203.0.113.7:12345<<",
			want: []string{"203.0.113.7:12345"},
		},
		{
			name: "内网 IPv4 仍保留",
			line: "连接 192.168.1.10:25565 和 10.1.2.3:7000",
			want: []string{"192.168.1.10:25565", "10.1.2.3:7000"},
		},
		{
			name: "IPv6 唯一本地地址仍保留",
			line: "连接 [fd00::1]:25565",
			want: []string{"[fd00::1]:25565"},
		},
		{
			name: "同一行里源码位置、域名、IPv4",
			line: "[client/control_ext.go:37] 使用 synth.example.com:11111 或 203.0.113.8:22222 连接",
			want: []string{"synth.example.com:11111", "203.0.113.8:22222"},
		},
		{
			name: "两个域名，后者留到状态机里决定",
			line: "使用 [synth-a.example.com:11111] 或 [synth-b.example.com:22222] 连接",
			want: []string{"synth-a.example.com:11111", "synth-b.example.com:22222"},
		},
		{
			name: "网址里的主机",
			line: "连接 https://synth.example.com:443/path",
			want: []string{"synth.example.com:443"},
		},
		{
			name: "多段 .go 域名",
			line: "使用 node.example.go:25565 连接",
			want: []string{"node.example.go:25565"},
		},
		{
			name: "没有路径的单段 .go 域名",
			line: "使用 example.go:25565 连接",
			want: []string{"example.go:25565"},
		},
		{
			name: "使用方括号里的单段 .go 域名",
			line: "使用 [example.go:25565] 来连接",
			want: []string{"example.go:25565"},
		},
		{
			name: "Use 方括号里的单段 .go 域名",
			line: "Use [example.go:25565] to connect",
			want: []string{"example.go:25565"},
		},
		{
			name: "下划线把主机切成后半段时整段都不要",
			line: "连接 my_node.example.com:23456",
			want: nil,
		},
		{
			name: "IPv6",
			line: "使用 [2001:db8::1]:53981 连接",
			want: []string{"[2001:db8::1]:53981"},
		},
		{
			name: "IPv6 展开写法",
			line: "remote [2001:db8:0:0:0:0:0:1]:443",
			want: []string{"[2001:db8:0:0:0:0:0:1]:443"},
		},
		{
			name: "IPv4 映射的 IPv6 不拆成里面的 IPv4",
			line: "连接 [::ffff:203.0.113.7]:23456",
			want: []string{"[::ffff:203.0.113.7]:23456"},
		},
		{
			name: "源码位置和 IPv6 在同一行",
			line: "[client/control_ext.go:37] 启动成功 [::ffff:203.0.113.9]:23456",
			want: []string{"[::ffff:203.0.113.9]:23456"},
		},
		{
			name: "端口超过 65535 不是地址",
			line: "连接 synth.example.com:65536",
			want: nil,
		},
		{
			name: "一位端口本来就不认",
			line: "连接 synth.example.com:8",
			want: nil,
		},
		{
			name: "OpenFrp 这种节点名",
			line: "启动成功 cn-sz-1.openfrp.top:23456",
			want: []string{"cn-sz-1.openfrp.top:23456"},
		},
		// 端口被正则截短（Codex 复审 beta.2 时提的）：右边紧挨着的还是数字或字母，这个候选就不要
		{
			name: "IPv6 的端口超过五位，不截成前五位",
			line: "使用 [2001:db8::1]:123456 连接",
			want: nil,
		},
		{
			name: "域名的端口后面粘着字母",
			line: "使用 host.example:12345abc 连接",
			want: nil,
		},
		{
			name: "IPv4 的端口超过五位",
			line: "连接 203.0.113.7:123456",
			want: nil,
		},
		{
			name: "截短的不要，同一行里正常的照留",
			line: "使用 host.example:12345abc 或 synth.example.com:23456 连接",
			want: []string{"synth.example.com:23456"},
		},
		{
			name: "端口后面直接接汉字照常",
			line: "地址 synth.example.com:53981连接成功",
			want: []string{"synth.example.com:53981"},
		},
		{
			name: "IPv6 后面直接接汉字照常",
			line: "使用 [2001:db8::1]:53981连接",
			want: []string{"[2001:db8::1]:53981"},
		},
		{
			name: "端口后面是英文句号",
			line: "remote synth.example.com:53981.",
			want: []string{"synth.example.com:53981"},
		},
		{
			name: "两个地址用逗号隔开",
			line: "连接 synth-a.example.com:11111,203.0.113.8:22222",
			want: []string{"synth-a.example.com:11111", "203.0.113.8:22222"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := publicAddrCandidates(c.line)
			if len(got) != len(c.want) {
				t.Fatalf("候选 %q，想要 %q\n行：%s", got, c.want, c.line)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("候选 %q，想要 %q", got, c.want)
				}
			}
			for _, bad := range []string{"ext.go:37", "control.go:168", "control.go:37", "control.go:100", "manager.go:173"} {
				for _, g := range got {
					if g == bad {
						t.Fatalf("把源码位置 %s 当成了地址，行：%s", bad, c.line)
					}
				}
			}
		})
	}
}

func newBareTunnel(t *testing.T, provider string) (*Manager, *Tunnel) {
	t.Helper()
	tempBase(t)
	m := NewManager()
	tun := &Tunnel{ID: "tun-synth", Provider: provider, Name: "合成"}
	m.mu.Lock()
	m.tunnels = []*Tunnel{tun}
	m.seq = 1
	m.mu.Unlock()
	return m, tun
}

func TestPublicAddrIgnoresSourceLocation(t *testing.T) {
	const host = "synth.example.com:53981"
	line := "2006-01-02 15:04:05.000 [I] [client/control_ext.go:37] TCP 类型隧道启动成功 使用 [" + host + "] 来连接到你的隧道"
	for _, provider := range []string{"openfrp", "natfrp"} {
		t.Run(provider, func(t *testing.T) {
			m, tun := newBareTunnel(t, provider)
			rs := &runState{}
			m.handleLine(tun, rs, line)
			if tun.PublicAddr != host {
				t.Fatalf("%s 公网地址 = %q", provider, tun.PublicAddr)
			}
			if !rs.sawDomain || !rs.online {
				t.Fatalf("应记成域名并算连上：sawDomain=%v online=%v", rs.sawDomain, rs.online)
			}
			text := strings.Join(m.Console(tun.ID).Lines(), "\n")
			if !strings.Contains(text, "公网地址: "+host) || strings.Contains(text, "ext.go:37") {
				t.Fatalf("控制台：%s", text)
			}
		})
	}
}

func TestPublicAddrSourceNoiseDoesNotReplaceReal(t *testing.T) {
	m, tun := newBareTunnel(t, "openfrp")
	rs := &runState{}
	m.handleLine(tun, rs, "[client/control_ext.go:37] login to server success")
	if tun.PublicAddr != "" {
		t.Fatalf("登录行不该出地址：%q", tun.PublicAddr)
	}
	m.handleLine(tun, rs, "使用 synth.example.com:53981 连接")
	m.handleLine(tun, rs, "2006-01-02 15:04:05.000 [I] [client/control.go:168] [mc] start proxy success")
	m.handleLine(tun, rs, "[proxy/proxy_manager.go:173] proxy added: [mc]")
	if tun.PublicAddr != "synth.example.com:53981" {
		t.Fatalf("后面的源码位置盖掉了真地址：%q", tun.PublicAddr)
	}
	// 只有源码位置、没有真地址：保持上一次的，也不要把文件名写进控制台那句公网地址
	before := strings.Join(m.Console(tun.ID).Lines(), "\n")
	m.handleLine(tun, rs, "[control.go:37] 映射启动成功")
	if tun.PublicAddr != "synth.example.com:53981" {
		t.Fatalf("单独的源码位置改写了地址：%q", tun.PublicAddr)
	}
	if after := strings.Join(m.Console(tun.ID).Lines(), "\n"); after != before {
		t.Fatalf("不该再记一条公网地址：\n%s", after)
	}
}

func TestPublicAddrPreferenceAndFallback(t *testing.T) {
	m, tun := newBareTunnel(t, "natfrp")
	rs := &runState{}
	m.handleLine(tun, rs, "使用 203.0.113.7:12345 连接")
	if tun.PublicAddr != "203.0.113.7:12345" {
		t.Fatalf("只有 IP：%q", tun.PublicAddr)
	}
	m.handleLine(tun, rs, "[client/control.go:10] 使用 frp-abc.natfrp.cloud:12345 或 203.0.113.9:12345 连接")
	if tun.PublicAddr != "frp-abc.natfrp.cloud:12345" || !rs.sawDomain {
		t.Fatalf("同一行域名应盖过 IP：%q saw=%v", tun.PublicAddr, rs.sawDomain)
	}
	m.handleLine(tun, rs, "或使用节点 IP 连接（不推荐）>>203.0.113.8:12345<<")
	if tun.PublicAddr != "frp-abc.natfrp.cloud:12345" {
		t.Fatalf("见过域名以后 IP 不该盖过：%q", tun.PublicAddr)
	}

	// 新的一次运行只给了 IP
	rs2 := &runState{}
	m.handleLine(tun, rs2, "或使用节点 IP 连接（不推荐）>>203.0.113.9:12345<<")
	if tun.PublicAddr != "203.0.113.9:12345" {
		t.Fatalf("这次只有 IP：%q", tun.PublicAddr)
	}

	// 两个域名取后面的；IPv6 让位给域名
	rs3 := &runState{}
	m.handleLine(tun, rs3, "使用 [synth-a.example.com:11111] 或 [synth-b.example.com:22222] 连接")
	if tun.PublicAddr != "synth-b.example.com:22222" {
		t.Fatalf("多个域名应取后面的：%q", tun.PublicAddr)
	}
	m.handleLine(tun, rs3, "remote [2001:db8::1]:53981")
	if tun.PublicAddr != "synth-b.example.com:22222" {
		t.Fatalf("IPv6 不该盖过域名：%q", tun.PublicAddr)
	}

	rs4 := &runState{}
	m.handleLine(tun, rs4, "[client/control_ext.go:37] remote [2001:db8::1]:53981")
	if tun.PublicAddr != "[2001:db8::1]:53981" {
		t.Fatalf("只有 IPv6：%q", tun.PublicAddr)
	}
	if isIPAddr(tun.PublicAddr) != true {
		t.Fatal("IPv6 应算 IP，好让后面的域名还能盖过它")
	}
}

func TestPublicAddrSkipsLocalAndMasked(t *testing.T) {
	m, tun := newBareTunnel(t, "openfrp")
	rs := &runState{}
	m.handleLine(tun, rs, "启动成功 127.0.0.1:25565 0.0.0.0:7000 [::1]:25565 [::]:80 synth.example.com:53981")
	if tun.PublicAddr != "synth.example.com:53981" {
		t.Fatalf("本地地址不该留下：%q", tun.PublicAddr)
	}
	m2, tun2 := newBareTunnel(t, "openfrp")
	m2.handleLineMasked(tun2, &runState{}, "[client/control_ext.go:37] 使用 synth.example.com:53981 连接", func(s string) string {
		if strings.Contains(s, "synth.example.com") {
			return "hidden"
		}
		return s
	})
	if tun2.PublicAddr != "" {
		t.Fatalf("脱敏掉的地址不该改记成源码位置：%q", tun2.PublicAddr)
	}
}

// 同一行里 IP 在前、域名在后：只写一次域名。控制台那句「公网地址」和 tunnel.up 事件是同一个条件下发的，
// 这里数控制台的句子，就是在数往外推了几次（webhook、voice_host）。
func TestPublicAddrSameLineWritesOnce(t *testing.T) {
	countAddrLines := func(m *Manager, id string) int {
		n := 0
		for _, l := range m.Console(id).Lines() {
			if strings.Contains(l, "公网地址: ") {
				n++
			}
		}
		return n
	}

	m, tun := newBareTunnel(t, "natfrp")
	rs := &runState{}
	m.handleLine(tun, rs, "[client/control_ext.go:37] 使用 203.0.113.7:12345 或 frp-abc.natfrp.cloud:12345 连接")
	if tun.PublicAddr != "frp-abc.natfrp.cloud:12345" || !rs.sawDomain {
		t.Fatalf("同一行域名应盖过 IP：%q saw=%v", tun.PublicAddr, rs.sawDomain)
	}
	if n := countAddrLines(m, tun.ID); n != 1 {
		t.Fatalf("同一行只该写一次公网地址，写了 %d 次：\n%s", n, strings.Join(m.Console(tun.ID).Lines(), "\n"))
	}

	// 两个 IP、前面没见过域名：取后面的，也只写一次
	m2, tun2 := newBareTunnel(t, "natfrp")
	m2.handleLine(tun2, &runState{}, "连接 203.0.113.7:12345 或 203.0.113.8:12345")
	if tun2.PublicAddr != "203.0.113.8:12345" {
		t.Fatalf("两个 IP 应取后面的：%q", tun2.PublicAddr)
	}
	if n := countAddrLines(m2, tun2.ID); n != 1 {
		t.Fatalf("两个 IP 只该写一次，写了 %d 次", n)
	}

	// 已经见过域名，这一行只有 IP：一次都不写
	before := countAddrLines(m, tun.ID)
	m.handleLine(tun, rs, "或使用节点 IP 连接（不推荐）>>203.0.113.9:12345<< 或 >>203.0.113.10:12345<<")
	if tun.PublicAddr != "frp-abc.natfrp.cloud:12345" || countAddrLines(m, tun.ID) != before {
		t.Fatalf("见过域名后只有 IP 的行不该写：%q", tun.PublicAddr)
	}
}

// 端口被截短的候选不进公网地址：不写、不在控制台记那句「公网地址」；后面来了正常的地址照常写。
func TestPublicAddrRejectsTruncatedPort(t *testing.T) {
	m, tun := newBareTunnel(t, "natfrp")
	rs := &runState{}
	for _, line := range []string{
		"启动成功 使用 [2001:db8::1]:123456 连接",
		"启动成功 使用 host.example:12345abc 连接",
		"remote 203.0.113.7:123456",
	} {
		m.handleLine(tun, rs, line)
		if tun.PublicAddr != "" {
			t.Fatalf("截短的端口被当成了地址：%q（行：%s）", tun.PublicAddr, line)
		}
	}
	if text := strings.Join(m.Console(tun.ID).Lines(), "\n"); strings.Contains(text, "公网地址") {
		t.Fatalf("不该记公网地址：\n%s", text)
	}
	m.handleLine(tun, rs, "使用 frp-abc.natfrp.cloud:12345 连接")
	if tun.PublicAddr != "frp-abc.natfrp.cloud:12345" {
		t.Fatalf("正常的地址：%q", tun.PublicAddr)
	}
}

func TestCustomPublicAddrIgnoresLogNoise(t *testing.T) {
	m, tun := newBareTunnel(t, "custom")
	tun.ServerAddr = "frp.example.com"
	tun.ServerPort = 7000
	tun.RemotePort = 25566
	rs := &runState{}
	// 成功之前，带「连接」的行如果只有源码位置，不能先写成 ext.go:37
	m.handleLine(tun, rs, "[client/control_ext.go:37] 正在连接")
	if tun.PublicAddr != "" {
		t.Fatalf("自定义隧道不该采用源码位置：%q", tun.PublicAddr)
	}
	m.handleLine(tun, rs, "[client/control.go:168] [mc] start proxy success")
	if tun.PublicAddr != "frp.example.com:25566" {
		t.Fatalf("成功后应用配置里的地址：%q", tun.PublicAddr)
	}
}
