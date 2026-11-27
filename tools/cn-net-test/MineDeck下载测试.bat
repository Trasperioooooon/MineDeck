@echo off & set "MD_SELF=%~f0" & "%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -Command "$t=[IO.File]::ReadAllText($env:MD_SELF,[Text.Encoding]::UTF8); iex $t.Substring($t.IndexOf([char]10)+1)" & pause & exit /b
# 上面第一行给 cmd 看：用 Windows 自带的 PowerShell 读这个文件第二行往后的内容来跑。下面全是 PowerShell，可以用记事本打开逐行看。
#
# MineDeck 下载测试（脚本版本 1，2026-11-27）
# 给在中国大陆的 MineDeck 用户双击运行，结果发回给作者，用来查下载服务端、依赖库、Java 和软件更新时为什么老断。
#
# 它做的事：用 Windows 自带的 curl，像 MineDeck 平时那样去连几个下载站，
# 记下每一次连没连上、用了多久、速度多少、最后被分到了哪台下载服务器（只记服务器的名字）。
# 它不做的事：不记你的 IP、电脑名、用户名、系统版本；不读你电脑上的任何文件；
# 下载下来的数据直接丢掉，不存到硬盘上。唯一写下的文件是桌面上的结果文件，内容和窗口里显示的一样。
# 测试时这些下载站会像平时下载一样看到你的连接，这一点和你用 MineDeck 下载时没有区别。

$ScriptVer = '1'
$UA = 'MineDeck/1.0 (Minecraft server setup tool; Windows)'   # 和 MineDeck 下载时用的一样
$Curl = Join-Path $env:SystemRoot 'System32\curl.exe'
$Quick = ($env:MD_NETTEST_QUICK -eq '1')                      # 作者自己试脚本时用的省流量模式，用户不用管

Write-Host ''
Write-Host 'MineDeck 下载测试' -ForegroundColor Cyan
Write-Host '会像 MineDeck 平时那样连几个下载站，记下连没连上、速度多少、被分到了哪台下载服务器。'
Write-Host '不记你的 IP、电脑名、用户名，不读你电脑上的文件；下载的数据直接丢掉。'
Write-Host '一共会下载大约 90MB，一般几分钟到十几分钟跑完。中途想停就直接关掉这个窗口。'
Write-Host '测试前请先关掉加速器、VPN 和代理，不然测出来的不是你平时的网络。' -ForegroundColor Yellow
Write-Host ''

if (-not (Test-Path $Curl)) {
  Write-Host '这台电脑上没有 Windows 自带的 curl（Windows 10 1803 以前的版本没有），这个脚本测不了。' -ForegroundColor Red
  exit
}

# 两个可以跳过的问题：只是你自己选的答案，脚本不会去查
$ispMap = @{ '1' = '电信'; '2' = '联通'; '3' = '移动'; '4' = '广电'; '5' = '教育网或校园网'; '6' = '其他' }
$ans = Read-Host '你的宽带是哪家？ 1 电信  2 联通  3 移动  4 广电  5 教育网/校园网  6 其他  （直接回车跳过）'
$isp = $ispMap["$ans".Trim()]; if (-not $isp) { $isp = '没说' }
$ans = Read-Host '现在开着加速器、VPN 或代理吗？ y 开着  n 没开  （直接回车跳过）'
switch ("$ans".Trim().ToLower()) { 'y' { $vpn = '开着' } 'n' { $vpn = '没开' } default { $vpn = '没说' } }

$inv = [Globalization.CultureInfo]::InvariantCulture
function Num([string]$s) { $d = 0.0; if ([double]::TryParse("$s".Trim(), [Globalization.NumberStyles]::Float, $inv, [ref]$d)) { return $d } return 0.0 }
function Size([double]$b) { if ($b -ge 1MB) { return ('{0:N1} MB' -f ($b / 1MB)) } if ($b -ge 1KB) { return ('{0:N0} KB' -f ($b / 1KB)) } return ('{0:N0} B' -f $b) }

$why = @{ 6 = '域名解析失败'; 7 = '连不上'; 28 = '超时'; 35 = '加密连接没建立起来'; 56 = '传到一半连接断了'; 52 = '服务器什么都没回';
          18 = '没传完连接就断了'; 47 = '跳转太多'; 60 = '证书校验没过'; 16 = 'HTTP/2 出错'; 92 = 'HTTP/2 出错'; 55 = '发送失败'; 63 = '文件比预期大' }

$lines = New-Object System.Collections.Generic.List[string]
$all = New-Object System.Collections.Generic.List[object]
$script:n = 0

