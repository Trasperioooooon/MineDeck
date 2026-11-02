# Build the MineDeck voice bridge: one jar for both NeoForge 1.21.1 and Forge 1.20.1.
# Windows counterpart of build.sh (same steps, same output). Needs JDK 17 or newer, no Gradle.
# This file is ASCII only on purpose, so Windows PowerShell 5.1 reads it correctly. See README.md.
#
# Usage:
#   .\build.ps1                     build the main jar (lowcodefml, no entry class)
#   .\build.ps1 -Fallback           also build the fallback jar (javafml + two empty entry classes)
#   .\build.ps1 -Api <jar>          SVC API jar to compile against (or set SVC_API_JAR)
#   .\build.ps1 -Out <dir>          output directory (default: build\)
#
# The SVC API is used at compile time only and never goes into the jar. Lookup order: -Api / SVC_API_JAR,
# then <cache>\voicechat-api-<version>.jar, then a download from maven.maxhenkel.de into the cache.
# The cache is .cache\ unless MINEDECK_CACHE is set.
param(
    [string]$Api = $env:SVC_API_JAR,
    [string]$Out = (Join-Path $PSScriptRoot 'build'),
    [switch]$Fallback
)

$ErrorActionPreference = 'Stop'
$apiVersion = '2.6.27'
$modVersion = '0.2.0'
$jarDate = '2026-01-01T00:00:00Z'
$cache = if ($env:MINEDECK_CACHE) { $env:MINEDECK_CACHE } else { Join-Path $PSScriptRoot '.cache' }
$utf8 = New-Object System.Text.UTF8Encoding $false

function Fail([string]$message) {
    Write-Host "ERROR: $message" -ForegroundColor Red
    exit 1
}

function Run([string]$exe, [string[]]$arguments) {
    & $exe @arguments
    if ($LASTEXITCODE -ne 0) { Fail "$exe failed (exit code $LASTEXITCODE)" }
}

foreach ($tool in 'javac', 'jar', 'javap') {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { Fail "$tool not found, please install JDK 17 or newer" }
}

# ---------- 1. SVC API ----------
if (-not $Api) {
    $Api = Join-Path $cache "voicechat-api-$apiVersion.jar"
    if (-not (Test-Path $Api)) {
        $url = "https://maven.maxhenkel.de/repository/public/de/maxhenkel/voicechat/voicechat-api/$apiVersion/voicechat-api-$apiVersion.jar"
        Write-Host "Downloading SVC API $apiVersion (compile time only): $url"
        New-Item -ItemType Directory -Force -Path $cache | Out-Null
        try {
            Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile "$Api.part"
            Invoke-WebRequest -UseBasicParsing -Uri "$url.sha1" -OutFile "$Api.sha1"
        } catch {
            Remove-Item -ErrorAction SilentlyContinue "$Api.part", "$Api.sha1"
            Fail "Could not download the SVC API. Download voicechat-api-$apiVersion.jar yourself and run .\build.ps1 -Api <path>, or build it from source with api-from-source.sh (Git Bash / WSL)."
        }
        $want = ([System.IO.File]::ReadAllText("$Api.sha1")).Trim().Substring(0, 40).ToLower()
        $got = (Get-FileHash "$Api.part" -Algorithm SHA1).Hash.ToLower()
        if ($want -ne $got) {
            Remove-Item "$Api.part"
            Fail "SVC API checksum mismatch (expected $want, got $got)"
        }
        Move-Item -Force "$Api.part" $Api
    }
}
if (-not (Test-Path $Api)) { Fail "SVC API jar not found: $Api" }
$Api = (Resolve-Path $Api).Path
Write-Host "SVC API (compile time only): $Api"

# ---------- 2. compile ----------
New-Item -ItemType Directory -Force -Path $Out | Out-Null
$Out = (Resolve-Path $Out).Path
$work = Join-Path $Out 'work'
if (Test-Path $work) { Remove-Item -Recurse -Force $work }
foreach ($d in 'main', 'stubs', 'fallback') { New-Item -ItemType Directory -Force -Path (Join-Path $work $d) | Out-Null }

$javacFlags = @('--release', '8', '-encoding', 'UTF-8', '-Xlint:all,-options', '-g')

function Sources([string]$dir) {
    $list = Join-Path $work ('sources-' + [guid]::NewGuid().ToString('N') + '.txt')
    $files = Get-ChildItem -Recurse -File -Filter '*.java' $dir | ForEach-Object { '"' + ($_.FullName -replace '\\', '/') + '"' } | Sort-Object
    [System.IO.File]::WriteAllLines($list, [string[]]$files, $utf8)
    return $list
}

$mainDir = Join-Path $work 'main'
Run 'javac' ($javacFlags + @('-cp', $Api, '-d', $mainDir, ('@' + (Sources (Join-Path $PSScriptRoot 'src\main\java')))))
Copy-Item -Recurse -Force (Join-Path $PSScriptRoot 'src\main\resources\*') $mainDir

