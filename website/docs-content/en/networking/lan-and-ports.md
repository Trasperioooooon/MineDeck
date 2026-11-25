---
title: LAN Play, Windows Firewall, and Port Rules
description: Connect directly over a home router or dorm LAN without tunneling, allow Java through Windows Firewall, and understand port conflicts and Windows excluded port ranges.
order: 10
since: v3.0.0
beta: false
featured: false
tags: ["LAN", "Connect Address", "Firewall", "Allow Access", "Ports", "25565", "Multiple NICs"]
updated: 2026-11-18
aliases: []
draft: false
---

When host and players share **the same router, home Wi-Fi, or virtual LAN tool**, **you do not need FRP tunneling**—connect straight over LAN IP + port for lower latency and zero bandwidth caps.

## Finding and Sharing Your LAN Address {#lan-address}

1. Start the server from **「服务器」** (Servers) or its details page and wait for `Done (...)!` in the console.
2. On the host PC itself, connect using `127.0.0.1` (or `127.0.0.1:<port>` if changed from `25565`).
3. Give friends on the same LAN the **`LAN_IP:Port`** address shown in the server header or card (click to copy).

> [!TIP]
> **Switching Multi-NIC IPs**: If your PC uses Ethernet + Wi-Fi or runs Hyper-V / WSL / VMware virtual adapters, MineDeck prioritizes standard private subnets (`192.168.x.x` / `10.x.x.x`). If friends still cannot reach the default IP, click **「···」** in the header (or card status selector) to switch to another adapter IP (saved in that browser).

## Windows Defender Firewall Prompt on First Launch {#firewall}

- The **first time a server runs**, Windows Defender Firewall usually asks whether Java (`OpenJDK Platform binary` / `javaw.exe`) may access the network.
- Check the active network profile(s) and click **「允许访问」** (Allow Access). Dismissing the prompt or allowing only the wrong profile leaves LAN friends stuck on "Connecting to the server..." until timeout—see [Port Busy, LAN Blocked, and frpc Antivirus Alerts](../troubleshooting/port-firewall-frpc.md) to fix it afterward.

## Port Assignment and Same-Host Avoidance {#ports}

- **Automatic avoidance on create**: When choosing a port (default `25565`) in **「新建服务器」** (Create Server), MineDeck skips ports used by other MineDeck servers and currently active listening sockets.
- **Pre-start bind check**: Before booting Java, MineDeck performs a quick bind probe on the server's `server-ip` and `server-port`; if busy, startup stops cleanly with a **「改端口」** (Change Port) prompt or suggested open port (except proxies or port `0`).
- **Windows system excluded port ranges**: Enabling Hyper-V, WSL2, or Docker Desktop reserves TCP port blocks at the OS level that cannot always be spotted cleanly during initial creation—if startup reports `Permission denied` or `Address already in use`, switch to another range (such as `25575`–`25699`).
- **Friends on different networks?** Without a public IP, set up [Tunnels and Platform Accounts](./tunnels-and-accounts.md).
