# 后台盯着测试程序开出的系统窗口（选择文件夹对话框之类），把当前可见的顶层窗口写进 state-<Copy>-oswin.json。
param([Parameter(Mandatory)][string]$Copy, [Parameter(Mandatory)][int]$ProcId)
$ErrorActionPreference = 'Continue'
Add-Type -TypeDefinition @'
using System; using System.Text; using System.Collections.Generic; using System.Runtime.InteropServices;
public static class OsWin {
  public delegate bool EnumProc(IntPtr h, IntPtr l);
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc f, IntPtr l);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr h, StringBuilder s, int n);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L, T, R, B; }
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  public static List<string> List(uint want) {
    var res = new List<string>();
    EnumWindows((h, l) => {
      uint pid; GetWindowThreadProcessId(h, out pid);
      if (pid == want && IsWindowVisible(h)) {
        var t = new StringBuilder(512); GetWindowText(h, t, 512);
        var c = new StringBuilder(256); GetClassName(h, c, 256);
        RECT r; GetWindowRect(h, out r);
        res.Add(h.ToInt64() + "\t" + c.ToString() + "\t" + t.ToString() + "\t" + (r.R - r.L) + "x" + (r.B - r.T));
      }
      return true; }, IntPtr.Zero);
    return res;
  }
}
'@
$out = Join-Path $PSScriptRoot "state-$Copy-oswin.json"
while ($true) {
  if (-not (Get-Process -Id $ProcId -ErrorAction SilentlyContinue)) { Remove-Item -LiteralPath $out -ErrorAction SilentlyContinue; break }
  $rows = @([OsWin]::List([uint32]$ProcId) | ForEach-Object { $f = $_ -split "`t"; [ordered]@{ hwnd = [int64]$f[0]; cls = $f[1]; title = $f[2]; size = $f[3] } })
  $mf = Join-Path $PSScriptRoot "state-$Copy-main.txt"
  $big = $rows | Where-Object { $_.cls -ne '#32770' -and $_.size -match '^(\d+)x(\d+)$' -and [int]$Matches[1] -ge 800 } | Select-Object -First 1
  if ($big) { $cur = if (Test-Path $mf) { (Get-Content $mf -Raw).Trim() } else { '' }; if ($cur -ne "$($big.hwnd)") { Set-Content -LiteralPath $mf -Value $big.hwnd -NoNewline } }
  $json = ConvertTo-Json -InputObject @($rows) -Depth 3
  try { [IO.File]::WriteAllText($out + '.tmp', $json, [Text.UTF8Encoding]::new($false)); Move-Item -LiteralPath ($out + '.tmp') -Destination $out -Force } catch {}
  Start-Sleep -Milliseconds 400
}
