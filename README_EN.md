[简体中文](README.md) | English

<p align="center">
  <img src="internal/web/ui/img/logo-144.png" width="96" alt="MineDeck icon">
</p>

<h1 align="center">MineDeck</h1>

<p align="center">A Windows tool that downloads Java and a Minecraft Java Edition server, then creates, imports, and manages servers in a window.</p>

<p align="center">
  <a href="https://github.com/Trasperioooooon/MineDeck/releases/latest"><img alt="Latest stable release" src="https://img.shields.io/github/v/release/Trasperioooooon/MineDeck"></a>
  <a href="https://github.com/Trasperioooooon/MineDeck/releases"><img alt="GitHub Releases total downloads" src="https://img.shields.io/github/downloads/Trasperioooooon/MineDeck/total"></a>
  <a href="https://github.com/Trasperioooooon/MineDeck/stargazers"><img alt="GitHub stars" src="https://img.shields.io/github/stars/Trasperioooooon/MineDeck"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/Trasperioooooon/MineDeck"></a>
</p>

The release badge points at the GitHub release marked Latest, which is the stable release. Pre-releases are not included. `internal/app/app.go` currently sets `Version` to `3.1.0-beta.2`. That string matches tag `v3.1.0-beta.2`, which is the same commit as current `main`.

Items with no label are in stable v3.0.0. Items marked **(Beta, v3.1.0-beta.1)** or **(Beta, v3.1.0-beta.2)** are already on current `main` and are not in the stable release. A stable install does not offer those pre-releases as updates. Remote branch `claude/minedeck-server-import-config-6dbe46` has no commits that `main` does not already contain.

The interface text is Chinese. Builds are Windows only.

## Features

### Server software

The registry in `internal/mcsrc/registry.go` has 12 types, in this order in the UI:

- Vanilla: the Mojang server.
- Paper: a plugin server. Versions and builds come from PaperMC Fill v3 (`fill.papermc.io`).
- Purpur: a Paper fork. Versions and builds come from `api.purpurmc.org`.
- Leaves: a Paper fork aimed at vanilla behavior. Versions and builds come from the Leaves API.
- Folia: Paper's multithreaded fork. It uses Fill v3, same as Paper.
- Fabric: a mod loader. Versions, loaders, and the installer come from Fabric Meta, with a BMCLAPI mirror.
- NeoForge: the mod loader used from 1.20.2 onward. Install uses the official installer and the BMCLAPI mirror.
- Forge: a mod loader. Install uses the official installer and the BMCLAPI mirror.
- Mohist: Forge mods and Bukkit plugins on one server. Versions and builds come from the MohistMC API.
- Banner: Fabric mods and Bukkit plugins on one server, via the same MohistMC API.
- Velocity: a proxy. Java is fixed at 21. Its version numbers are Velocity's, not Minecraft's. It uses Fill v3.
- Waterfall: a BungeeCord-fork proxy. Java is fixed at 17. It uses Fill v3.

Proxies have no EULA and no `server.properties`.

Supported Minecraft versions start at 1.12.2 (`FloorVersion` in `internal/mcsrc/mojang.go`) and include year-numbered 26.x releases. Which versions a given type can actually install comes from that type's own list. When the list includes a release time, the UI keeps it.

### Versions and Java

The Java major used at create time is the `javaVersion` field in that Minecraft version's metadata. When the field is absent, `staticJavaFor` uses this table:

| Minecraft version | Java |
| --- | --- |
| Before 1.17 (including 1.12.2) | 8 |
| 1.17.x | 16 |
| 1.18 through 1.20.4 | 17 |
| 1.20.5 through 1.21.x | 21 |
| 26.1 and later | 25 |

Velocity always uses Java 21. Waterfall always uses Java 17.

- On startup, MineDeck scans Java already installed on the PC: the registry, `JAVA_HOME`, `PATH`, and common install directories. A matching major is reused.
- Otherwise it downloads a runtime used only by MineDeck, under `runtimes/java-{major}/` next to the program. System environment variables are left unchanged.
- Java download order: the Tsinghua TUNA Adoptium mirror, then the Adoptium API, then the Mojang runtime (mirrored by BMCLAPI, file by file).
- Settings → Java runtimes can rescan, or add a local `java.exe` after probing `java -version`. Pointing at a path accepts loopback requests only.
- **(Beta, v3.1.0-beta.1)** Memory / startup on each server can switch Java: a runtime MineDeck downloaded, one already on the PC, or a new download. If a mod in an imported pack declares a newer Java than the instance uses, the import result and the server page say so and link to that screen. The server can still be started.

