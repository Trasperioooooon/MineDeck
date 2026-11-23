package atomicfile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func longPath(path string) string {
	if strings.HasPrefix(path, `\\?\`) || strings.HasPrefix(path, `\\.\`) {
		return path
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if len(p) < 248 {
		return p
	}
	if strings.HasPrefix(p, `\\?\`) || strings.HasPrefix(p, `\\.\`) {
		return p
	}
	if strings.HasPrefix(p, `\\`) {
		return `\\?\UNC\` + p[2:]
	}
	return `\\?\` + p
}

// verbatimPath 给已是绝对路径的目录加上 \\?\ 字面前缀，避免把上层目录单独拿去打开时被 Win32 剥掉名字末尾的空格和点。
func verbatimPath(p string) string {
	if strings.HasPrefix(p, `\\?\`) || strings.HasPrefix(p, `\\.\`) {
		return p
	}
	if strings.HasPrefix(p, `\\`) {
		return `\\?\UNC\` + p[2:]
	}
	return `\\?\` + p
}

func move(tmp, path string) error {
	from, err := windows.UTF16PtrFromString(longPath(tmp))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(longPath(path))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func moveNew(tmp, path string) error {
	from, err := windows.UTF16PtrFromString(longPath(tmp))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(longPath(path))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}

// Windows 10/11 的扩展改名可忽略被替换文件的只读属性，不需要先修改任何目标属性。
// 若文件系统不支持，保存直接失败；绝不退回到按路径清除只读、先删原文件的做法。
type renameInfo struct {
	Flags     uint32
	Root      windows.Handle
	NameBytes uint32
	Name      [1]uint16
}

// 替换时目标文件只要被谁打开着（哪怕只是查一下文件名、什么权限都不要、允许别人删），
// MoveFileEx 就报拒绝访问；临时文件被打开着、又不许别人删，报共享冲突。
// 这种占用通常一眨眼就放开：本程序里不排队的读取、服务端读配置、杀毒和索引都会这样。
// 所以目标不是只读、错误是这两种时，隔一会儿再试，
// 间隔从 1 毫秒翻倍到 125 毫秒封顶，一共最多再试 10 次、加起来约半秒；还不行就照旧报错。
// 重试只再调 MoveFileEx：它本身拒绝覆盖只读文件，所以重试期间目标变成只读也不会被盖掉。
// 2026-11-14 CI 上 TestUpdatePropsKeepsEveryonesChange 偶发 Access is denied，起因是本程序自己：
// 排队的保存在进队前算锁键，那时会打开目标文件，撞上了正在替换的那一个。
// 算锁键现在一般不再打开目标（见 finalPath），这里的重试兜的是上面那几种占用，和算锁键退回老查法的少数情况。
var retryDelays = []time.Duration{
	1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond, 8 * time.Millisecond, 16 * time.Millisecond,
	32 * time.Millisecond, 64 * time.Millisecond, 125 * time.Millisecond, 125 * time.Millisecond, 125 * time.Millisecond,
}

// beforeRetry 测试用：第 attempt 次重试（从 1 数）再做一次之前调用，替换、publishNew 是再调 MoveFileEx 之前，RetryRemove 是再删之前。
var beforeRetry func(attempt int)

// retryable 失败是不是值得再试一次：错误是拒绝访问或共享冲突，而且目标不是只读文件、不是文件夹。
// 原文件原来就是只读的（old 记着）不在这里重试，交给下面的扩展改名，规矩和以前一样。
func retryable(err error, path string, old fs.FileInfo) bool {
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return false
	}
	if old != nil && old.Mode().Perm()&0o200 == 0 {
		return false
	}
	cur, e := os.Lstat(path)
	if errors.Is(e, fs.ErrNotExist) {
		return true // 目标不在，占着的是临时文件
	}
	if e != nil || cur.IsDir() || cur.Mode().Perm()&0o200 == 0 {
		return false
	}
	// 末级是目录联接或目录符号链接时 Lstat 不报 ModeDir，要看属性：改名换不掉文件夹，不用白等
	if a, ok := cur.Sys().(*syscall.Win32FileAttributeData); ok && a.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return false
	}
	return true
}

// beforeReopen 测试用：第 attempt 次重试（从 1 数）重新打开临时文件之前调用。
var beforeReopen func(attempt int)

// beforeReadOnlyRetry 测试用：第 attempt 次重试（从 1 数）只读退路改名之前调用。
var beforeReadOnlyRetry func(attempt int)

func sleepCtx(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func isBusyErr(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

// RetryRemove 执行文件或文件夹删除；碰上共享冲突或刚删完里面最后一个文件时的目录非空假象，按 retryDelays 重试。
// 不重试拒绝访问：只读属性或权限不足等半秒也不会好。
func RetryRemove(op func() error) error {
	err := op()
	for retries := 0; err != nil && retries < len(retryDelays) && isRemoveBusyErr(err); {
		time.Sleep(retryDelays[retries])
		retries++
		if beforeRetry != nil {
			beforeRetry(retries)
		}
		err = op()
	}
	return err
}

func isRemoveBusyErr(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_DIR_NOT_EMPTY)
}

// openForSync 替换之前把写好的临时文件重新打开一次，为的是把它刷到盘上。
// 刚写完关上的文件，杀毒软件、索引、同步盘常常紧接着打开来看一眼；它们打开时要是不许别人写，这一下就报共享冲突。
// 这和改名时碰到的是同一种短暂占用（见上面 retryDelays 的说明），同样隔一会儿再试，间隔和次数用同一张表。
// 以前这一下不在重试里：占着临时文件的那个程序，改名那一步等得起，重新打开这一步却当场失败，整次保存就不成了
// （2026-11-16 复审记下的存疑项，读代码属实）。只认共享冲突：拒绝访问在这里多半是权限的事，等也没用。
func openForSync(tmp string) (*os.File, error) {
	return openForSyncCtx(context.Background(), tmp)
}

func openForSyncCtx(ctx context.Context, tmp string) (*os.File, error) {
	f, err := os.OpenFile(tmp, os.O_RDWR, 0)
	for retries := 0; err != nil && retries < len(retryDelays) && errors.Is(err, windows.ERROR_SHARING_VIOLATION); {
		if cerr := sleepCtx(ctx, retryDelays[retries]); cerr != nil {
			return nil, cerr
		}
		retries++
		if beforeReopen != nil {
			beforeReopen(retries)
		}
		f, err = os.OpenFile(tmp, os.O_RDWR, 0)
	}
	return f, err
}

func replace(tmp, path string, old fs.FileInfo) error {
	return replaceCtx(context.Background(), tmp, path, old)
}

func replaceCtx(ctx context.Context, tmp, path string, old fs.FileInfo) error {
	err := move(tmp, path)
	retries := 0
	for err != nil && retries < len(retryDelays) && retryable(err, path, old) {
		if cerr := sleepCtx(ctx, retryDelays[retries]); cerr != nil {
			return cerr
		}
		retries++
		if beforeRetry != nil {
			beforeRetry(retries)
		}
		err = move(tmp, path)
	}
	if err == nil {
		return nil
	}
	if retries > 0 {
		err = fmt.Errorf("重试 %d 次仍不行：%w", retries, err)
	}
	// 目标一开始就是只读普通文件时，MoveFileEx 遇到只读目标（无论目标是否被占、怎么被占）报 ERROR_ACCESS_DENIED；
	// 放行 isBusyErr 里的 ERROR_SHARING_VIOLATION 是为了兜住「目标只读、而临时文件此刻正被短暂占着」的情形，让它也进只读退路重试。
	if !isBusyErr(err) || old == nil || !old.Mode().IsRegular() || old.Mode().Perm()&0o200 != 0 {
		return err
	}
	return replaceReadOnly(ctx, tmp, path, old, err)
}

// publishNew 只在目标不存在时把 tmp 移到 path（不带 MOVEFILE_REPLACE_EXISTING，不在目标位置预先建 0 字节占位文件）：
// 目标已存在时 MoveFileEx 报 ERROR_ALREADY_EXISTS / ERROR_FILE_EXISTS（满足 errors.Is(err, fs.ErrExist)），当场返回、不重试；
// 临时文件被短暂占着（共享冲突，或目标尚不存在时的拒绝访问）则按同一张 retryDelays 表重试。
func publishNew(tmp, path string) error {
	err := moveNew(tmp, path)
	retries := 0
	for err != nil && retries < len(retryDelays) && retryableNew(err, path) {
		time.Sleep(retryDelays[retries])
		retries++
		if beforeRetry != nil {
			beforeRetry(retries)
		}
		err = moveNew(tmp, path)
	}
	if err != nil && retries > 0 {
		return fmt.Errorf("重试 %d 次仍不行：%w", retries, err)
	}
	return err
}

func retryableNew(err error, path string) bool {
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return false
	}
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return true
	}
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		_, e := os.Lstat(path)
		return errors.Is(e, fs.ErrNotExist)
	}
	return false
}

// replaceReadOnly 用忽略只读属性的扩展改名替换原只读文件；若目标或临时文件被短暂占用（拒绝访问或共享冲突），
// 按同一张 retryDelays 间隔表重试，每次重试前都重新核对目标仍是原来的同一份文件，非占用错误照旧立即返回。
func replaceReadOnly(ctx context.Context, tmp, path string, old fs.FileInfo, moveErr error) error {
	from, e := windows.UTF16PtrFromString(longPath(tmp))
	if e != nil {
		return e
	}
	buf, e := renameInfoBuffer(path)
	if e != nil {
		return e
	}
	retries := 0
	for {
		current, e := os.Lstat(path)
		if e != nil || !os.SameFile(old, current) {
			return fmt.Errorf("只读目标已改变，没有覆盖：%w", moveErr)
		}
		h, e := windows.CreateFile(from, windows.DELETE|windows.SYNCHRONIZE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
			windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if e == nil {
			e = windows.SetFileInformationByHandle(h, windows.FileRenameInfoEx, &buf[0], uint32(len(buf)))
			if e == nil {
				verr := verifyRenamed(h, path)
				windows.CloseHandle(h)
				return verr
			}
			windows.CloseHandle(h)
		}
		if retries >= len(retryDelays) || !isBusyErr(e) {
			if retries > 0 {
				return fmt.Errorf("重试 %d 次仍不行：%w", retries, e)
			}
			return e
		}
		if cerr := sleepCtx(ctx, retryDelays[retries]); cerr != nil {
			return cerr
		}
		retries++
		if beforeReadOnlyRetry != nil {
			beforeReadOnlyRetry(retries)
		}
	}
}

// renameInfoBuffer 扩展改名（FileRenameInfoEx）要的参数：替换已有的、按 POSIX 规矩、忽略只读属性，新名字是 path。
// 名字后面要留一个结尾的空字符（make 出来就是 0）：系统换算这个路径时按以空字符结尾的字符串读，不只看 NameBytes。
// 以前没留，名字后面紧挨着的内存不是 0 时就读进一串乱码，新文件被改名成「server.properties」加乱码，
// 原文件没动，却报成功（2026-11-16 查清；完整跑一个包、内存里有旧数据时才容易碰上，单跑一条用例几百遍都碰不上）。
func renameInfoBuffer(path string) ([]byte, error) {
	name, err := windows.UTF16FromString(longPath(path))
	if err != nil {
		return nil, err
	}
	name = name[:len(name)-1]
	var header renameInfo
	offset := int(unsafe.Offsetof(header.Name))
	buf := make([]byte, offset+(len(name)+1)*2)
	info := (*renameInfo)(unsafe.Pointer(&buf[0]))
	info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS | windows.FILE_RENAME_IGNORE_READONLY_ATTRIBUTE
	info.NameBytes = uint32(len(name) * 2)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buf[offset])), len(name)), name)
	return buf, nil
}

// verifyRenamed 扩展改名报了成功以后，核对改过名的临时文件（句柄 h）现在确实就在 path 上：两边都问系统的规范路径来比。
// 2026-11-15 CI 上见过一次只读目标走这条退路报成功、内容却没变，查清是名字后面没留结尾的空字符（见 renameInfoBuffer），
// 新文件被改名到了一个带乱码的名字上。那处已经改了；这道核对是尽力而为的报警器：以后改名改错了地方，多数情况会报错而不是报成功。
// 两边都经 GetFinalPathNameByHandle 取回磁盘上的真实规范路径后按原样逐字比对（在开了区分大小写的文件夹里也能分出只差大小写的两个文件；
// 普通文件夹里两边取到的都是同一个目录项的真实大小写，同样一致）。任何一边问不到规范路径就不核对，照旧算成功。
func verifyRenamed(h windows.Handle, path string) error {
	got, gotOK := handleFinal(h, 2)
	if !gotOK {
		return nil
	}
	want, wantOK := handlePath(path, false)
	return checkRenamedPath(path, got, gotOK, want, wantOK, func() (string, bool) { return handleFinal(h, 0) })
}

func checkRenamedPath(path, got string, gotOK bool, want string, wantOK bool, display func() (string, bool)) error {
	if !gotOK || !wantOK || got == want {
		return nil
	}
	where := got
	if display != nil {
		if d, ok := display(); ok {
			where = d
		}
	}
	return fmt.Errorf("改名报告成功，但新文件没有落在 %s 上，而是在 %s，没有换上", path, where)
}

// handleFinal 一个打开着的句柄现在实际在哪：volume 为 0 是盘符形式，2 是设备路径形式（原样返回系统给的，不去前缀）。
func handleFinal(h windows.Handle, volume uint32) (string, bool) {
	buf := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volume)
		if err != nil || n == 0 {
			return "", false
		}
		if int(n) < len(buf) {
			return filepath.Clean(windows.UTF16ToString(buf[:n])), true
		}
		buf = make([]uint16, n+1)
	}
}

func canonicalPath(path string) string {
	return strings.ToUpper(canonicalEntryPath(path, finalPath))
}

// finalPath 的最后一项由 follow 决定是否跟随。锁住的是原子改名要替换的目录项，
// 所以最终文件不能跟随符号链接；其父目录里的联接仍需解析。
//
// 不跟随时（算锁键的最后一项）先不打开那个目录项本身，到上一级的目录列表里查（listedPath）。
// 以前一律用句柄把最后一项打开一次。排队等锁的人都要先算锁键，正好碰上持锁的人在改名替换这个文件时，
// 改名就被拒（Access is denied）：旧式改名替换不了还有句柄开着的文件，哪怕那个句柄只为查路径、允许删除。
// 40 个保存同时排队的测试在 CI 上就是这样偶发失败的（2026-11-15）。
//
// 目录列表里查不准的（文件不在、上一级打不开或不许列、上一级的名字末尾带空格或点、名字对不上、名字带通配符、卷根）退回原来的查法，
// 所以凡是老查法查得出的，锁键和以前一字不差：同一个文件不会因为换了查法多出一把锁。
// 退回去的那一下又会短暂打开最后一项，这些情况少见，真撞上了由 replace 的重试兜住。
func finalPath(path string, follow bool) (string, bool) {
	if follow {
		return handlePath(path, true)
	}
	if real, ok := listedPath(path); ok {
		return real, true
	}
	return handlePath(path, false)
}

// listedPath 不打开 path 本身，查它的真实路径：上一级用句柄问系统，最后一项的长名到上一级的目录列表里查。
// 目录列表只打开上一级文件夹，不会挡住别人改名替换 path。拿不准就返回 false，由调用方换别的查法。
func listedPath(path string) (string, bool) {
	parent := filepath.Dir(path)
	if parent == path {
		return "", false // 卷根没有上一级
	}
	name := filepath.Base(path)
	// 目录查询把这几个字符当通配符，会匹配到别的文件；正常文件名里不会有它们
	if strings.ContainsAny(name, `*?<>"`) {
		return "", false
	}
	// 上一级单独拿出来问系统时，系统会把它当成路径的最后一项重新规范一遍：名字末尾的空格和点被去掉
	// （「world 」变成「world」），设备名被换成设备。那样问到的是旁边另一个文件夹，拼出来的就成了另一个文件的锁键，
	// 而同一个文件换一种写法（比如带 \\?\ 的）算出来的又是对的那一把：一个文件两把锁。
	// 这种文件夹普通办法建不出来，解压工具、WSL 建得出。交给老查法，它按完整路径打开，不把上一级拆出来
	// （2026-11-15 Codex 复审指出，本机实测属实）。
	if abs, err := filepath.Abs(parent); err != nil || abs != parent {
		return "", false
	}
	dir, ok := handlePath(parent, true)
	if !ok {
		return "", false
	}
	u, err := windows.UTF16PtrFromString(longPath(path))
	if err != nil {
		return "", false
	}
	var fd windows.Win32finddata
	h, err := windows.FindFirstFile(u, &fd)
	if err != nil {
		return "", false
	}
	windows.FindClose(h)
	long := windows.UTF16ToString(fd.FileName[:])
	if !strings.EqualFold(long, name) && !strings.EqualFold(windows.UTF16ToString(fd.AlternateFileName[:]), name) {
		return "", false
	}
	return filepath.Join(dir, long), true
}

// handlePath 打开 path 问系统它的最终路径（设备路径形式）。follow 为 false 时不跟随最后一项的链接。
// 打开的句柄会挡住别人改名替换这个文件，所以要被原子替换的那个文件先用 listedPath 查，查不准才用它（见 finalPath）。
func handlePath(path string, follow bool) (string, bool) {
	target := longPath(path)
	if follow && (filepath.IsAbs(path) || strings.HasPrefix(path, `\\`)) {
		// 逐级退回解析上层文件夹时按原样字面打开，不经 Win32 剥除尾空格和尾点（否则 "a." / "a " 会误打开旁边的 "a"）。
		target = verbatimPath(path)
	}
	u, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return "", false
	}
	flags := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
	if !follow {
		flags |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}
	h, err := windows.CreateFile(u, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 2)
		if err != nil || n == 0 {
			return "", false
		}
		if int(n) < len(buf) {
			return filepath.Clean(windows.UTF16ToString(buf[:n])), true
		}
		buf = make([]uint16, n+1)
	}
}
