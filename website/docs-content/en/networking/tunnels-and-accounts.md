---
title: Tunnels and Platform Accounts
description: Expose your local Minecraft server over the Internet using OpenFrp, SakuraFrp, or a custom frps server when friends are not on the same LAN.
order: 20
since: v3.0.0
beta: false
featured: true
tags: ["Tunneling", "OpenFrp", "SakuraFrp", "frps", "frpc", "Follow Server Start", "Platform Account", "Browser OAuth", "Access Key", "Antivirus"]
updated: 2026-11-18
aliases: []
draft: false
---

> [!NOTE]
> **Friends on the same LAN do NOT need tunneling** (same home router or dorm subnet): share the `LAN_IP:Port` displayed in the server header instead. Only use FRP tunneling when playing across separate networks without a public IP.

Open sidebar **「内网穿透」** (Tunnels) to expose local servers using **OpenFrp**, **SakuraFrp (**樱花穿透**)**, or a **custom `frps`** instance.

## Two Ways to Add a Tunnel {#ways}

### Option A: Connect a Platform Account & Create/Import Inside MineDeck (`v3.2.0+`) {#account}

Skip copying tunnel IDs and tokens back and forth by signing in under **「平台账户」** (Platform Accounts) below the list:

- **OpenFrp**: Click **「浏览器授权」** (Browser Authorize) and sign in via your browser—MineDeck never stores your password.
- **SakuraFrp**: Paste your Access Key (**访问密钥**) from the SakuraFrp dashboard.

Once linked, view traffic quotas and account summaries in-app or jump to the provider site for daily check-ins/purchases (MineDeck never auto-checks in or buys plans on your behalf). Create new TCP or UDP tunnels or import existing ones. In SakuraFrp's **「在 樱花frp 创建并导入」** dialog, check **「这是公开服务器」** (This is a public server) whenever posting the IP publicly on forums or server lists, and select nodes marked for multi-line or public gaming (violating node rules can result in account bans without refunds). Private play with close friends can leave it unchecked; the standalone manual "Add Tunnel" form does not include that toggle.

> [!WARNING]
> **Security Boundary**: Connecting/disconnecting platform accounts and creating/importing tunnels alter credentials and **work only on the host desktop**; Phone & LAN Remote browsers show status read-only. Real-world coverage across diverse platform account setups is also continuously expanding based on user feedback.

### Option B: Manual Entry via **「添加隧道」** (Add Tunnel) {#manual}

Click **「添加隧道」** above the list and select your provider:

| Provider | Required Inputs | Client Binary |
| --- | --- | --- |
| **OpenFrp** | Create a TCP tunnel matching your server port in OpenFrp console, then enter **User Token** and **Tunnel ID** | Downloaded automatically by MineDeck |
| **SakuraFrp** | Create the tunnel on SakuraFrp, then enter **Access Key** and **Tunnel ID** (public-server node toggle is only in Option A's dialog) | Follow the card prompt to download and place `frpc` once |
| **Custom `frps`** | Enter `frps` host, `frps` port, `auth token`, public remote port, and local port (**never enter MineDeck's own web port**) | `frpc` downloaded automatically by MineDeck |

## Binding a Server and Enabling "Follow Server Start" {#bind}

Select your target **「绑定服务器」** (Bound Server) and turn on **「跟随服务器启动」** (Follow Server Start):

- Starting the bound server launches the tunnel automatically; during crash auto-restarts, the tunnel stays up without flapping.
- Stopping the server keeps the tunnel up briefly and closes it after ~5 seconds if not restarted.
- **Copying the public address**: Once running, the public address (preferring hostnames over raw IPs) appears on both the tunnel card and the server list card—click to copy.

> [!TIP]
> For UDP voice tunnels used by **Music Sharing**, OpenFrp UDP voice relay setups remain under ongoing real-world verification—check **「扩展 → 音乐共享」** inside your server details and test with your network.

## Troubleshooting `frpc` Alerts and Address Issues {#troubleshoot}

- **Short secrets masked**: Tokens under 6 bytes in custom `frps` tunnels hide matching log lines so keys cannot be inferred from logs; use longer tokens.
- **`frpc` blocked or missing**: This card hint appears only in two cases: Windows returned error `225`/`226` (blocked as malware/PUA, with `226` indicating deletion), or `frpc.exe` disappeared right after existing. Treat it strictly as detection of those two states—not automatic proof of a false positive. Check the download origin and hash shown by MineDeck before deciding whether to restore from **Protection History** (**保护历史记录**) or your antivirus quarantine.
- **Public address errors**: Inspect the tunnel card error and tunnel logs directly; if an older build leaked a local path into the address line, upgrade to `v3.2.0` or newer.

Still stuck? Check [Port Busy, LAN Blocked, and frpc Alerts](../troubleshooting/port-firewall-frpc.md) or [General FAQ](../faq/general-faq.md).