### Create and install

The new-server wizard asks for software, version, build, memory, port, difficulty, game mode, and the EULA. Online mode starts unchecked. Allow flight starts checked (the initial checkbox state in `internal/web/ui/app.js`). Non-vanilla software has an optional mod-picking step before basic settings; see the cart below.

- Downloads try each URL in order, up to 3 rounds, with a wait between rounds. The file is checked against the artifact's SHA-1, SHA-256, or MD5, then written atomically.
- Mojang's version manifest and server jar try the BMCLAPI mirror first, then the official URL. Settings → Download and network can force automatic, mirror, or official.
- Paper, Purpur, Folia, Velocity, and Waterfall use their own official endpoints. The code does not attach BMCLAPI to those.
- The Forge and NeoForge installers are passed `--mirror` aimed at BMCLAPI, and the vanilla server jar is placed first. A failed installer run is tried again, up to 3 attempts.
- **(Beta, v3.1.0-beta.2)** Older Forge installers can exit successfully after their own network check fails and they install nothing. That partial result is treated as a failed attempt, and the retry continues. This applies to new old-Forge servers and to handover imports.
- Instances below 1.18.1 get the log4shell `noLookups` flag. 1.12.2 through 1.16.5 also get Mojang's log4j XML. Later edits on Memory / startup put those flags back if they are missing.
- When Java or frpc is started, `JAVA_HOME`, `JAVA_OPTS`, `JAVA_TOOL_OPTIONS`, `_JAVA_OPTIONS`, `JDK_JAVA_OPTIONS`, and `CLASSPATH` are removed from that process environment.
- Each instance directory contains a `run.bat` that matches the GUI launch.
- The port is checked before start. Stop sends `stop`, then force-kills after about 30 seconds. Quitting MineDeck stops running servers and tunnels. Java and frpc are placed in one Job object so they do not stay behind.
- A `stop` typed by a player or in the console is not treated as a crash and does not trigger crash restart.
- While a deploy step is retrying, the task snapshot can include the last host, the reason, and how long ago. If progress and the log are idle for about 30 seconds, the deploy page says the download is slow or retrying.
- When an installer log is very long, the first few dozen lines are kept and the omitted middle is counted. **(Beta, v3.1.0-beta.2)** Handover import and modpack import use the same log trimming, so the installer's output does not push the opening notes off the log.

### Modpack import

- A Modrinth `.mrpack` can be imported directly. Software, version, and loader are read from the pack. Files marked client-only are skipped.
- A CurseForge zip can be imported with your own CurseForge API key. The key is stored with Windows DPAPI in `secrets.json` next to the program. The UI and the API do not return the plaintext.
- The upload limit is 4 GiB. **(Beta, v3.1.0-beta.2)** A selected file over the limit is reported before the upload, and the limit is included in the message.
- **(Beta, v3.1.0-beta.2)** CurseForge files are placed by their site category: mods in `mods`, resource packs in `resourcepacks`, shader packs in `shaderpacks`, data packs in `datapacks`. Only a single file name is accepted; a name that contains a path is ignored.
- **(Beta, v3.1.0-beta.1)** A file whose URL is outside the allowed sites, that has no checksum, that is not https, whose checksum does not match, or that CurseForge could not resolve, no longer fails the whole import. Those files are listed as pending on the task, and each one can be:
  - skipped;
  - replaced by copying a local file or folder as-is, with no content check;
  - downloaded after you read the risk note and confirm, for an https public address. The bytes must match the checksum in the pack. A confirmed host name is stored in `trustedHosts` with scope `modpack`. Later files on that host skip the prompt.
- **(Beta, v3.1.0-beta.1)** The import form has an optional local source directory. Pending files that already exist there are kept as-is. **(Beta, v3.1.0-beta.2)** Once a CurseForge pending item has a file name and project name, a same-named file in that directory can be picked up automatically.
- Downloads use a checked client. A direct connection requires each resolved IP to be public before connect. If a proxy is configured, the check uses the host name, and loopback or private names are refused. Redirects follow https only, at most 10 times.
- Files kept as-is from an override directory or the local source directory are listed on the task as unchecked.

### Adopt an existing server (Beta, v3.1.0-beta.1)

New server can point at a folder that is already a server. MineDeck reads software, version, build, launch file, previous memory, and port, then registers the folder in place. Files stay where they are.

