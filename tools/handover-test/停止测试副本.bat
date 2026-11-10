@echo off
rem Stops the two MineDeck test copies under D:\MineDeck-handover-test.
rem Keep this file ASCII-only: cmd reads .bat files in the system code page.
set "PSModulePath="
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0stop.ps1"
echo.
pause