# Pack the files in sorted order with a fixed timestamp, so the jar is byte-for-byte reproducible.
function Make-Jar([string]$dir, [string]$jarFile) {
    $manifest = Join-Path $work 'MANIFEST.txt'
    # Write Created-By ourselves, otherwise jar fills in the JDK version and a different JDK update changes the jar.
    $text = "Manifest-Version: 1.0`nCreated-By: MineDeck bridge/voicechat build script`nImplementation-Title: MineDeck Voice Bridge`nImplementation-Version: $modVersion`nImplementation-Vendor: MineDeck`n"
    [System.IO.File]::WriteAllText($manifest, $text, $utf8)
    if (Test-Path $jarFile) { Remove-Item -Force $jarFile }
    $root = (Resolve-Path $dir).Path.TrimEnd('\') + '\'
    $files = Get-ChildItem -Recurse -File $dir | ForEach-Object { $_.FullName.Substring($root.Length) -replace '\\', '/' }
    $sorted = [string[]]$files
    [System.Array]::Sort($sorted, [System.StringComparer]::Ordinal)
    $list = Join-Path $work 'files.txt'
    [System.IO.File]::WriteAllLines($list, $sorted, $utf8)
    Push-Location $dir
    try {
        Run 'jar' @('--create', "--date=$jarDate", '--file', $jarFile, '--manifest', $manifest, "@$list")
    } finally {
        Pop-Location
    }
}

$mainJar = Join-Path $Out 'minedeck-voicebridge.jar'
Make-Jar $mainDir $mainJar
Write-Host "Built: $mainJar"

if ($Fallback) {
    $stubDir = Join-Path $work 'stubs'
    $fbDir = Join-Path $work 'fallback'
    Run 'javac' ($javacFlags + @('-d', $stubDir, ('@' + (Sources (Join-Path $PSScriptRoot 'src\fallback\stubs')))))
    Copy-Item -Recurse -Force (Join-Path $mainDir '*') $fbDir
    Run 'javac' ($javacFlags + @('-cp', $stubDir, '-d', $fbDir, ('@' + (Sources (Join-Path $PSScriptRoot 'src\fallback\java')))))
    # Same two toml files, only the loader is switched to javafml (version ranges stay: Forge 47 / NeoForge 1).
    foreach ($f in 'mods.toml', 'neoforge.mods.toml') {
        $src = Join-Path $PSScriptRoot "src\main\resources\META-INF\$f"
        $text = [System.IO.File]::ReadAllText($src, $utf8)
        $new = [regex]::Replace($text, '(?m)^modLoader="lowcodefml"(\r?)$', 'modLoader="javafml"$1')
        if ($new -eq $text) { Fail "could not switch $f to javafml" }
        [System.IO.File]::WriteAllText((Join-Path $fbDir "META-INF\$f"), $new, $utf8)
    }
    $fallbackJar = Join-Path $Out 'minedeck-voicebridge-javafml.jar'
    Make-Jar $fbDir $fallbackJar
    Write-Host "Built: $fallbackJar"
}

# ---------- 3. checks ----------
function Check-Jar([string]$jarFile, [string]$extraAllowed) {
    Write-Host "== checking $(Split-Path -Leaf $jarFile)"
    $list = & jar tf $jarFile
    foreach ($need in 'META-INF/mods.toml', 'META-INF/neoforge.mods.toml', 'pack.mcmeta', 'minedeck/voicebridge/BridgePlugin.class') {
        if ($list -notcontains $need) { Fail "$need is missing from the jar" }
    }
    if ($list | Where-Object { $_ -like 'de/maxhenkel/*' }) { Fail 'the jar must not contain SVC files' }
    Write-Host '  both toml files present; no SVC files'

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [System.IO.Compression.ZipFile]::OpenRead($jarFile)
    try {
        foreach ($entry in $zip.Entries) {
            if (-not $entry.FullName.EndsWith('.class')) { continue }
            $stream = $entry.Open()
            $head = New-Object byte[] 8
            $read = 0
            while ($read -lt 8) {
                $n = $stream.Read($head, $read, 8 - $read)
                if ($n -le 0) { break }
                $read += $n
            }
            $stream.Close()
            $major = $head[6] * 256 + $head[7]
            if ($major -ne 52) { Fail "$($entry.FullName) has class version $major, not 52 (Java 8)" }
        }
    } finally {
        $zip.Dispose()
    }
    Write-Host '  every class has version 52 (Java 8)'

    if (Get-Command jdeps -ErrorAction SilentlyContinue) {
        $allowed = '^(java\.|de\.maxhenkel\.voicechat\.api(\.|$)|minedeck\.voicebridge(\.|$)' + $extraAllowed + ')'
        $deps = & jdeps -verbose:package -cp $Api $jarFile 2>&1 | ForEach-Object { "$_" } |
            Where-Object { $_ -match '^\s+\S+\s+->\s+(\S+)' } | ForEach-Object { ($_ -split '\s+')[3] } | Sort-Object -Unique
        $bad = $deps | Where-Object { $_ -notmatch $allowed }
        if ($bad) { Fail "unexpected dependencies: $($bad -join ', ')" }
        Write-Host '  jdeps: only java.*, de.maxhenkel.voicechat.api.* (and the loader @Mod annotations in the fallback jar); no Minecraft classes'
    } else {
        Write-Host '  (jdeps not found, dependency check skipped)'
    }

    $javap = & javap -v -cp $jarFile minedeck.voicebridge.BridgePlugin | Out-String
    if ($javap -notmatch [regex]::Escape('Lde/maxhenkel/voicechat/api/ForgeVoicechatPlugin;')) { Fail 'BridgePlugin has no @ForgeVoicechatPlugin annotation' }
    Write-Host '  BridgePlugin carries @ForgeVoicechatPlugin (runtime visible)'
}

Check-Jar $mainJar ''
$jars = @($mainJar)
if ($Fallback) {
    Check-Jar $fallbackJar '|net\.minecraftforge\.fml\.common$|net\.neoforged\.fml\.common$'
    $jars += $fallbackJar
}
Write-Host 'Done. SHA-256:'
foreach ($j in $jars) { Write-Host ("  {0}  {1}" -f (Get-FileHash $j -Algorithm SHA256).Hash.ToLower(), $j) }