- It writes `instance.json`. An existing `run.bat` is copied to `run.before-minedeck.bat` first (an existing backup is not overwritten). Forge / NeoForge `user_jvm_args.txt` is backed up the same way. JVM arguments other than memory are kept as extra arguments.
- If `eula.txt` has not been accepted, the form must check the EULA before it is written. 1.12.2 through 1.16.5 receive the same log4shell log config as a new server.
- If the task fails, files touched by that attempt are put back.
- Refused folders: a whole drive, the MineDeck program directory and its parents, cache and runtime directories, anything overlapping the backup directory, the servers root, and a folder already listed or currently being created or adopted (including parents and children). Junctions and symbolic links are resolved before the comparison.
- A folder with nothing that can launch is refused. If a child folder looks like a server, the error names it.
- Delete can leave the folder on disk and only remove the server from the list. `instance.json` is renamed to `instance.json.bak`. A later adopt can bring the previous settings back from that file.

### Handover pack (Beta, v3.1.0-beta.1)

While a server is stopped, its card menu can pack it as a `.mdpack` for MineDeck on another PC.

- Two purposes, with the same files and different wording for the receiver: `handover` (hand the server to the next owner; the default) and `distributable` (send it to a friend so they can run their own copy).
- The server jar is omitted by default and downloaded again on import. Full mode includes it.
- Included: worlds (including extra worlds from multi-world plugins), `server.properties`, the software's own config files (such as `spigot.yml`, `bukkit.yml`, `paper.yml`), `mods`, `plugins`, `config`, `kubejs`, `scripts`, whitelist and operator lists, and an already accepted `eula.txt`.
- Not included: logs and crash reports. The server jar is omitted unless full mode is on. Files outside the manifest (for example map tiles a plugin wrote in the server root) are left out and listed one by one in the pack log.
- Previous JVM arguments, schedules, crash-restart, in-game AI, companions, and music sharing are not carried over. RCON that was enabled is turned off. The import log lists what was left behind.
- Config files are copied as they are, including passwords in them (RCON, databases, bot tokens). Player data, the whitelist, and the operator list are in the pack.
- Import is New server → Import handover pack. You set a name and a port. Java and the server jar are prepared on the receiving PC. The Java major can be changed. If the pack already contains an accepted EULA, it does not have to be checked again.
- The upload limit is 8 GiB. **(Beta, v3.1.0-beta.2)** A file over the limit is reported before upload. Choosing a different `.mdpack` clears the previous summary.
- While a pack or import task is running, that server cannot be started or deleted.
- A note for the person receiving the pack: [docs/接收者上手指南.md](docs/接收者上手指南.md).

### Mods, plugins, and the cart

On a non-vanilla instance, the Resources tab searches Modrinth for the current software and Minecraft version and installs into `mods` or `plugins`. This tab is in the stable release.

- The mod list for an instance can be viewed. A file can be enabled or disabled by renaming its extension.
- Mod config files inside the instance can be read and written. Writing goes through a confirmation step.

**(Beta, v3.1.0-beta.1)** The cart is a separate path from Resources:

- The new-server wizard adds an optional Pick mods step for non-vanilla software. It can be skipped. Mod and plugin servers have a Cart tab. Vanilla has neither.
- Search hits come from Modrinth, filtered by software and version. Client-only mods are not added. Files already in the server are not installed again. Downloads are checked against the checksum Modrinth provides.
- Dependencies can be checked first. Required dependencies are listed; skipping the check still installs them with the cart.
- Picks made in the wizard install after the server is created. If the draft cart no longer matches the list that was on screen (for example the assistant added a project), install stops until the current list has been seen.
- The in-app assistant can search and add to the cart. Installing only creates a confirmation card that lists the files to download. They are written after you confirm.

### Running, console, and config

Instance tabs depend on the software: Console; Players and Backups except on a proxy; Schedules; Resources except on vanilla; Cart, Music sharing, In-game AI, and Companions when they apply; Common settings and All config except on a proxy; then Memory / startup.

