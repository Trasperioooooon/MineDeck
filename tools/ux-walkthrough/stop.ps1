# 关掉一份测试副本里的 MineDeck（像在托盘菜单里点「退出」一样让程序自己退出），再清掉副本里留下的进程。
# 用法：pwsh -NoProfile -File D:\MineDeck-ux\rig\stop.ps1 -Copy first|return
param([Parameter(Mandatory)][ValidateSet('first', 'return')][string]$Copy)
$ErrorActionPreference = 'Continue'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$d = "D:\MineDeck-ux\$Copy"; $mark = $d + '\'
$appPort = if ($Copy -eq 'first') { 27481 } else { 27482 }
function Get-Ours { @(Get-CimInstance Win32_Process -Filter "Name='MineDeck.exe'" | Where-Object { $_.ExecutablePath -and $_.ExecutablePath.StartsWith($mark, [StringComparison]::OrdinalIgnoreCase) }) }
function Get-Others {
  $ids = @(Get-Ours | ForEach-Object ProcessId)
  @(Get-CimInstance Win32_Process | Where-Object {
      ($_.Name -eq 'java.exe' -and ($ids -contains $_.ParentProcessId -or ($_.ExecutablePath -and $_.ExecutablePath.StartsWith($mark, [StringComparison]::OrdinalIgnoreCase)))) -or
      ($_.Name -eq 'msedgewebview2.exe' -and $_.CommandLine -and $_.CommandLine.IndexOf($mark, [StringComparison]::OrdinalIgnoreCase) -ge 0) -or
      (($_.Name -eq 'node.exe' -or $_.Name -eq 'pwsh.exe') -and $_.CommandLine -and ($_.CommandLine -match 'consolewatch\.js"? +' + $Copy + '\b' -or $_.CommandLine -match 'oswatch\.ps1"? +-Copy +' + $Copy + '\b'))
    })
}
$log = Join-Path $d 'run.log'
$tok = if (Test-Path $log) { ([regex]'token=([0-9A-Za-z_\-]+)').Match((Get-Content -LiteralPath $log -Raw)).Groups[1].Value } else { '' }
$ours = Get-Ours
if ($ours.Count -gt 0 -and $tok) {
  try { $r = Invoke-WebRequest -Uri "http://127.0.0.1:$appPort/api/shutdown" -Method POST -Headers @{ 'X-Token' = $tok } -UseBasicParsing -TimeoutSec 10 -SkipHttpErrorCheck; "asked the program to quit: HTTP $([int]$r.StatusCode)" } catch { "could not ask the program to quit: $($_.Exception.Message)" }
  for ($i = 0; $i -lt 60 -and (Get-Ours).Count -gt 0; $i++) { Start-Sleep -Milliseconds 500 }
  "program exited by itself: $((Get-Ours).Count -eq 0)"
}
for ($round = 0; $round -lt 6; $round++) {
  $left = @(Get-Others) + @(Get-Ours)
  if ($left.Count -eq 0) { break }
  foreach ($q in $left) { try { Stop-Process -Id $q.ProcessId -Force -ErrorAction Stop } catch {} }
  Start-Sleep -Milliseconds 400
}
"processes of this copy still running: $(@(Get-Ours).Count + @(Get-Others).Count)"
