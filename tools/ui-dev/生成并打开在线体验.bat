@echo off
chcp 65001 >nul
title MineDeck 在线体验（静态版）
cd /d "%~dp0"
where node >nul 2>nul
if errorlevel 1 (
  echo 没找到 Node.js。请先装一个 18 或更新的版本：https://nodejs.org/
  echo.
  pause
  exit /b 1
)
echo 正在生成静态版的在线体验，生成完浏览器会自己打开。
echo.
node proto-static.js --open
echo.
echo 产物在这个文件夹下的 proto-dist 里。以后想再看，直接双击里面的 index.html 就行，不用再运行这个脚本；
echo 界面改过以后想看新的，再双击一次这个脚本重新生成。
echo.
pause