- The console is an SSE stream: a replay, then live lines. Encoding can be GBK or UTF-8. Warnings and errors are colored. The log can be filtered. Commands keep a history. Common commands and online player names complete on Tab.
- Server console output uses UTF-8 unless that instance was set to GBK.
- The MOTD editor has color choices and a preview. Chinese text and emoji are written in the format the game expects.
- All config edits `server.properties`. The catalog in `internal/web/ui/props-catalog.js` has 78 keys in 7 groups: basics and gameplay, world generation, players and permissions, performance, network and remote management, resource packs, security and misc. Each key has a Chinese label, a description, and whether a restart is required. Keys the current Minecraft version does not use are hidden. Search matches the Chinese label or the key.
- From Minecraft 1.21.9, keys the catalog marks as game rules (for example PVP, command blocks, monster spawning) apply immediately.
- If `level-name` points outside the server folder, backup and restore use the default `world`. **(Beta, v3.1.0-beta.1)** This is a change from v3.0.0, which followed that path.
- Memory is a slider plus a number, starting at 512 MB, shown against physical RAM. Extra JVM arguments are one per line.
- Open directory can open the instance root, or `mods`, `plugins`, `world`, `logs`, `crash-reports`. Those calls accept loopback requests only.

### Players

- View online players, plus the whitelist, operators, and bans.
- Add or remove a whitelist entry; op or deop; kick; ban or pardon.
- While the server is running, these go through console commands. While it is stopped, the list files are edited directly. Kick requires a running server.
- Offline-mode UUIDs are computed with the offline algorithm.

### Backups, crash restart, and schedules

- Create, restore, and delete a backup. A backup can be taken while the server is running.
- Each server keeps 10 backups by default, configurable from 1 to 1000. New backups are written to the current backup root. List, restore, and delete also look at the built-in root and roots used before the directory was changed. The same file name is one backup. Only a `.zip` file name is accepted.
- Crash restart is off by default. When it is on, waits are 10 seconds, then 30, then 60. After 3 failures in a row it stops. If that run had already been up for more than 10 minutes, the count is cleared before this crash is counted. A manual start or stop in between cancels the pending restart.
- A schedule runs every day at one `HH:MM`: restart, one console command, or a backup. Times without a leading zero are stored as `HH:MM`.

### Tunnels

- Three providers: OpenFrp, SakuraFrp (provider id `natfrp` in the code), and your own frps.
- frpc is downloaded per provider into `runtimes/frpc/`. Credentials are DPAPI-encrypted in `tunnels.json` and are not returned by the API. A custom frps token is passed in the process environment, not written into the frpc config. OpenFrp and SakuraFrp still receive the credential as a command-line argument.
- A tunnel is either game or voice. A voice tunnel's public address is written to the bound instance's `voice_host`. Custom frps can be TCP or UDP. SakuraFrp and OpenFrp protocols are chosen in the provider's own console.
- A tunnel can be bound to a server and set to follow that server. If the server crash-restarts, a following tunnel comes back up. If the server stays stopped for about 5 seconds, the tunnel stops.
- The public address can be copied from the page. A domain is shown in preference to a raw address. The card shows the last start error, including a credential that can no longer be decrypted (typical after moving to another PC or Windows user).
- frpc logs are an SSE stream.

### Window, tray, and appearance

- The UI is an embedded page in a WebView2 window. Without WebView2, the system browser opens the local URL.
- The default port is 27333. If it is taken, a random port is used. The window title is "MineDeck · MC 一键开服".
- Seven schemes: Craft (default), Fluent, GitHub, Graphite, Nord, Catppuccin, Solarized. Each can be light, dark, or follow the system. Eight accent colors (blue, indigo, violet, teal, green, orange, rose, graphite), or follow the scheme. Appearance is stored in local `localStorage`, so another device on the LAN can remember its own.
- Fonts are packed in: subsets of Geist, Geist Mono, and Noto Sans SC. Characters outside the subset fall back to the system font.
- Each of the 12 server types has a pixel avatar.
- Before the page paints, the window background is the title-bar color, and a startup screen is shown. The native window stores the current title-bar color and uses it on the next launch before the page draws.
- The process declares DPI awareness. Window size follows the system scale.
- Closing the window can minimize to the tray while servers keep running. The tray menu can open the panel, or stop everything and quit. If a server is running and minimize-to-tray is off, closing asks first.
- Start with Windows is a per-user `HKCU\...\Run` value and does not need an administrator. That launch uses `-minimized`, goes to the tray, and does not activate the foreground window. A leftover AutoMCHUB autostart entry is removed.
- The first launch has an onboarding card and asks for the terms and disclaimer.
- Image files placed in a `bg` folder next to the program can be used as the UI background.
- Server and backup roots can be moved to another drive through the system folder picker. Previous roots stay in the scan list. More than one root is recognized.
- A display name keeps ASCII letters, digits, hyphens, and underscores. Spaces and dots become hyphens. If nothing remains, the directory is `server-` plus the creation time.
- **(Beta, v3.1.0-beta.1)** A background task can be opened again after changing pages or refreshing. The list is a summary; steps and the log are loaded per task. Task records for this process are cleared when MineDeck restarts.
- **(Beta, v3.1.0-beta.1)** The WebView2 window does not show the link status bar at the bottom left.
- Double-clicking the title bar maximizes or restores.
- A phone browser on the LAN page keeps the bottom clear of the browser toolbar and the safe area. Landscape uses the phone layout as well.

