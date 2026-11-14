param([string]$Step = 'all')
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$d = 'D:\MineDeck-ux\return'; $port = 27482
$tok = ([regex]'token=([0-9A-Za-z_\-]+)').Match((Get-Content "$d\run.log" -Raw)).Groups[1].Value
$H = @{ 'X-Token' = $tok }
function Api($method, $path, $body) {
  $p = @{ Uri = "http://127.0.0.1:$port$path"; Method = $method; Headers = $H; TimeoutSec = 60 }
  if ($null -ne $body) { $p.Body = [Text.Encoding]::UTF8.GetBytes(($body | ConvertTo-Json -Depth 6 -Compress)); $p.ContentType = 'application/json; charset=utf-8' }
  $r = Invoke-RestMethod @p
  if (-not $r.ok) { throw "API $method $path failed: $($r | ConvertTo-Json -Compress -Depth 5)" }
  $r.data
}
function WaitTask($id) {
  for ($i = 0; $i -lt 600; $i++) {
    $t = Api GET "/api/tasks/$id" $null
    if ($t.ended) { "task $id ended: result=$($t.result) error=$($t.error) log-tail=$(($t.log | Select-Object -Last 2) -join ' / ')"; return }
    Start-Sleep -Seconds 2
  }
  throw "task $id timeout"
}
function Create($b) { $r = Api POST '/api/instances' $b; $id = if ($r.taskId) { $r.taskId } elseif ($r.id) { $r.id } else { $r }; "create $($b.name): task $id"; WaitTask $id }
function WaitDone($name) {
  for ($i = 0; $i -lt 180; $i++) {
    $l = Get-ChildItem "$d\servers" -Directory | ForEach-Object { Join-Path $_.FullName 'logs\latest.log' } | Where-Object { Test-Path $_ }
    $insts = Api GET '/api/instances' $null
    $me = $insts | Where-Object { $_.name -eq $name }
    if ($me.state -eq 'running') { "$name running"; return }
    Start-Sleep -Seconds 2
  }
  throw "$name not running"
}

if ($Step -in 'all', 'config') {
  Api PUT '/api/config' @{ onboarded = $true; tosAccepted = $true } | Out-Null; 'config set'
}
if ($Step -in 'all', 'create') {
  Create @{ name = '创造小镇'; core = 'vanilla'; mc = '26.3'; build = ''; xmxMb = 2048; port = 25566; eula = $true; onlineMode = $false; motd = '创造模式建筑'; difficulty = 'peaceful'; gamemode = 'creative' }
  Create @{ name = '旧电脑搬来的服'; core = 'vanilla'; mc = '1.21.11'; build = ''; xmxMb = 1536; port = 25567; eula = $true; onlineMode = $false; motd = ''; difficulty = 'easy'; gamemode = 'survival' }
}
if ($Step -in 'all', 'list') { Api GET '/api/instances' $null | ForEach-Object { "$($_.name) | $($_.core) $($_.mc) | $($_.state)" } }
