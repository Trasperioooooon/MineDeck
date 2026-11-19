---
title: 数据位置、搬家与卸载
description: 认识 MineDeck 绿色自包含目录结构、DPAPI 加密迁移限制、跨盘改目录方法与彻底卸载步骤。
order: 40
since: v3.0.0
beta: false
featured: false
tags: ["数据位置", "目录结构", "搬家", "迁移", "卸载", "secrets.json", "runtimes", "DPAPI", "AppData"]
updated: 2026-11-18
aliases: []
draft: false
---

MineDeck 采用自包含（Portable）设计：**核心数据全部留在 `MineDeck.exe` 所在文件夹下**，不会往注册表乱写核心数据，也不会改动系统 Java 环境变量。

## 默认数据目录结构 {#folders}

假设你把 `MineDeck.exe` 放在 `D:\Games\MineDeck\`，运行后同一文件夹下会逐步生成以下内容：

| 文件夹 / 文件 | 用途 | 搬家 / 删除提示 |
| --- | --- | --- |
| `servers\` | 默认新建的各台服务器目录 | 也可以通过全局设置改放到别的磁盘 |
| `backups\` | 世界备份文件（`.zip`） | 独立于服务器存放，移出或删服后备份仍保留 |
| `runtimes\` | MineDeck 自动下载的便携 Java 与 `frpc` | **不要手动删**，否则使用自带 Java 的服无法启动 |
| `cache\` | 核心安装包下载缓存、工具箱临时成品（`cache\toolbox\`）等 | 体积大时可在确认无后台任务运行后清理 |
| `logs\` | 运行日志与操作审计记录（`logs\ops\`） | 排查问题或反馈时可参考 |
| `data\` | 待装清单临时本地文件、MCP 运行时状态等内部数据 | 随主目录保留即可 |
| `config.json` | 全局设置、更新渠道、额外存储根、接管实例登记、已信任下载域名 | 重要配置文件，搬家需一并带走 |
| `secrets.json` | CurseForge API Key、OpenFrp/樱花账户凭据、AI API Key、MCP 令牌 | **使用 Windows DPAPI 按当前电脑+当前 Windows 用户加密** |
| `tunnels.json` | 已添加的穿透隧道配置与加密后的自定义/平台隧道凭据 | 其中加密凭据同样绑定当前电脑+当前 Windows 用户 |

除上述同级目录外，只有以下少量路径会落在系统目录：
- **`%LocalAppData%\MineDeck`**：WebView2 桌面窗口缓存与本地界面偏好存储（配色/深浅色等）。
- **Windows 注册表当前用户启动项 `HKCU\...\Run`**：仅在你手动开启 **「设置 → 启动与托盘 → 开机自动启动」** 时写入；不需要管理员权限。

## 更改默认存放目录（不用手动挪旧服） {#change-roots}

如果你后续想把新的服务器或新备份存到容量更大的盘符：

1. 进入侧栏 **「设置 → 存储位置」**（仅在本机桌面打开时可修改，局域网设备不可更改）。
2. 选择新的 **「服务器存放文件夹」** 或 **「备份存放文件夹」**。

> [!NOTE]
> 修改存放位置立即生效，但 **只对之后新建的服务器与新生成的备份有效**；原有服务器与旧备份会继续留在原文件夹照常显示、启动与还原，程序会自动扫描旧根目录。

## 整套搬家与换电脑注意事项 {#migration}

想把 MineDeck 换到别的磁盘或另一台 Windows 电脑时：

1. 先完全停止所有运行中的服务器并退出 MineDeck。
2. **不要只搬一个文件夹**：建议把包含 `MineDeck.exe`、`servers`、`backups`、`runtimes`、`config.json` 等在内的整个主目录原封不动拷贝到目标位置（如果只是某些单台服搬家，也可以用 [接管已有服务器与交接包](../server-setup/adopt-and-handover.md) 里的 `.mdpack` 方式）。
3. **路径变化后的必做检查**：如果新电脑或新磁盘路径变了，之前选用 MineDeck 便携 Java 的服务器会保留原绝对路径，启动时页头可能会提示找不到 Java。打开对应服务器的 **「设置 → 启动与内存」** 重新点选一次即可。
4. **换电脑或换 Windows 用户后的加密凭据重填**：由于 `secrets.json` 和 `tunnels.json` 依赖当前电脑当前 Windows 用户的 DPAPI 加密钥匙，拷贝到新电脑或另一个系统用户下后**无法自动解密**——启动后界面会给出提醒，你需要重新填写 **OpenFrp / 樱花平台账户、自定义隧道凭据、CurseForge Key、AI 服务商 API Key**，并在 **MCP 接口** 点一次 **「重置令牌」**。

## 彻底卸载 MineDeck {#uninstall}

如果需要从电脑上完全移除 MineDeck：

1. 如果曾打开过开机自启，先进入程序 **「设置 → 启动与托盘」** 关闭开机自动启动（清理注册表启动项）。
2. 停止所有服务器并点击左下角 **「退出 MineDeck」**。
3. 直接删除 `MineDeck.exe` 所在的整个文件夹。
4. 在资源管理器地址栏输入 `%LocalAppData%\MineDeck`，如果该目录存在也一并删除。
5. 如果你在「设置 → 存储位置」中曾把服务器、备份或接管过的服务器目录放到过其他盘符，确认不需要后一并手动删除即可。