### LAN access, API, webhooks, and updates

- Allow LAN access requires an access password first. The server stores it as bcrypt (an older SHA-256 hex hash is upgraded after a successful login). The listen address is chosen only at startup: turning LAN on takes a restart to move from `127.0.0.1` to `0.0.0.0`; turning it off applies immediately. Turning it off or changing the password invalidates existing LAN sessions.
- Loopback requests send the `token` from the startup URL in the `X-Token` header. LAN clients exchange a password at `POST /api/login` for a session cookie.
- Picking a folder, opening Explorer, adding a local Java, adopt scan and adopt, pending import files, and AI setting changes accept loopback requests only. A LAN session cannot choose where instances or backups are stored.
- Paths, fields, and limits of the local HTTP API are in [docs/API.md](docs/API.md). The console and frpc logs are SSE.
- A webhook URL can be set. In-process events are posted. Types include `instance.start`, `instance.stop`, `instance.crash`, `player.join`, `player.leave`, `backup.done`, `tunnel.up`, and `tunnel.down`.
- The default update repository is `Trasperioooooon/MineDeck` and can be changed. A silent check can run at startup, or you can check and apply from Settings → Updates and about. Applying downloads the new build and restarts.
- GitHub file URLs are tried via `ghfast.top`, then `gh-proxy.com`, then directly. The exe and frpc must match the published SHA-256.
- A stable install is not offered a pre-release. Pre-releases do not update to each other automatically; replace `MineDeck.exe` by hand.

### AI connection

AI is optional. With no provider and key in settings, MineDeck does not contact an AI service and does not download price data for estimates.

- Two protocols: OpenAI-compatible Chat Completions, and Claude Messages.
- Seven presets: DeepSeek, Qwen (Alibaba Cloud Bailian), Kimi (Moonshot), Zhipu, OpenAI, Claude, and Ollama (local, no key). A custom base URL is separate. Default models and URLs are in `internal/ai/presets.go`.
- The key is stored on this PC with DPAPI. It is sent only to the base URL you set.
- Cost estimates use LiteLLM's public prices. Cached price data older than 7 days is downloaded again. You can enter your own unit prices, or refresh the public prices.
- Each feature can have a daily token quota.
- Changing only the path on the same site keeps the saved key. Switching site or provider asks before clearing it.
- Changing AI settings, listing models, and testing the connection accept loopback requests only. A LAN session can read settings and cannot change them.

### In-app assistant and the error banner

- The AI assistant in the title bar reads logs, config, and crash reports before it answers. Text sent to the model has local paths, LAN addresses, and secrets removed. You can view the text that was actually sent.
- Writes such as changing config, switching Java, restarting, or installing the cart produce a confirmation card first. If building the proposal fails, an empty card is not left up.
- Write requests from an external MCP client show up at the top of the assistant as well, and run only after confirmation in MineDeck.
- The banner at the top of the console does not call an AI service. Rules in `internal/diag/rules.go` run in order, 13 kinds: missing mod dependency, a client-only mod on the server, the wrong loader, a duplicate mod, mods declaring each other incompatible, a Mixin conflict, the wrong Java or an unsupported JVM flag, port in use, not enough memory, watchdog shutdown, EULA not accepted, a friend failing to join because mods do not match, and a broken config file. With AI configured, the banner can send the case to the assistant.
- UI actions, confirmed executions, schedules, and crash restarts are written under `logs\ops` next to the program. Secret fields in those records are masked.

### In-game AI

Turn it on from the In-game AI tab. Settings and the day's usage live in that instance as `gameai.json` and `gameai-state.json`. Deleting the instance deletes them.

