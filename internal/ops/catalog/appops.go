package catalog

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"minedeck/internal/app"
	"minedeck/internal/autostart"
	"minedeck/internal/ops"
	"minedeck/internal/procutil"
	"minedeck/internal/update"
)

// publicConfig 返回可下发的配置副本：抹去访问密码哈希和 CurseForge API Key
// （是否已设置由 app.info 的 lanSet、cfKeySet 告知）。
// 凡是把配置写进响应的地方都必须经由此函数，不得直接下发 app.GetConfig()。
func publicConfig() app.Config {
	cfg := app.GetConfig()
	cfg.AccessPasswordHash = "" // 不下发哈希
	cfg.CFApiKey = ""           // Key 只能写、不能读（迁移没成功时这里还留着明文）
	return cfg
}

func localIPs() []string {
	var out []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
			out = append(out, ipn.IP.String())
		}
	}
	return out
}

// pickFolder 通过 PowerShell 调用系统 FolderBrowserDialog（-STA），返回所选目录。
func pickFolder() (string, error) {
	const ps = "Add-Type -AssemblyName System.Windows.Forms | Out-Null; " +
		"$f = New-Object System.Windows.Forms.FolderBrowserDialog; " +
		"$f.Description = 'Choose a folder for MineDeck'; $f.ShowNewFolderButton = $true; " +
		"if ($f.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($f.SelectedPath) }"
	cmd := exec.Command("powershell", "-STA", "-NoProfile", "-NonInteractive", "-Command", ps)
	cmd.Env = procutil.CleanEnv()
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("打开文件夹对话框失败: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}

// configPatch 修改全局配置的参数：只更新传了的键（指针为 nil 表示没传）。
type configPatch struct {
	Source             *string `json:"source"`
	CFApiKey           *string `json:"cfApiKey"` // 只写：DPAPI 加密存进 secrets.json；空串清除
	WebhookURL         *string `json:"webhookUrl"`
	UpdateRepo         *string `json:"updateRepo"`
	CheckUpdateOnStart *bool   `json:"checkUpdateOnStart"`
	ServersDir         *string `json:"serversDir"`
	BackupsDir         *string `json:"backupsDir"`
	BackupKeep         *int    `json:"backupKeep"`
	Onboarded          *bool   `json:"onboarded"`
	TosAccepted        *bool   `json:"tosAccepted"`
	MinimizeToTray     *bool   `json:"minimizeToTray"`
	AutoStart          *bool   `json:"autoStart"`
	ListenLAN          *bool   `json:"listenLan"`
	LanPassword        *string `json:"lanPassword"` // 明文仅在本次请求中出现，存储为 bcrypt 哈希
	ChromeColor        *string `json:"chromeColor"` // 界面标题栏底色 #rrggbb（下次启动时 WebView2 的默认底色）；空串清除
}

func (c *cat) appOps() {
	c.reg.Register(&ops.Op{
		Name: "app.info", Title: "查看程序信息", Level: ops.Read,
		Desc: "MineDeck 的版本、本机内存总量和可用量、实际监听端口、实例和备份的存放目录、全局配置（不含任何密钥）、本机的局域网 IP。",
		Run: func(context.Context, ops.Caller, ops.Args) (any, error) {
			cfSet, cfBroken := app.CFKeyState()
			return map[string]any{
				"version":     app.Version,
				"os":          app.OSVersion(),
				"base":        app.Base,
				"ramMb":       app.TotalRAMMB(),
				"availRamMb":  app.AvailRAMMB(),
				"port":        c.Port,
				"serversRoot": app.ServersRoot(),
				"backupsRoot": app.BackupsRoot(),
				"config":      publicConfig(),
				"lanSet":      app.GetConfig().AccessPasswordHash != "",
				"cfKeySet":    cfSet,               // CurseForge API Key 是否已设置（Key 本身从不下发）
				"cfKeyBroken": cfBroken,            // 已存的 Key 解不开（换了电脑或 Windows 用户），要重填
				"autoStart":   autostart.Enabled(), // 开机自启真值以注册表为准
				"ips":         localIPs(),
			}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "app.config.set", Title: "修改全局设置", Level: ops.HumanOnly,
		Desc: "只更新传了的键。存放目录、备份目录只能在本机改。",
		Params: ops.Object(map[string]*ops.Schema{
			"source":             ops.String("下载源：auto（国内镜像优先）、mirror（只用国内镜像）、official（只用官方源）"),
			"cfApiKey":           ops.String("CurseForge API Key（只写不读；空串清除）"),
			"webhookUrl":         ops.String("事件推送地址，以 http(s):// 开头；空串关闭"),
			"updateRepo":         ops.String("检查更新用的 GitHub 仓库，如 user/MineDeck"),
			"checkUpdateOnStart": ops.Boolean("启动时在后台检查一次更新"),
			"serversDir":         ops.String("新实例的默认存放目录（仅本机；空串恢复默认）"),
			"backupsDir":         ops.String("备份目录（仅本机；空串恢复默认）"),
			"backupKeep":         ops.Integer("每个实例保留的备份份数，1–1000（超出按边界算）"),
			"onboarded":          ops.Boolean("首次运行的引导已完成"),
			"tosAccepted":        ops.Boolean("已同意用户协议与免责声明"),
			"minimizeToTray":     ops.Boolean("关窗时最小化到托盘"),
			"autoStart":          ops.Boolean("开机自动启动"),
			"listenLan":          ops.Boolean("允许局域网访问（要先设访问密码，重启生效）"),
			"lanPassword":        ops.String("局域网访问密码（只写不读）"),
			"chromeColor":        ops.String("界面标题栏底色 #rrggbb；空串清除"),
		}),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			body, err := bind[configPatch](a)
			if err != nil {
				return nil, err
			}
			// 存放 / 备份目录决定之后实例与备份写到主机何处（下方还会 MkdirAll）：与创建实例的 root 同理仅限本机，
			// 远程 / 局域网请求带这两个键（含置空恢复默认）即整次拒绝，其余键不受影响
			if (body.ServersDir != nil || body.BackupsDir != nil) && !caller.Local {
				return nil, ops.Errorf(403, "存放目录仅支持在本机修改")
			}
			// 可失败的校验与 I/O（建目录、算哈希）在锁外先做完，锁内只做字段套用
			if body.WebhookURL != nil {
				u := strings.TrimSpace(*body.WebhookURL)
				if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
					return nil, fmt.Errorf("Webhook 地址需以 http(s):// 开头")
				}
			}
			var chromeColor string
			if body.ChromeColor != nil && *body.ChromeColor != "" {
				col, ok := app.ParseHexColor(*body.ChromeColor)
				if !ok {
					return nil, fmt.Errorf("chromeColor 须为 #rrggbb 形式的颜色")
				}
				chromeColor = col
			}
			if body.ServersDir != nil {
				if d := strings.TrimSpace(*body.ServersDir); d != "" {
					if err := os.MkdirAll(d, 0o755); err != nil {
						return nil, fmt.Errorf("存放目录不可用: %w", err)
					}
				}
			}
			if body.BackupsDir != nil {
				if d := strings.TrimSpace(*body.BackupsDir); d != "" {
					if err := os.MkdirAll(d, 0o755); err != nil {
						return nil, fmt.Errorf("备份目录不可用: %w", err)
					}
				}
			}
			var newPassHash string
			if body.LanPassword != nil && *body.LanPassword != "" {
				h, err := bcrypt.GenerateFromPassword([]byte(*body.LanPassword), bcrypt.DefaultCost)
				if err != nil { // 超出 bcrypt 72 字节上限等
					return nil, fmt.Errorf("密码不可用（最长 72 字节）: %w", err)
				}
				newPassHash = string(h)
			}
			// 读-改-写在 UpdateConfig 的锁内一次完成，避免并发保存互相丢字段
			if _, err := app.UpdateConfig(func(c *app.Config) error {
				if body.Source != nil {
					c.Source = *body.Source
				}
				if body.WebhookURL != nil {
					c.WebhookURL = strings.TrimSpace(*body.WebhookURL)
				}
				if body.UpdateRepo != nil {
					c.UpdateRepo = strings.TrimSpace(*body.UpdateRepo)
				}
				if body.CheckUpdateOnStart != nil {
					c.CheckUpdateOnStart = *body.CheckUpdateOnStart
				}
				if body.ServersDir != nil {
					c.ServersDir = strings.TrimSpace(*body.ServersDir) // 旧值由 UpdateConfig 并入 Roots，旧实例照常扫描
				}
				if body.BackupsDir != nil {
					c.BackupsDir = strings.TrimSpace(*body.BackupsDir) // 旧值由 UpdateConfig 并入 BackupRoots，旧备份照常可用
				}
				if body.BackupKeep != nil {
					k := *body.BackupKeep
					if k < 1 {
						k = 1
					} else if k > 1000 {
						k = 1000
					}
					c.BackupKeep = k
				}
				if body.Onboarded != nil {
					c.Onboarded = *body.Onboarded
				}
				if body.TosAccepted != nil {
					c.TosAccepted = *body.TosAccepted
				}
				if body.MinimizeToTray != nil {
					c.MinimizeToTray = *body.MinimizeToTray
				}
				if newPassHash != "" {
					c.AccessPasswordHash = newPassHash
				}
				if body.ChromeColor != nil {
					c.ChromeColor = chromeColor
				}
				if body.ListenLAN != nil {
					if *body.ListenLAN && c.AccessPasswordHash == "" {
						return fmt.Errorf("开启局域网访问前请先设置访问密码")
					}
					c.ListenLAN = *body.ListenLAN
				}
				return nil
			}); err != nil {
				return nil, err
			}
			// CurseForge API Key 存在 secrets.json（不落 config.json），和开机自启一样放在配置保存成功之后再写
			if body.CFApiKey != nil {
				if err := app.SetCFKey(*body.CFApiKey); err != nil {
					return nil, ops.Errorf(500, "保存 CurseForge API Key 失败: %w", err)
				}
			}
			// 开机自启是注册表副作用（不落 config.json）：放在所有校验与配置保存成功之后再执行，
			// 避免上面任一校验失败提前 return 时已经改动了注册表（否则会「报错却已悄悄改了自启」）
			if body.AutoStart != nil {
				if err := autostart.Set(*body.AutoStart); err != nil {
					return nil, ops.Errorf(500, "设置开机自启失败: %w", err)
				}
			}
			return publicConfig(), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "app.shutdown", Title: "退出 MineDeck", Level: ops.HumanOnly,
		Desc: "优雅停止所有运行中的服务器（保存世界）后退出程序。",
		Run: func(context.Context, ops.Caller, ops.Args) (any, error) {
			if c.Shutdown != nil {
				go func() {
					time.Sleep(300 * time.Millisecond) // 先把响应发出去
					c.Shutdown()
				}()
			}
			return "ok", nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "app.update.check", Title: "检查更新", Level: ops.Read,
		Desc: "到设置里的 GitHub 仓库查最新版本（要联网）。",
		Run: func(ctx context.Context, _ ops.Caller, _ ops.Args) (any, error) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			rel, has, err := update.Check(ctx, app.GetConfig().UpdateRepo)
			if err != nil {
				return nil, ops.Wrap(502, err)
			}
			return map[string]any{"current": app.Version, "latest": rel, "hasUpdate": has}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "app.update.apply", Title: "更新并重启 MineDeck", Level: ops.HumanOnly,
		Desc: "下载新版本、校验后替换程序并重启。所有服务器都停止后才能更新。",
		Run: func(context.Context, ops.Caller, ops.Args) (any, error) {
			// 防止换血期间新旧进程并发管理同一存档
			for _, i := range c.Mgr.List() {
				if i.Status() != "stopped" {
					return nil, fmt.Errorf("实例「%s」仍在运行，请先停止所有服务器再更新", i.Name)
				}
			}
			ctx := context.Background() // 独立于请求生命周期
			rel, has, err := update.Check(ctx, app.GetConfig().UpdateRepo)
			if err != nil {
				return nil, ops.Wrap(502, err)
			}
			if !has {
				return nil, fmt.Errorf("已是最新版本")
			}
			if err := update.Apply(ctx, rel); err != nil {
				return nil, ops.Wrap(502, err)
			}
			if c.Shutdown != nil {
				go func() {
					time.Sleep(500 * time.Millisecond)
					c.Shutdown()
				}()
			}
			return "updating", nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "host.pickdir", Title: "选择文件夹", Level: ops.HumanOnly, LocalOnly: true, NoLog: true,
		LocalMsg: "文件夹选择仅支持在本机操作（远程管理时请手动填写路径）",
		Desc:     "在主机桌面上弹出系统的文件夹选择框，返回所选目录（取消为空）。",
		Run: func(context.Context, ops.Caller, ops.Args) (any, error) {
			path, err := pickFolder()
			if err != nil {
				return nil, ops.Wrap(500, err)
			}
			return map[string]string{"path": path}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "host.openpath", Title: "在资源管理器中打开目录", Level: ops.HumanOnly, LocalOnly: true, NoLog: true,
		LocalMsg: "打开目录仅支持在本机操作",
		Params:   ops.Object(map[string]*ops.Schema{"path": ops.String("要打开的目录")}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			body, err := bind[struct {
				Path string `json:"path"`
			}](a)
			if err != nil {
				return nil, err
			}
			p := filepath.Clean(strings.TrimSpace(body.Path))
			if st, err := os.Stat(p); err != nil || !st.IsDir() {
				return nil, fmt.Errorf("目录不存在")
			}
			_ = exec.Command("explorer.exe", p).Start()
			return "ok", nil
		},
	})
}
