@echo off
rem Starts the two MineDeck test copies (A and B) under D:\MineDeck-handover-test.
rem Keep this file ASCII-only: cmd reads .bat files in the system code page.
rem PSModulePath is cleared so Windows PowerShell does not pick up PowerShell 7 modules
rem when this file is launched from a PowerShell 7 terminal.
set "PSModulePath="
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0start.ps1"
echo.
pause