- The default prefix is `@AI`. Replies are visible to everyone by default.
- `/私`: the reply goes only to the asker. The question itself is still visible to others.
- `/问`: answer only, do not run a command. Players without execute permission do not see this option in help.
- `/额度`: how many questions are left today.
- `/帮助`: help text built from the options this server actually has enabled.
- Defaults when those settings have not been changed: 8 seconds of cooldown per player (shared with companion orders); 60 questions per player per day; 300 per server per day (questions and companion orders together); 500000 tokens per server per day.
- More than 5 counted triggers from the same player within 1 minute (an accepted question, or a non-model reply such as help) silences that player for 3 minutes. A message blocked by cooldown or by the daily cap is not one of those 5.
- Players ticked in the panel can have the model run allowlisted commands. Templates in `internal/chatai/command.go`:
  - On by default: time, weather, teleport yourself, effects on yourself, set your own spawn, locate a structure or biome, `list`.
  - Off by default: change your own game mode, give yourself items, change difficulty, show the seed.
- `op` and `execute` are not templates and are not run. Each command's arguments are checked against that template.
- After a command, the reply waits for the server's result. Replies are split to the in-game chat width.
- The owner's settings page shows the server's tokens today, the estimate, and how many times each player asked.
- The panel can run a recognition test and can show recent Q&A.

### Music sharing

The tab is shown for NeoForge 1.20.5 and later, Forge 1.20.1 and later, and Mohist 1.20.1 and later. Fabric and Banner have no bridge mod, so the tab is hidden.

- MineDeck captures audio playing on this PC and sends it through Simple Voice Chat. Each player can lower or mute it in the voice settings.
- On Windows 11 (build 20348 or newer) the default is per-process loopback: the chosen program and its children. On Windows 10 only a device-wide loopback is available, which includes game audio and voice; the page asks for confirmation before that starts.
- Output is 48 kHz, 16-bit, mono, one frame every 20 ms, which is Simple Voice Chat's frame format.
- The source list is audio sessions on playback devices, without system sounds, ended sessions, or MineDeck itself.
- The checklist covers software and version, Simple Voice Chat, the bridge mod, and the voice port. If Simple Voice Chat is missing, it can be installed from Modrinth for that loader and version. The bridge jar ships inside MineDeck and is installed from this tab.
- With no voice tunnel running, the page says remote players will not hear it, instead of assuming that joining the game is enough.
- `voice_host` can be edited. When a voice tunnel comes up, MineDeck also writes the public address there.
- Starting or stopping a broadcast, and installing the mods, are done by a person on this PC. Quitting MineDeck stops broadcasts before it stops servers.

Bridge source is in [bridge/voicechat](bridge/voicechat/README.md). The jar packed into the program is under `internal/bridge/jar`.

### AI companions

The tab covers two mods: NeoForge 1.21.1 and Forge 1.20.1. The mod version packed into the program is 0.2.1. Source is in [companion](companion/README.md). Jars are under `internal/companion/jar`.

- A companion is a server-side entity. It does not use a player slot and does not appear in the Tab list. The default cap is 4, configurable from 1 to 16. Companions are saved with the world.
- Model calls stay in MineDeck. The mod runs fixed actions sent over the local connection.
- In-game actions: follow, collect, go to coordinates, attack, give items to a player, stop, say, and status. Spawn and remove are not chat actions. They are done on the Companions tab, or through an assistant confirmation card.
- You can set regions the companion must not dig, and mobs it must not attack. If the attack list is unset, the built-in default list is used.
- Only players authorized under In-game AI can order a companion.
- The server mod is installed from the Companions tab. PlayerEngine and Architectury API are fetched from Modrinth for the server. Friends install the companion jar from the release page on the client, at the same version as the server.
- A stopped companion floats in water. Giving items reports the count actually handed over.
- Quitting MineDeck sends end to the companion mod. Companions and their tasks remain in the world.

### MCP

Off by default. Turn it on under Settings → AI (optional) → MCP.

- It accepts connections from this PC only, and it requires a token. The token is stored encrypted and can be reset.
- An MCP client can use HTTP `/mcp`, or start `MineDeck.exe -mcp` as a stdio child. `-mcp` does not open a window. It forwards stdin and stdout to a MineDeck that is already running.
- Reading status and logs runs directly. Writes such as config changes and restarts only create a proposal, which runs after confirmation in MineDeck. Operations marked human-only are not exposed to the external tool.
- Text sent to the external tool is redacted the same way as for the in-app assistant.
- The panel can copy a config snippet for a specific tool. That tool uses its own model, not the API key stored in MineDeck.

### Operation log and local limits

UI actions and AI-initiated actions go through `internal/ops`. Each one has a parameter description and a level: read, write, danger, or human-only. Human-only operations cannot be called by AI or by a schedule.

