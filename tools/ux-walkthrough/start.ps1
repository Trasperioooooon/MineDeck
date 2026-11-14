# 打开一份测试副本里的 MineDeck 真窗口（D:\MineDeck-ux\<Copy>），供 drive.js 操作。
# 用法：pwsh -NoProfile -File D:\MineDeck-ux\rig\start.ps1 -Copy first|return -Out <证据文件夹> [-NoGui]
# 程序用的 LOCALAPPDATA / APPDATA / TEMP 指进副本，不碰本机正在用的 MineDeck。已经开着就只报状态。
param([Parameter(Mandatory)][ValidateSet('first', 'return')][string]$Copy, [string]$Out, [switch]$NoGui)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$rig = $PSScriptRoot
$d = "D:\MineDeck-ux\$Copy"
$appPort = if ($Copy -eq 'first') { 27481 } else { 27482 }
$cdpPort = if ($Copy -eq 'first') { 9341 } else { 9342 }
$mark = $d + '\'
$stateFile = Join-Path $rig "state-$Copy.json"
if (-not $Out) { if (Test-Path $stateFile) { $Out = (Get-Content $stateFile -Raw | ConvertFrom-Json).out } else { $Out = "D:\MineDeck-ux\evidence\$Copy" } }
New-Item -ItemType Directory -Force -Path $Out | Out-Null

function Get-Ours { @(Get-CimInstance Win32_Process -Filter "Name='MineDeck.exe'" | Where-Object { $_.ExecutablePath -and $_.ExecutablePath.StartsWith($mark, [StringComparison]::OrdinalIgnoreCase) }) }

$running = Get-Ours
if ($running.Count -gt 0) {
  "already running: pid $($running[0].ProcessId)"
  $st = [ordered]@{ copy = $Copy; dir = $d; appPort = $appPort; cdpPort = $cdpPort; out = $Out; pid = $running[0].ProcessId }
  $st | ConvertTo-Json | Set-Content -LiteralPath $stateFile -Encoding utf8
  exit 0
}

foreach ($sub in 'profile\Local', 'profile\Roaming', 'profile\Temp') { New-Item -ItemType Directory -Force -Path (Join-Path $d $sub) | Out-Null }
$saved = @{ LOCALAPPDATA = $env:LOCALAPPDATA; APPDATA = $env:APPDATA; TEMP = $env:TEMP; TMP = $env:TMP }
$log = Join-Path $d 'run.log'
Remove-Item -LiteralPath $log -Force -ErrorAction SilentlyContinue
try {
  $env:LOCALAPPDATA = Join-Path $d 'profile\Local'; $env:APPDATA = Join-Path $d 'profile\Roaming'; $env:TEMP = Join-Path $d 'profile\Temp'; $env:TMP = $env:TEMP
  $argList = @('-port', "$appPort")
  if ($NoGui) { $argList += '-nogui' } else { $env:WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS = "--remote-debugging-port=$cdpPort --disable-backgrounding-occluded-windows --disable-renderer-backgrounding" }
  $p = Start-Process -FilePath (Join-Path $d 'MineDeck.exe') -ArgumentList $argList -WorkingDirectory $d -RedirectStandardError $log -RedirectStandardOutput (Join-Path $d 'out.log') -PassThru
} finally {
  foreach ($k in $saved.Keys) { Set-Item -Path "env:$k" -Value $saved[$k] }
  Remove-Item Env:\WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS -ErrorAction SilentlyContinue
}
$tok = ''
for ($i = 0; $i -lt 100 -and -not $tok; $i++) {
  Start-Sleep -Milliseconds 300
  if (Test-Path -LiteralPath $log) { $tok = ([regex]'token=([0-9A-Za-z_\-]+)').Match((Get-Content -LiteralPath $log -Raw)).Groups[1].Value }
}
if (-not $tok) { throw 'program did not print its start line' }
$st = [ordered]@{ copy = $Copy; dir = $d; appPort = $appPort; cdpPort = $cdpPort; out = $Out; pid = $p.Id }
$st | ConvertTo-Json | Set-Content -LiteralPath $stateFile -Encoding utf8
if ($NoGui) { "started without window: pid $($p.Id), port $appPort"; exit 0 }
$ver = $null
for ($i = 0; $i -lt 100 -and -not $ver; $i++) { try { $ver = Invoke-RestMethod -Uri "http://127.0.0.1:$cdpPort/json/version" -TimeoutSec 2 } catch { Start-Sleep -Milliseconds 400 } }
if (-not $ver) { throw 'window did not open its DevTools port' }
# background helpers: console log of the page, and a list of system dialogs the program opens
$pwsh = (Get-Process -Id $PID).Path
Start-Process -FilePath 'node' -ArgumentList ('"' + (Join-Path $rig 'consolewatch.js') + '"'), $Copy -WindowStyle Hidden | Out-Null
Start-Process -FilePath $pwsh -ArgumentList '-NoProfile', '-File', ('"' + (Join-Path $rig 'oswatch.ps1') + '"'), '-Copy', $Copy, '-ProcId', "$($p.Id)" -WindowStyle Hidden | Out-Null
Start-Sleep -Seconds 2
"window is up: pid $($p.Id); evidence folder $Out"
