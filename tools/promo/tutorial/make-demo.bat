@echo off
chcp 65001 >nul
rem 重新生成演示视频：录十二段（四段并行）、混配乐、编码。大约二十分钟。成片在 out 文件夹里。
cd /d "%~dp0"
echo [1/2] 录制各段
node record.js --jobs=4 || goto :fail
echo [2/2] 混配乐、编码
node assemble.js || goto :fail
echo.
echo 完成：%~dp0out\MineDeck-demo-1440p60.mp4
pause
exit /b 0
:fail
echo.
echo 出错了，看上面的提示。录制卡住时，out\fail 里有当时的画面。
pause
exit /b 1
