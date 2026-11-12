@echo off
chcp 65001 >nul
title MineDeck 界面原型
cd /d "%~dp0"
where node >nul 2>nul
if errorlevel 1 (
  echo 没找到 Node.js。请先装一个 18 或更新的版本：https://nodejs.org/
  echo.
  pause
  exit /b 1
)
echo 正在启动界面原型，浏览器马上会自己打开。
echo 界面是真的，数据是编的：可以随便点，不会真的下载和开服。
echo.
node proto.js 27391 --open
echo.
pause
