$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$d = 'D:\MineDeck-ux\return'; $port = 27482
$tok = ([regex]'token=([0-9A-Za-z_\-]+)').Match((Get-Content "$d\run.log" -Raw)).Groups[1].Value
$H = @{ 'X-Token' = $tok }
function Api($method, $path, $body) {
  $p = @{ Uri = "http://127.0.0.1:$port$path"; Method = $method; Headers = $H; TimeoutSec = 120 }
  if ($null -ne $body) { $p.Body = [Text.Encoding]::UTF8.GetBytes(($body | ConvertTo-Json -Depth 6 -Compress)); $p.ContentType = 'application/json; charset=utf-8' }
  $r = Invoke-RestMethod @p -SkipHttpErrorCheck
  if (-not $r.ok) { throw "API $method $path failed: $($r | ConvertTo-Json -Compress -Depth 5)" }
  $r.data
}
function E($n) { [uri]::EscapeDataString($n) }
function St($n) { (Api GET '/api/instances' $null | Where-Object { $_.name -eq $n }).status }
function RunUntilDone($n) {
  Api POST "/api/instances/$(E $n)/start" $null | Out-Null
  for ($i = 0; $i -lt 150; $i++) { $s = St $n; if ($s -eq 'running') { "$n running"; return }; Start-Sleep 2 }
  throw "$n did not reach running (last $s)"
}
function StopWait($n) {
  Api POST "/api/instances/$(E $n)/stop" $null | Out-Null
  for ($i = 0; $i -lt 90; $i++) { if ((St $n) -eq 'stopped') { "$n stopped"; return }; Start-Sleep 2 }
  throw "$n did not stop"
}
RunUntilDone '生存服'
Start-Sleep 5
Api POST "/api/instances/$(E '生存服')/backups" @{} | Out-Null; 'backup 1 requested'
Start-Sleep 15
Api POST "/api/instances/$(E '生存服')/command" @{ cmd = 'say hello' } | Out-Null
Api POST "/api/instances/$(E '生存服')/backups" @{} | Out-Null; 'backup 2 requested'
Start-Sleep 15
StopWait '生存服'
RunUntilDone '创造小镇'
Start-Sleep 5
StopWait '创造小镇'
(Api GET "/api/instances/$(E '生存服')/backups" $null) | ConvertTo-Json -Depth 3 -Compress
$t = Api POST '/api/tunnels' @{ provider = 'custom'; name = '朋友联机'; credential = 'ux-test-dummy-token'; serverAddr = 'frp.example.com'; serverPort = 7000; remotePort = 35565; localPort = 25565; boundInstance = '生存服'; autoStart = $false; purpose = 'game'; proto = 'tcp' }
"tunnel: $($t | ConvertTo-Json -Compress)"
