---
title: 端口占用、局域网连不上与 frpc 被拦
description: 解决「FAILED TO BIND TO PORT」端口被占（含 Windows 系统保留段）、局域网连不上以及 OpenFrp/樱花穿透 frpc 被拦截问题。
order: 30
since: v3.0.0
beta: false
featured: false
tags: ["端口被占", "FAILED TO BIND TO PORT", "保留端口", "Hyper-V", "局域网连不上", "防火墙", "frpc", "杀毒软件", "HTTP 567", "OpenFrp", "樱花穿透"]
updated: 2026-11-18
aliases: []
draft: false
---

联机时遇到「打不开端口」「同一路由器也连不上」或「隧道启动失败」，通常属于下面三类场景之一。

## 一、开服前提示端口被占 / `FAILED TO BIND TO PORT` {#port-busy}

- **同机另一台 MineDeck 服务器占用**：
  直接点击页头或控制台提示里的 **「改端口」** 按钮（或者系统推荐的避让端口一键改启动），把 `server-port` 改成空闲端口（例如 `25566`）即可。
- **上次崩溃后进程残余或别的软件占用**：
  按惯例可先检查任务管理器里有没有多余残留的旧 `java.exe`，确认不是其他服务器在用后结束掉再起；若使用了 Query / RCON 也注意核对它们的端口不与主端口撞车。
- **Windows Hyper-V / WSL2 / Docker 系统保留端口冲突（常见隐形坑！）**：
  开启过 Hyper-V、WSL2、Docker Desktop 或安卓模拟器时，Windows 会悄悄切出好几段系统保留 TCP 端口（包含经常撞上的 `25565` 周边端口段），界面新建向导预先查不到这些系统级保留。
  你可以在终端运行以下命令行查明当前保留区间，挑一个**明显避开这些区间**的端口（比如 `35565`）填入 **「设置 → 常用 → 服务器端口」**：

```powershell
netsh interface ipv4 show excludedportrange protocol=tcp
```

## 二、服务器明明开了，但局域网里的朋友连不上 {#lan-blocked}

如果本机 `127.0.0.1` 能顺利进游戏，而同一个路由器的朋友一直「正在连接 → 超时」：

1. **检查 Windows 防火墙**：首次开某台新服时可能把防火墙弹出的访问确认点成了取消，导致别人被拦在门外。前往「Windows 安全中心 → 防火墙和网络保护 → 允许应用通过防火墙」，找到 **`OpenJDK Platform binary`**（或所用 `java.exe`），把「专用 / 公用」权限打勾保存。
2. **确认不在同一个子网或用错了虚拟机网卡 IP**：在服务器页头点 **「···」** 切一下其他展示出的网卡 IP（优先选路由器下发的 `192.168.x.x`），不要发 `127.0.0.1` 或 VMware/VirtualBox 的 `192.168.56.x` 虚拟网卡地址给别人。
3. **核对 `server-ip` 保持为空**：到 **「设置 → 常用」** 查看 `server-ip` 一项——通常必须保留**空白**；一旦误填了别的内网 IP 或公网地址，朋友们反而连不进。
4. **开了 AP 隔离 / 校园网**：公共 Wi-Fi、校园宿舍网或开启了「客人 Wi-Fi / AP 隔离」的路由器会禁止设备间互相直接访问，此时必须改用 [内网穿透与平台账户](../networking/tunnels-and-accounts.md) 走公网连接地址。

## 三、内网穿透 `frpc` 无法下载或被杀软误报拦截 {#frpc-blocked}

- **Windows 安全中心 / 第三方杀毒软件把 `frpc.exe` 当成木马隔离**：
  因为 `frpc` 经常被开源社区与商业工具打包进各种远程程序，安全软件常误报拦截并把文件从 `runtimes\frpc\` 里悄然移走或拒绝对外连网。
  解决：打开「Windows 安全中心 → 病毒和威胁防护 → 保护历史记录」，将 `runtimes\frpc\` 下被误隔离的文件还原并加入信任/排除项。
- **OpenFrp 下载遇到 HTTP 567 拦截**：
  自 `v3.1.2` 起 MineDeck 已针对 OpenFrp 下载源的机器人拦截改用专属 TLS 1.3 线路；若哪天服务商策略变动仍导致下载失败，隧道报错说明里会列出详细手动放法——自己从对应发布处下载 `frpc_windows_amd64.zip`，解压出 `frpc.exe` 放进 **`runtimes\frpc\openfrp\`** 即可。
- **樱花穿透手动放一次 `frpc`**：
  初次使用樱花穿透隧道时，如果提示缺少客户端，请按隧道卡片引导前往樱花管理面板下载 Windows 版客户端放进对应提示目录，只需放置一次，后续隧道均可复用。
- **换电脑或切换 Windows 用户后报错「凭据解不开」**：
  `tunnels.json` 中的穿透凭据由本机 Windows DPAPI 绑定当前电脑与系统用户加密保护，迁移后需要按界面提示清掉并重填一次访问密钥或重新授权平台账户。
