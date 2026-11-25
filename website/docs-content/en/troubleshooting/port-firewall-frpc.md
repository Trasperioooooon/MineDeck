---
title: Port Busy, LAN Connection Blocked, and frpc Antivirus Alerts
description: Fix FAILED TO BIND TO PORT (including Windows excluded TCP ranges), unblock Windows Firewall for LAN friends, and handle OpenFrp/SakuraFrp frpc quarantine.
order: 30
since: v3.0.0
beta: false
featured: false
tags: ["Port Busy", "FAILED TO BIND TO PORT", "Excluded Port Range", "Hyper-V", "LAN Blocked", "Firewall", "frpc", "Antivirus", "HTTP 567", "OpenFrp", "SakuraFrp"]
updated: 2026-11-18
aliases: []
draft: false
---

"Port already in use", "LAN friends time out on the same Wi-Fi", and "tunnel fails to start `frpc`" are the three most common networking snags.

## 1. Port Busy / `FAILED TO BIND TO PORT` {#port-busy}

- **Occupied by another MineDeck server on this PC**:
  Click **「改端口」** (Change Port) or the suggested switch-port button in the header/banner to move `server-port` to an open port (e.g. `25566`).
- **Leftover crash process or third-party app**:
  Check Task Manager for any stray `java.exe`, and ensure Query / RCON ports don't collide with the main game port.
- **Windows Hyper-V / WSL2 / Docker excluded port ranges (hidden trap!)**:
  Enabling Hyper-V, WSL2, Docker Desktop, or Android emulators causes Windows to reserve chunks of dynamic TCP ports—sometimes right across `25565`. Inspect active reserved blocks in PowerShell and pick a clean port (such as `35565`) in **「设置 → 常用 → 服务器端口」**:

```powershell
netsh interface ipv4 show excludedportrange protocol=tcp
```

## 2. Server Is Running, but LAN Friends Time Out {#lan-blocked}

If `127.0.0.1` joins locally on the host PC while friends on the same router time out on "Connecting to the server...":

1. **Allow Java through Windows Defender Firewall**: If you canceled the first-launch firewall popup, open *Windows Security → Firewall & network protection → Allow an app through firewall*, locate **`OpenJDK Platform binary`** (or your active `java.exe`), and enable both **Private** and **Public** networks.
2. **Check adapter IP / subnet**: Click **「···」** in the server header to verify you copied your router's `192.168.x.x` LAN IP rather than `127.0.0.1` or a virtual VMware/VirtualBox adapter (`192.168.56.x`).
3. **Keep `server-ip` blank**: In **「设置 → 常用」**, `server-ip` should almost always stay **empty** unless intentionally binding a multi-homing IP.
4. **AP isolation or campus Wi-Fi**: Guest Wi-Fi, dorm networks, and routers with AP/client isolation block peer-to-peer LAN traffic—use [Tunnels and Platform Accounts](../networking/tunnels-and-accounts.md) instead.

## 3. `frpc` Download Failures or Antivirus Quarantine {#frpc-blocked}

- **Windows Security / third-party AV quarantining `frpc.exe`**:
  Security suites frequently flag or delete `frpc.exe` under `runtimes\frpc\` because public tunneling binaries are also bundled in remote-admin tools.
  Fix: Open *Windows Security → Virus & threat protection → Protection history*, check the download source and hash presented by MineDeck, and restore/exclude the file in your AV quarantine only once verified.
- **OpenFrp HTTP 567 bot block**:
  Since `v3.1.2`, MineDeck routes OpenFrp binary downloads over a dedicated TLS 1.3 path; if upstream changes break automatic download, follow the manual path shown in the tunnel error card (extract `frpc.exe` from `frpc_windows_amd64.zip` into **`runtimes\frpc\openfrp\`**).
- **SakuraFrp manual binary placement**:
  If a SakuraFrp tunnel reports a missing client binary on first use, download the Windows `frpc` client from SakuraFrp and place it into the folder shown on the tunnel card (once placed, future SakuraFrp tunnels reuse it).
- **"Cannot decrypt credentials" after switching PCs or Windows accounts**:
  Credentials in `tunnels.json` are bound to your Windows user via DPAPI; clear and re-enter your access key or re-authorize your platform account.
