// Package assets 放要打进程序里的静态资源。
//
// 图标文件留在这个文件夹里，没有跟着程序入口挪进 cmd/minedeck：官网的发布流程（.github/workflows/pages.yml）
// 也按 assets/icon.ico 这个位置用它，而 go:embed 够不到上级目录，所以由这个包内嵌、入口那边取用。
package assets

import _ "embed"

// IconICO 是应用图标 icon.ico 的内容：窗口、任务栏和托盘运行时用它（见 cmd/minedeck/icon_windows.go）。
// exe 文件自己的图标另由 cmd/minedeck/rsrc_windows_amd64.syso 提供。
//
//go:embed icon.ico
var IconICO []byte
