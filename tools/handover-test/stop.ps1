# 交接测试：把两份测试副本停掉（走它们自己的关闭接口，连同里面开着的服务器一起正常停）。
# 用法：双击同目录的「停止测试副本.bat」。只碰测试目录里的那两份，不碰你平时用的 MineDeck。
# 这个文件要存成带 BOM 的 UTF-8。
param(
    [string]$Root = 'D:\MineDeck-handover-test'
)
$ErrorActionPreference = 'Stop'
function Say([string]$s) { Write-Host $s }

$f = Join-Path $Root 'copies.json'
$copies = $null
if (Test-Path $f) { $copies = Get-Content $f -Raw -Encoding UTF8 | ConvertFrom-Json }

foreach ($k in 'A', 'B') {
    $target = Join-Path (Join-Path $Root $k) 'MineDeck.exe'
    $alive = @(Get-CimInstance Win32_Process -Filter "Name='MineDeck.exe'" | Where-Object { $_.ExecutablePath -eq $target })
    if ($alive.Count -eq 0) { Say "副本 $k 没在运行。"; continue }
    if ($copies -and $copies.$k) {
        try {
            Invoke-RestMethod -Method Post -Uri ($copies.$k.url + '/api/shutdown') -Headers @{ 'X-Token' = $copies.$k.token } -TimeoutSec 30 | Out-Null
        } catch { }
    }
    for ($i = 0; $i -lt 90; $i++) {
        $alive = @(Get-CimInstance Win32_Process -Filter "Name='MineDeck.exe'" | Where-Object { $_.ExecutablePath -eq $target })
        if ($alive.Count -eq 0) { break }
        Start-Sleep -Seconds 1
    }
    if ($alive.Count -gt 0) {
        # 关闭接口没让它退出：只结束这一个进程（按程序所在位置认，不按名字一锅端）
        foreach ($p in $alive) { Stop-Process -Id $p.ProcessId -Force }
        Say "副本 $k 没有自己退出，已结束它的进程。"
    } else {
        Say "副本 $k 已停止。"
    }
}

# 测试副本开的服务器是 java 进程：正常关闭时会跟着停。这里只查看、不结束。
$left = @(Get-CimInstance Win32_Process -Filter "Name='java.exe'" | Where-Object {
        ($_.ExecutablePath -and $_.ExecutablePath.StartsWith($Root, [StringComparison]::OrdinalIgnoreCase)) -or
        ($_.CommandLine -and $_.CommandLine.IndexOf($Root, [StringComparison]::OrdinalIgnoreCase) -ge 0) })
if ($left.Count -gt 0) {
    Say ''
    Say '下面这些 java 进程看起来是测试副本留下的，还没退出（这里不自动结束，请确认后自己处理）：'
    foreach ($p in $left) { Say ("  进程 {0}：{1}" -f $p.ProcessId, $p.ExecutablePath) }
}
