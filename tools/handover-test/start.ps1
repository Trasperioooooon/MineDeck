# 交接测试：准备两份互不相干的 MineDeck 测试副本（A 和 B），不开窗口地启动起来。
# 用法：双击同目录的「启动测试副本.bat」。可以反复运行：已经在跑的副本不会重复启动，
# 地址和令牌会重新写进测试目录的 copies.json。
#
# 测试目录默认在 D:\MineDeck-handover-test，和项目目录、和你平时用的那份 MineDeck 完全分开。
# 这个文件要存成带 BOM 的 UTF-8（Windows PowerShell 5.1 不带 BOM 会把中文读成乱码）。
param(
    [string]$Root = 'D:\MineDeck-handover-test',
    [string]$Tag = 'v3.1.0-beta.1'
)
$ErrorActionPreference = 'Stop'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$repo = 'Trasperioooooon/MineDeck'
$ports = @{ A = 27411; B = 27412 }

function Say([string]$s) { Write-Host $s }

# 测试目录不能是一份真的 MineDeck：里面有 config.json 或 servers 就不动它
foreach ($mark in 'config.json', 'servers', 'MineDeck.exe') {
    if (Test-Path (Join-Path $Root $mark)) {
        throw "「$Root」看起来是一份正在用的 MineDeck（里面有 $mark），不能拿来当测试目录。换一个空文件夹。"
    }
}
foreach ($d in '', 'A', 'B', 'download', 'packs', 'work', 'results') {
    New-Item -ItemType Directory -Force -Path (Join-Path $Root $d) | Out-Null
}

# ---------- 下载发布版并核对 SHA-256 ----------
$dl = Join-Path $Root 'download'
$exe = Join-Path $dl 'MineDeck.exe'
$sums = Join-Path $dl 'SHA256SUMS'
$tagFile = Join-Path $dl 'tag.txt'
$base = "https://github.com/$repo/releases/download/$Tag"

function Fetch([string]$name, [string]$dest) {
    Say "下载 $name ……"
    & curl.exe -fsSL --retry 3 -o $dest "$base/$name"
    if ($LASTEXITCODE -ne 0) { throw "下载 $name 失败（curl 退出码 $LASTEXITCODE）。检查网络后再运行一次。" }
}
function ExeOk {
    if (-not (Test-Path $exe) -or -not (Test-Path $sums) -or -not (Test-Path $tagFile)) { return $false }
    if ((Get-Content $tagFile -Raw).Trim() -ne $Tag) { return $false }
    $want = $null
    foreach ($line in Get-Content $sums) {
        $p = $line.Trim() -split '\s+'
        if ($p.Count -ge 2 -and $p[-1].TrimStart('*') -eq 'MineDeck.exe') { $want = $p[0].ToLower() }
    }
    if (-not $want) { return $false }
    # 不用 Get-FileHash：从 PowerShell 7 的终端里间接启动 Windows PowerShell 时，它会因为继承来的模块路径而用不了
    $sha = [Security.Cryptography.SHA256]::Create()
    $fs = [IO.File]::OpenRead($exe)
    try { $got = ([BitConverter]::ToString($sha.ComputeHash($fs)) -replace '-', '').ToLower() } finally { $fs.Dispose(); $sha.Dispose() }
    return ($got -eq $want)
}
if (-not (ExeOk)) {
    Fetch 'SHA256SUMS' $sums
    Fetch 'MineDeck.exe' $exe
    [IO.File]::WriteAllText($tagFile, $Tag)
    if (-not (ExeOk)) { throw "下载回来的 MineDeck.exe 和 SHA256SUMS 对不上，不继续。" }
}
Say "MineDeck $Tag 已就绪，SHA-256 核对通过。"

# ---------- 启动两份副本 ----------
$copies = [ordered]@{}
foreach ($k in 'A', 'B') {
    $dir = Join-Path $Root $k
    $target = Join-Path $dir 'MineDeck.exe'
    $out = Join-Path $dir 'start.out.log'
    $err = Join-Path $dir 'start.err.log'
    $running = @(Get-CimInstance Win32_Process -Filter "Name='MineDeck.exe'" | Where-Object { $_.ExecutablePath -eq $target })
    if ($running.Count -gt 0) {
        $procId = $running[0].ProcessId
        Say "副本 $k 已经在运行（进程 $procId），不重复启动。"
    } else {
        Copy-Item $exe $target -Force
        Remove-Item $out, $err -ErrorAction SilentlyContinue
        $p = Start-Process -FilePath $target -ArgumentList @('-nogui', '-port', "$($ports[$k])") -WorkingDirectory $dir `
            -RedirectStandardOutput $out -RedirectStandardError $err -WindowStyle Hidden -PassThru
        $procId = $p.Id
        Say "副本 $k 已启动（进程 $procId）。"
    }
    # 启动日志里有一行带令牌的地址；端口被占用时 MineDeck 会自己换一个，所以以日志为准
    $url = $null; $token = $null
    for ($i = 0; $i -lt 80 -and -not $url; $i++) {
        foreach ($f in $out, $err) {
            if (-not (Test-Path $f)) { continue }
            $m = Select-String -Path $f -Pattern 'http://127\.0\.0\.1:(\d+)/\?token=([0-9A-Za-z_\-]+)' | Select-Object -First 1
            if ($m) {
                $url = 'http://127.0.0.1:' + $m.Matches[0].Groups[1].Value
                $token = $m.Matches[0].Groups[2].Value
            }
        }
        if (-not $url) { Start-Sleep -Milliseconds 500 }
    }
    if (-not $url) {
        throw "副本 $k 启动后 40 秒内没在日志里看到地址。看一下 $out 和 $err；如果它是很早以前启动的，先双击「停止测试副本.bat」再来。"
    }
    $copies[$k] = [ordered]@{ url = $url; token = $token; dir = $dir; pid = $procId }
    # 双击就能在浏览器里打开这份副本的界面（填 CurseForge Key 时用）
    $shortcut = "[InternetShortcut]`r`nURL=$url/?token=$token`r`n"
    [IO.File]::WriteAllText((Join-Path $Root "打开副本${k}的界面.url"), $shortcut, [Text.Encoding]::ASCII)
}
$json = ($copies | ConvertTo-Json -Depth 4)
[IO.File]::WriteAllText((Join-Path $Root 'copies.json'), $json, (New-Object Text.UTF8Encoding $false))

# ---------- 把任务说明和测试脚本放进测试目录（每次都用项目里的最新版覆盖） ----------
foreach ($f in '任务说明.md', '给测试AI的提示词.txt', 'mdtest.py', 'handover.py', 'cfpack.py', 'run.py') {
    $src = Join-Path $here $f
    if (Test-Path $src) { Copy-Item $src (Join-Path $Root $f) -Force }
}

Say ''
Say '两份测试副本都在运行：'
foreach ($k in 'A', 'B') { Say ("  副本 {0}：{1}（数据在 {2}）" -f $k, $copies[$k].url, $copies[$k].dir) }
Say ''
Say "测试目录：$Root"
Say '  给测试AI的提示词.txt  整段复制给测试 AI 的开场白'
Say '  任务说明.md        给测试 AI 看的任务说明'
Say '  handover.py 等     测试 AI 要运行的脚本'
Say '  copies.json        两份副本的地址和令牌（只在这台电脑上有用，不要发给别人）'
Say '  打开副本A的界面.url  双击在浏览器里打开副本 A（填 CurseForge Key 用）'
Say '  packs\             CurseForge 整合包的 zip 放这里'
Say '  results\           测试 AI 把结果写在这里'
Say ''
Say '测完以后双击「停止测试副本.bat」。'
