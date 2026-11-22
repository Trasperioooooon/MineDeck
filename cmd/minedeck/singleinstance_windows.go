package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	alreadyRunningMsg = "这个文件夹里的 MineDeck 已经开着了。如果刚关掉它，等几秒再打开。"

	_MB_OK              = 0x00000000
	_MB_ICONINFORMATION = 0x00000040
	_MB_SETFOREGROUND   = 0x00010000
)

var (
	pMessageBox = user32t.NewProc("MessageBoxW")

	// 测试里替换：互斥量名字算法、占住时最多等多久（默认等 3 秒，兜住刚从托盘退出马上又双击时的收尾）、提示框及标准错误输出。
	singleInstanceMutexName = defaultSingleInstanceMutexName
	singleInstanceWait      = 3 * time.Second
	singleInstanceAlert     = showSingleInstanceBox
	singleInstanceStderr    = io.Writer(os.Stderr)
)

// defaultSingleInstanceMutexName 按程序所在文件夹算命名互斥量名：先解开联接和短名再统一大写取 SHA-256，
// 对象名里不直接放路径和用户名；不同文件夹各放一份 MineDeck 时哈希不同，互不阻挡。
func defaultSingleInstanceMutexName(dir string) string {
	resolved := dir
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		resolved = r
	} else if abs, err := filepath.Abs(dir); err == nil {
		resolved = abs
	}
	norm := strings.ToUpper(filepath.Clean(resolved))
	sum := sha256.Sum256([]byte(norm))
	return `Local\MineDeck-` + hex.EncodeToString(sum[:16])
}

func showSingleInstanceBox(msg string) {
	text, err1 := windows.UTF16PtrFromString(msg)
	title, err2 := windows.UTF16PtrFromString("MineDeck")
	if err1 != nil || err2 != nil {
		return
	}
	pMessageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), _MB_OK|_MB_ICONINFORMATION|_MB_SETFOREGROUND)
}

// ensureSingleInstance 尝试占住 dir 对应的单实例互斥量；拿不到且等满超时后写标准错误并返回 ok=false。
// quiet 为 true（带 -nogui 或 -minimized，如无人值守脚本、计划任务、开机自启）时不弹系统提示框，避免无人点击挂起或打扰。
func ensureSingleInstance(dir string, quiet bool) (release func(), ok bool) {
	rel, acquired, err := acquireSingleInstance(dir)
	if err != nil {
		log.Println("单实例检查创建互斥量失败，继续启动:", err)
		return func() {}, true
	}
	if !acquired {
		_, _ = fmt.Fprintln(singleInstanceStderr, alreadyRunningMsg)
		if !quiet {
			singleInstanceAlert(alreadyRunningMsg)
		}
		return nil, false
	}
	return rel, true
}

// acquireSingleInstance 在独占锁定的系统线程上创建并等待命名互斥量：
// Windows 互斥量归属到具体系统线程（同一线程重复等待会递归放行，跨线程 ReleaseMutex 会失败），
// 固定在独立系统线程上既保证退出释放归属正确，也让同一测试进程里模拟两次获取时互不递归放行。
func acquireSingleInstance(dir string) (func(), bool, error) {
	name := singleInstanceMutexName(dir)
	wait := singleInstanceWait

	type result struct {
		rel func()
		ok  bool
		err error
	}
	resCh := make(chan result, 1)

	go func() {
		runtime.LockOSThread()
		uName, err := windows.UTF16PtrFromString(name)
		if err != nil {
			runtime.UnlockOSThread()
			resCh <- result{err: err}
			return
		}
		h, err := windows.CreateMutex(nil, false, uName)
		if h == 0 {
			runtime.UnlockOSThread()
			resCh <- result{err: err}
			return
		}
		var waitMS uint32
		if wait > 0 {
			ms := wait.Milliseconds()
			if ms > 0xFFFFFFFE {
				waitMS = 0xFFFFFFFE
			} else {
				waitMS = uint32(ms)
			}
		}
		state, werr := windows.WaitForSingleObject(h, waitMS)
		switch state {
		case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
			// WAIT_ABANDONED 表示上一份进程未显式 ReleaseMutex 就退出了（例如强退或更新退出），内核已把所有权移交本线程。
			releaseCh := make(chan struct{})
			doneCh := make(chan struct{})
			var once sync.Once
			rel := func() {
				once.Do(func() {
					close(releaseCh)
					<-doneCh
				})
			}
			resCh <- result{rel: rel, ok: true}
			<-releaseCh
			_ = windows.ReleaseMutex(h)
			_ = windows.CloseHandle(h)
			runtime.UnlockOSThread()
			close(doneCh)
		case uint32(windows.WAIT_TIMEOUT):
			_ = windows.CloseHandle(h)
			runtime.UnlockOSThread()
			resCh <- result{ok: false}
		default:
			_ = windows.CloseHandle(h)
			runtime.UnlockOSThread()
			resCh <- result{err: werr}
		}
	}()

	r := <-resCh
	return r.rel, r.ok, r.err
}
