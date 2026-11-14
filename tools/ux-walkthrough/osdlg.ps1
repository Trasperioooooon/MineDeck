# 操作测试程序开出的系统窗口（比如「选择文件夹」对话框），以及把缩到托盘的主窗口重新显示出来。
# 由 drive.js 调用：-Action shot|pick|cancel|showmain -Copy first|return [-Path <文件夹>] [-File <截图路径>] [-Hwnd <句柄>]
param([Parameter(Mandatory)][string]$Action, [Parameter(Mandatory)][string]$Copy, [string]$Path, [string]$File, [int64]$Hwnd)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes, System.Drawing
Add-Type -TypeDefinition @'
using System; using System.Text; using System.Runtime.InteropServices;
public static class OsDlg {
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint f);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L, T, R, B; }
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int c);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr SendMessage(IntPtr h, uint m, IntPtr w, string l);
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern IntPtr GetDlgItem(IntPtr h, int id);
}
'@
function Shot([IntPtr]$h, [string]$file) {
  $r = New-Object OsDlg+RECT; [OsDlg]::GetWindowRect($h, [ref]$r) | Out-Null
  $w = [Math]::Max(1, $r.R - $r.L); $ht = [Math]::Max(1, $r.B - $r.T)
  $bmp = [System.Drawing.Bitmap]::new($w, $ht); $g = [System.Drawing.Graphics]::FromImage($bmp)
  $dc = $g.GetHdc(); [OsDlg]::PrintWindow($h, $dc, 2) | Out-Null; $g.ReleaseHdc($dc); $g.Dispose()
  $bmp.Save($file, [System.Drawing.Imaging.ImageFormat]::Png); $bmp.Dispose()
}
$rig = $PSScriptRoot
$st = Get-Content (Join-Path $rig "state-$Copy.json") -Raw | ConvertFrom-Json
$wins = @()
$wf = Join-Path $rig "state-$Copy-oswin.json"
if (Test-Path $wf) { $wins = @(Get-Content $wf -Raw | ConvertFrom-Json) }
$dialogs = @($wins | Where-Object { $_.cls -eq '#32770' })
function Dlg { if ($Hwnd) { [IntPtr]$Hwnd } elseif ($dialogs.Count -gt 0) { [IntPtr]$dialogs[0].hwnd } else { throw '程序现在没有开着系统对话框' } }
switch ($Action) {
  'shot' { $h = Dlg; Shot $h $File; "saved $File" }
  'pick' {
    $h = Dlg
    $edit = [OsDlg]::GetDlgItem($h, 1152)
    if ($edit -eq [IntPtr]::Zero) { throw '在对话框里找不到填写路径的输入框' }
    [OsDlg]::SendMessage($edit, 0x000C, [IntPtr]::Zero, $Path) | Out-Null   # WM_SETTEXT
    Start-Sleep -Milliseconds 300
    $ok = [OsDlg]::GetDlgItem($h, 1)
    if ($ok -eq [IntPtr]::Zero) { throw '找不到确定按钮' }
    [OsDlg]::PostMessage($ok, 0x00F5, [IntPtr]::Zero, [IntPtr]::Zero) | Out-Null   # BM_CLICK
    Start-Sleep -Milliseconds 800
    "filled in $Path and pressed the dialog's confirm button"
  }
  'cancel' {
    $h = Dlg
    $c = [OsDlg]::GetDlgItem($h, 2)
    if ($c -ne [IntPtr]::Zero) { [OsDlg]::PostMessage($c, 0x00F5, [IntPtr]::Zero, [IntPtr]::Zero) | Out-Null; 'pressed the dialog cancel button' } else { [OsDlg]::PostMessage($h, 0x10, [IntPtr]::Zero, [IntPtr]::Zero) | Out-Null; 'closed the dialog window' }
    Start-Sleep -Milliseconds 800
  }
  'showmain' {
    $mf = Join-Path $rig "state-$Copy-main.txt"
    if (-not (Test-Path $mf)) { throw '还没记下主窗口' }
    $h = [IntPtr][int64](Get-Content $mf -Raw).Trim()
    [OsDlg]::ShowWindow($h, 9) | Out-Null; [OsDlg]::ShowWindow($h, 5) | Out-Null; [OsDlg]::SetForegroundWindow($h) | Out-Null
    'main window shown again (same as clicking the tray icon)'
  }
}