The local API uses a token chosen at random on each launch. Folder pickers and "open path" do not accept a LAN session.

## Screenshots

These seven images are in `docs/images/`. The version in the corner of the window is v2.9.0-beta.1, from when they were captured. They do not show the v3.1.0 handover pack, mod cart, or pending import files.

<!-- TODO: screenshot of v3.1.0-beta handover (.mdpack), mod cart, and pending modpack files. Those screens are not in the repo. -->

| Server list | Console error banner |
| --- | --- |
| <img src="docs/images/01-servers.png" alt="Server list with Paper, Forge, vanilla, and Velocity cards, plus start, stop, and manage buttons"> | <img src="docs/images/02-console-banner.png" alt="Console banner: missing dependency geckolib, and a client-only mod placed on the server"> |
| In-app assistant and a confirmation card | In-game AI log |
| <img src="docs/images/03-assistant.png" alt="The assistant read the mod list, identified a client-only mod, and offered a card to disable that file"> | <img src="docs/images/04-gameai-log.png" alt="In-game AI log: weather clear was run, op was refused"> |
| AI companions | Music sharing |
| <img src="docs/images/05-companion.png" alt="Companions page: mod connected, one companion collecting, one idle, and two no-dig regions"> | <img src="docs/images/06-music.png" alt="Music sharing with NetEase Cloud Music selected and the checklist passing"> |

<img src="docs/images/07-settings-ai.png" alt="AI settings with DeepSeek enabled and a successful connection test">

## Installation

