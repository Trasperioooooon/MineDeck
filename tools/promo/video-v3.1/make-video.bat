@echo off
chcp 65001 >nul
rem MineDeck v3.1.0 宣传片：重拍素材、逐帧渲染、编码、合配乐、出封面。双击运行，大约 15 分钟。
rem 需要：Node 18+、ffmpeg 在 PATH 里、tools\ui-dev\node_modules 里的 Playwright、本机装好的 Google Chrome。
setlocal
set "HERE=%~dp0"
set "NODE_PATH=%HERE%..\..\ui-dev\node_modules"
cd /d "%HERE%"

echo [1/6] 检查上屏文字
node check-text.js || goto :fail
echo [2/6] 在仿真后端上拍界面截图
node capture.js || goto :fail
echo [3/6] 逐帧渲染（5040 帧）
node render.js || goto :fail
echo [4/6] 编码、验证、出联系表
node encode.js || goto :fail
echo [5/6] 合配乐
node music.js || goto :fail
echo [6/6] 出两张封面
node cover.js || goto :fail

echo.
echo 完成。视频在 %HERE%out\
pause
exit /b 0

:fail
echo.
echo 出错了，往上翻看是哪一步。
pause
exit /b 1