# 测一个地址。Range 给了就只要文件的那一段；CheckRange 只看服务器支不支持只要一段（支持才能断点续传）
function Test-Url([string]$Group, [string]$Name, [string]$Url, [int]$Max = 60, [string]$Range = '', [switch]$CheckRange) {
  $script:n++
  $a = @('-s', '-o', 'NUL', '--http1.1', '--ssl-no-revoke', '-L', '--max-redirs', '8', '--connect-timeout', '15', '-m', "$Max", '-A', $UA,
         '-w', '%{http_code}|%{time_connect}|%{time_starttransfer}|%{time_total}|%{size_download}|%{speed_download}|%{num_redirects}|%{time_redirect}|%{url_effective}')
  if ($Range) { $a += @('-r', $Range) }
  if ($CheckRange) { $a += @('-r', '0-1023', '--max-filesize', '1048576') }
  $a += $Url
  $out = & $Curl @a 2>$null
  $exit = $LASTEXITCODE
  $f = ("$out").Split([char[]]'|', 9)
  while ($f.Count -lt 9) { $f += '' }
  $http = $f[0].Trim(); $bytes = Num $f[4]; $speed = Num $f[5]
  $redir = if ((Num $f[6]) -gt 0) { '跳转 {0:N0} 次（{1:N2} 秒）' -f (Num $f[6]), (Num $f[7]) } else { '没跳转' }
  $finalHost = ''
  try { $finalHost = ([uri]$f[8].Trim()).Authority } catch { }   # 只留服务器名，地址后面的参数不要
  $ok = ($exit -eq 0 -and ($http -eq '200' -or $http -eq '206'))
  if ($CheckRange) {
    if ($http -eq '206') { $res = '支持断点续传'; $ok = $true }
    elseif ($http -eq '200') { $res = '不支持断点续传（回了整个文件）'; $ok = $true }
    else { $res = if ($why.ContainsKey($exit)) { $why[$exit] } elseif ($exit -ne 0) { "出错（curl 代码 $exit）" } else { "服务器回了 HTTP $http" } }
  } elseif ($ok) { $res = '成功' }
  elseif ($exit -eq 0) { $res = "服务器回了 HTTP $http" }
  elseif ($why.ContainsKey($exit)) { $res = $why[$exit] }
  else { $res = "出错（curl 代码 $exit）" }
  $line = '[{0}] {1}：{2}；HTTP {3}；连上 {4:N2} 秒；首字节 {5:N2} 秒；共 {6:N1} 秒；{7}；{8}/秒；{9}；下载服务器 {10}' -f `
    $Group, $Name, $res, $http, (Num $f[1]), (Num $f[2]), (Num $f[3]), (Size $bytes), (Size $speed), $redir, $(if ($finalHost) { $finalHost } else { '-' })
  $lines.Add($line)
  $all.Add([pscustomobject]@{ Group = $Group; Ok = $ok; Res = $res; Host = $finalHost })
  $color = if ($ok) { 'Gray' } else { 'Red' }
  Write-Host ("({0}) {1}" -f $script:n, $line) -ForegroundColor $color
}

$start = Get-Date
$big = if ($Quick) { '0-2097151' } else { '' }

Write-Host ''; Write-Host '一、软件更新会用到的地址' -ForegroundColor Cyan
$sums = 'https://github.com/Trasperioooooon/MineDeck/releases/download/v3.2.9/SHA256SUMS'
Test-Url '更新' 'GitHub 接口' 'https://api.github.com/repos/Trasperioooooon/MineDeck/releases/latest' 30
Test-Url '更新' 'GitHub 直连下载校验文件' $sums 30
Test-Url '更新' 'ghfast.top 加速' ('https://ghfast.top/' + $sums) 30
Test-Url '更新' 'gh-proxy.com 加速' ('https://gh-proxy.com/' + $sums) 30
Test-Url '更新' '官网版本信息' 'https://minedeck.cn/data/latest.json' 30
Test-Url '更新' '官网镜像下载 MineDeck.exe（约 18MB）' 'https://dl.minedeck.cn/releases/v3.2.9/MineDeck.exe' 300 $big

Write-Host ''; Write-Host '二、版本列表、Java 和各家核心的来源' -ForegroundColor Cyan
Test-Url '来源' 'BMCLAPI 版本清单' 'https://bmclapi2.bangbang93.com/mc/game/version_manifest_v2.json' 30
Test-Url '来源' 'Mojang 官方版本清单' 'https://piston-meta.mojang.com/mc/game/version_manifest_v2.json' 30
Test-Url '来源' 'BMCLAPI Forge 列表' 'https://bmclapi2.bangbang93.com/forge/minecraft/1.20.1' 30
Test-Url '来源' 'Forge 官方' 'https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json' 30
Test-Url '来源' 'NeoForge 官方 maven' 'https://maven.neoforged.net/releases/net/neoforged/neoforge/21.1.77/neoforge-21.1.77-installer.jar.sha1' 30
Test-Url '来源' 'NeoForge forgecdn' 'https://neoforged.forgecdn.net/releases/net/neoforged/neoforge/21.1.77/neoforge-21.1.77-installer.jar.sha1' 30
Test-Url '来源' 'BMCLAPI Fabric' 'https://bmclapi2.bangbang93.com/fabric-meta/v2/versions/installer' 30
Test-Url '来源' 'Fabric 官方' 'https://meta.fabricmc.net/v2/versions/installer' 30
Test-Url '来源' 'Paper 官方' 'https://fill.papermc.io/v3/projects/paper' 30
Test-Url '来源' '清华镜像 Java 目录' 'https://mirrors.tuna.tsinghua.edu.cn/Adoptium/21/jre/x64/windows/' 30
Test-Url '来源' 'Adoptium 官方 Java 接口' 'https://api.adoptium.net/v3/assets/latest/21/hotspot?os=windows&architecture=x64&image_type=jre' 30

# 三、NeoForge 1.21.1 安装器要下的依赖库里挑的 20 个（一共 135 个、约 41MB），各从 BMCLAPI 和官方拉一遍。
# M 开头的官方在 libraries.minecraft.net，N 开头的官方在 maven.neoforged.net；BMCLAPI 那边是同一个路径。
$libs = @(
  'M com/google/guava/listenablefuture/9999.0-empty-to-avoid-conflict-with-guava/listenablefuture-9999.0-empty-to-avoid-conflict-with-guava.jar',
  'N net/neoforged/javadoctor/gson-io/2.0.17/gson-io-2.0.17.jar',
  'N net/neoforged/installertools/cli-utils/2.1.2/cli-utils-2.1.2.jar',
  'N net/neoforged/JarJarMetadata/0.4.1/JarJarMetadata-0.4.1.jar',
  'N net/jodah/typetools/0.6.3/typetools-0.6.3.jar',
  'M org/apache/logging/log4j/log4j-slf4j2-impl/2.22.1/log4j-slf4j2-impl-2.22.1.jar',
  'N org/ow2/asm/asm-analysis/9.3/asm-analysis-9.3.jar',
  'N com/github/jponge/lzma-java/1.3/lzma-java-1.3.jar',
  'N org/apache/maven/maven-artifact/3.8.5/maven-artifact-3.8.5.jar',
  'N net/minecraftforge/srgutils/0.4.15/srgutils-0.4.15.jar',
  'N org/ow2/asm/asm-commons/9.7/asm-commons-9.7.jar',
  'N net/md-5/SpecialSource/1.11.0/SpecialSource-1.11.0.jar',
  'N org/ow2/asm/asm/9.5/asm-9.5.jar',
  'N org/jline/jline-reader/3.20.0/jline-reader-3.20.0.jar',
  'N commons-beanutils/commons-beanutils/1.9.3/commons-beanutils-1.9.3.jar',
  'N org/codehaus/plexus/plexus-utils/3.3.0/plexus-utils-3.3.0.jar',
  'N trove/trove/1.0.2/trove-1.0.2.jar',
  'N org/apache/commons/commons-lang3/3.8.1/commons-lang3-3.8.1.jar',
  'N org/apache/commons/commons-collections4/4.2/commons-collections4-4.2.jar',
  'N org/openjdk/nashorn/nashorn-core/15.4/nashorn-core-15.4.jar'
)
if ($Quick) { $libs = $libs[0..4] }
Write-Host ''; Write-Host '三、Forge/NeoForge 安装器要下的依赖库（从 BMCLAPI 拉一遍，再从官方拉一遍）' -ForegroundColor Cyan
foreach ($l in $libs) {
  $p = $l.Substring(2); $file = Split-Path $p -Leaf
  Test-Url '依赖库-BMCLAPI' $file ('https://bmclapi2.bangbang93.com/maven/' + $p) 60
}
foreach ($l in $libs) {
  $p = $l.Substring(2); $file = Split-Path $p -Leaf
  $base = if ($l.StartsWith('M ')) { 'https://libraries.minecraft.net/' } else { 'https://maven.neoforged.net/releases/' }
  Test-Url '依赖库-官方' $file ($base + $p) 60
}

Write-Host ''; Write-Host '四、大文件（看会不会下到一半断掉）和断点续传' -ForegroundColor Cyan
$srvB = 'https://bmclapi2.bangbang93.com/version/1.21.1/server'
$srvO = 'https://piston-data.mojang.com/v1/objects/59353fb40c36d304f2035d51e7d6e6baa98dc05c/server.jar'
Test-Url '大文件' 'BMCLAPI 原版服务端 1.21.1（约 50MB，整个下）' $srvB 300 $big
Test-Url '大文件' 'Mojang 官方原版服务端（只下前 10MB）' $srvO 120 $(if ($Quick) { '0-2097151' } else { '0-10485759' })
Test-Url '大文件' 'BMCLAPI NeoForge 安装器（约 7MB）' 'https://bmclapi2.bangbang93.com/maven/net/neoforged/neoforge/21.1.77/neoforge-21.1.77-installer.jar' 180 $big
for ($i = 1; $i -le 3; $i++) { Test-Url '续传' "BMCLAPI 原版服务端 第 $i 次" $srvB 30 -CheckRange }
Test-Url '续传' 'Mojang 官方原版服务端' $srvO 30 -CheckRange
Test-Url '续传' '官网镜像 MineDeck.exe' 'https://dl.minedeck.cn/releases/v3.2.9/MineDeck.exe' 30 -CheckRange

# 汇总：依赖库两边各成功几个、失败原因、BMCLAPI 把请求分到了哪些下载服务器
$sum = New-Object System.Collections.Generic.List[string]
foreach ($g in @('依赖库-BMCLAPI', '依赖库-官方')) {
  $rows = @($all | Where-Object { $_.Group -eq $g })
  $okN = @($rows | Where-Object { $_.Ok }).Count
  $sum.Add(('{0}：成功 {1}/{2}' -f $g, $okN, $rows.Count))
  $rows | Where-Object { -not $_.Ok } | Group-Object Res | ForEach-Object { $sum.Add(('  失败原因「{0}」{1} 次' -f $_.Name, $_.Count)) }
  $rows | Where-Object { $_.Host } | Group-Object Host | Sort-Object Count -Descending | ForEach-Object {
    $hOk = @($_.Group | Where-Object { $_.Ok }).Count
    $sum.Add(('  下载服务器 {0}：{1} 次，成功 {2} 次' -f $_.Name, $_.Count, $hOk))
  }
}
$failAll = @($all | Where-Object { -not $_.Ok }).Count
$sum.Add(('全部 {0} 项，失败 {1} 项，用时 {2:N0} 秒' -f $all.Count, $failAll, ((Get-Date) - $start).TotalSeconds))

$head = @(
  "MineDeck 下载测试结果（脚本版本 $ScriptVer$(if ($Quick) { '，省流量模式' })）",
  ('测试时间：{0}（电脑上的时间）' -f (Get-Date -Format 'yyyy-MM-dd HH:mm')),
  "宽带：$isp；加速器/VPN：$vpn",
  ''
)
$text = ($head + @('汇总') + $sum + @('', '明细') + $lines) -join "`r`n"

Write-Host ''; Write-Host '汇总' -ForegroundColor Cyan
$sum | ForEach-Object { Write-Host $_ }

$desk = [Environment]::GetFolderPath('Desktop')
if ($Quick -and $env:MD_NETTEST_OUT) { $desk = $env:MD_NETTEST_OUT }   # 作者试脚本时不往桌面写
if (-not $desk -or -not (Test-Path $desk)) { $desk = Split-Path $env:MD_SELF }
$outFile = Join-Path $desk 'MineDeck下载测试结果.txt'
$saved = $false
try { [IO.File]::WriteAllText($outFile, $text, (New-Object Text.UTF8Encoding $true)); $saved = $true } catch { }
$copied = $false
if (-not $Quick) { try { Set-Clipboard -Value $text; $copied = $true } catch { } }

Write-Host ''
if ($saved) { Write-Host '结果已经存到桌面上的「MineDeck下载测试结果.txt」，请把这个文件发给作者。' -ForegroundColor Green }
else { Write-Host '结果文件没存成，请把这个窗口里的内容截图发给作者。' -ForegroundColor Yellow }
if ($copied) { Write-Host '结果也已经复制好了，可以直接粘贴到聊天窗口里。' -ForegroundColor Green }
Write-Host '谢谢！'
