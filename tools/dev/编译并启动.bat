@echo off
chcp 65001 >nul
setlocal
title MineDeck 开发版
rem 把仓库里现在的源码编到仓库外面的文件夹，再从那里启动。
rem MineDeck 把 exe 所在的文件夹当数据目录（服务器、备份、Java、配置都放在那里），
rem 所以开发时编出来的程序不要放在源码文件夹里运行。
rem 用法：双击，编到和仓库并排的 MineDeck-dev 文件夹；
rem       或者把一个文件夹拖到这个文件上，编到那个文件夹里。

for %%I in ("%~dp0..\..") do set "REPO=%%~fI"
if "%~1"=="" (
  for %%I in ("%REPO%\..\MineDeck-dev") do set "OUT=%%~fI"
) else (
  set "OUT=%~f1"
)

rem 不许编进仓库里面：把仓库路径从输出路径里去掉，变了就说明在仓库里
set "CHK=%OUT%\"
call set "REST=%%CHK:%REPO%\=%%"
if /i not "%REST%"=="%CHK%" (
  echo 输出文件夹 %OUT% 在仓库里面。请换一个仓库外面的文件夹。
  echo.
  pause
  exit /b 1
)

where go >nul 2>nul
if errorlevel 1 (
  echo 没找到 Go。请先装 Go 1.25 或更新的版本：https://go.dev/dl/
  echo.
  pause
  exit /b 1
)
if not defined GOPROXY set "GOPROXY=https://goproxy.cn,direct"
if not exist "%OUT%" mkdir "%OUT%"

echo 正在把 %REPO% 的源码编到 %OUT% ……
pushd "%REPO%"
go build -o "%OUT%\minedeck-dev.exe" ./cmd/minedeck
if errorlevel 1 (
  popd
  echo.
  echo 编译没有通过，上面是报错。
  pause
  exit /b 1
)
popd

rem 界面偏好和 WebView2 的数据放进这个文件夹，不和平时用的 MineDeck 混在一起。
rem 必须在编译之后再改：Go 的编译缓存也放在 LOCALAPPDATA 里。
set "LOCALAPPDATA=%OUT%\_profile\Local"
set "APPDATA=%OUT%\_profile\Roaming"
if not exist "%LOCALAPPDATA%" mkdir "%LOCALAPPDATA%"
if not exist "%APPDATA%" mkdir "%APPDATA%"

echo.
echo 编好了，正在启动。这个黑窗口里是程序的日志，关掉它程序也会退出。
echo 数据目录：%OUT%
echo 提醒：开发版和平时用的 MineDeck 共用「开机自动启动」那一项，开发版里别去动那个开关。
echo.
cd /d "%OUT%"
"%OUT%\minedeck-dev.exe" %2 %3 %4 %5
echo.
echo 程序已退出。
pause