Download `MineDeck.exe` from [GitHub Releases](https://github.com/Trasperioooooon/MineDeck/releases). Latest on that page is the current stable release. [v3.1.0-beta.1](https://github.com/Trasperioooooon/MineDeck/releases/tag/v3.1.0-beta.1) and [v3.1.0-beta.2](https://github.com/Trasperioooooon/MineDeck/releases/tag/v3.1.0-beta.2), published today, are pre-releases and have to be downloaded by hand. A PC running v3.0.0 is not prompted to update to them.

A domestic mirror and the site minedeck.cn are still being prepared. The address will be added here when they are up.

1. Put `MineDeck.exe` in its own folder. Servers, downloaded Java, the cache, and backups live in that folder by default. UI preferences are in `%LocalAppData%\MineDeck`.
2. Double-click it. If Windows shows "protected your PC", choose More info → Run anyway. The executable is not code-signed.
3. The same release page includes companion-mod jars for player clients, and `SHA256SUMS`. Pick the NeoForge 1.21.1 jar or the Forge 1.20.1 jar and put it in the client's `mods`. The mod the owner installs from the Companions tab is the server-side mod. It does not replace the client jar.

Upgrading from v2.6.1 (the file was named `AutoMCHUB.exe`) cannot use in-app update to find the renamed file. Place `MineDeck.exe` in the old folder by hand. The old startup entry is removed; turn it back on under Settings → Startup and tray if you want it.

To uninstall, delete the folder that contains the exe, and `%LocalAppData%\MineDeck`.

## Quick start

1. Open the program and choose New server.
2. Pick the software, version, and build, accept the EULA, and create.
3. After create finishes, press start. `Done (...)!` in the console means the server is up.
4. Players on the same LAN connect to `this PC's LAN IP:port`. Players elsewhere add a tunnel under Tunnels, bind it to this server, turn on follow, and use the public address.

Common entry points:

- Send a command: instance → Console → the box at the bottom.
- Change config: instance → Common settings, or All config. Keys that need a restart are marked.
- Change memory or Java: instance → Memory / startup.
- No window: `MineDeck.exe -nogui`. The default port is 27333; `-port 27333` sets it. If the port is taken, a random one is used. The URL and token are printed in the log.
- The silent launch used by start-with-Windows: `MineDeck.exe -minimized`.
- Stdio bridge for a local AI tool: `MineDeck.exe -mcp`. MineDeck itself must already be running.

## FAQ

### Do I install Java myself?

No prior install is required. A matching major already on the PC is reused. Otherwise a portable runtime is downloaded in the order above, and the system Java is left as it is.

### How do players outside the LAN connect?

Add OpenFrp, SakuraFrp, or your own frps under Tunnels, bind it to this server, and turn on follow. Players on the same LAN do not need a tunnel.

### Which versions and server types are supported?

Minecraft 1.12.2 through the newest release on the manifest, including 26.x. The 12 types are listed above. Modrinth `.mrpack` and CurseForge zip files can be imported.

### Where do I look when a server fails to start?

Start with the banner at the top of the console. The 13 rules do not need AI. With AI configured, use "have AI look" on the banner, or ask in the assistant.

### Will AI change the server on its own, and who is charged?

The provider you configure charges for use. MineDeck estimates from public prices and can cap daily tokens. Reading logs runs directly. Config changes, Java switches, restarts, and mod installs show a confirmation card first. In-game commands run only for authorized players and only inside enabled templates. `op` and `execute` are not run.

### What do friends install?

AI companions: the friend installs the companion mod on the client, at the same version as the server. Music sharing: the server and the friend both install the same Simple Voice Chat version. MineDeck installs the bridge mod on the server.

### Which build should I download: stable, or today's two betas?

v3.0.0 is the current stable release. v3.1.0-beta.1 adds handover packs, the mod cart, pending files during modpack import, adopting an existing server, switching Java in the UI, and finding a background task again after leaving the page. v3.1.0-beta.2 adds no feature; it changes a few handover and CurseForge import behaviors, marked beta.2 above. Put a beta in its own folder, and back up an existing server before trying it.

### Where is the data?

By default, next to the exe: `servers`, `backups`, `runtimes`, `cache`, and `logs\ops`. UI preferences are in `%LocalAppData%\MineDeck`. After `runtimes` is deleted, existing servers are missing their portable Java until Memory / startup prepares it again.

### What else is a known limit?

- The first start of a server may raise a Windows Firewall prompt. LAN players can connect after access is allowed.
- Part of a Forge or NeoForge install still contacts official hosts for libraries. Mirrors and retries can still end in failure.
- Music sharing on Windows 10 can only capture the whole playback device.
- Paper and Purpur have no domestic mirror in this code.

## Contributing

Open a [GitHub Issue](https://github.com/Trasperioooooon/MineDeck/issues). Do not paste API keys, MCP tokens, tunnel credentials, or a CurseForge key.

Building from source on Windows needs Go 1.25 or newer (`go.mod` says 1.25.0):

```powershell
$env:GOPROXY = 'https://goproxy.cn,direct'
go build -trimpath -ldflags="-s -w -H windowsgui" -o MineDeck.exe .
```

A debug build with a console:

```powershell
go build -o minedeck-cli.exe .
.\minedeck-cli.exe -nogui
```

The UI font subsets and `rsrc_windows_amd64.syso` are already in the repo, so the build does not need Python. After UI copy changes, check the font subset: [tools/fonts/README.md](tools/fonts/README.md). After replacing `assets/icon.ico`, regenerate the syso; the command is in [design-system/MASTER.md](design-system/MASTER.md).

Layout:

- `main.go`, `tray_windows.go`, `mcpbridge.go`: process entry, tray, and the MCP stdio bridge.
- `internal/app`: data directories and global config.
- `internal/mcsrc`, `internal/java`, `internal/inst`: version lists, portable Java, and instance create and process control.
- `internal/modpack`: Modrinth and CurseForge packs.
- `internal/transfer`: `.mdpack` handover packs.
- `internal/tunnel`: tunnels.
- `internal/ops`: the operation registry, confirmation, and the operation log. Concrete operations are in `internal/ops/catalog`.
- `internal/ai`, `internal/assistant`, `internal/diag`, `internal/chatai`: AI clients, the in-app assistant, error rules, and in-game AI.
- `internal/music`, `internal/musicshare`, `internal/bridge`, `internal/companion`: capture, broadcast, the voice bridge, and the companion mod.
- `internal/web`: REST, SSE, and the embedded UI (`internal/web/ui`).
- `tools/ui-dev`: a UI simulator and interaction tests that do not need Windows.

Notes on versions and download sources: [docs/MC自动化开服技术实现方案与版本映射表.md](docs/MC自动化开服技术实现方案与版本映射表.md). Release notes are in `docs/releases/`.

## License

The program is under the [MIT License](LICENSE). Copyright (c) 2026 MineDeck Contributors.

The embedded Geist, Geist Mono, and Noto Sans SC fonts are under the [SIL Open Font License 1.1](https://openfontlicense.org/). The subsets keep the copyright and license text from the fonts, in [`internal/web/ui/fonts/`](internal/web/ui/fonts/). How the subsets were built is in [tools/fonts/README.md](tools/fonts/README.md).

Third-party notices shipped with the companion mod are in [companion/LICENSE-NOTICE.md](companion/LICENSE-NOTICE.md).
