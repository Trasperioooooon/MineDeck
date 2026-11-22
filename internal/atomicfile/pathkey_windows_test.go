package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/sys/windows"
)

// 排队等锁的人要先算锁键，这时持锁的人可能正在改名替换同一个文件。
// 以前算锁键会用句柄打开这个文件本身，碰上改名替换，改名就被拒（Access is denied）；
// 改之前同样一边循环算锁键一边保存，500 次里本机失败 101 次、CI 上失败 288 次；
// inst 的 TestUpdatePropsKeepsEveryonesChange 在 CI 上偶发失败也是它（2026-11-15 在 CI 上加计数核实过）。
// 替换文件现在带重试，不关掉的话旧查法下这个用例也会过，所以先把重试关掉，它才抓得住回归。
func TestPathKeyDoesNotBlockReplace(t *testing.T) {
	setRetry(t, nil, nil)
	p := filepath.Join(t.TempDir(), "server.properties")
	if err := os.WriteFile(p, []byte("a=b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stop, done, started := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				PathKey(p)
				if calls.Add(1) == 1 {
					close(started)
				}
			}
		}
	}()
	<-started // 算锁键的那一路真的跑起来了再开始存
	const n = 200
	var errs []error
	for i := 0; i < n; i++ {
		if err := Write(p, []byte(fmt.Sprintf("a=%d\n", i)), 0o644); err != nil {
			errs = append(errs, err)
		}
	}
	close(stop)
	<-done
	// 算锁键的次数比保存还少，说明那一路没怎么抢到时间片，这一遍没测到东西，不能算过
	if c := calls.Load(); c < n {
		t.Fatalf("保存 %d 次期间只算了 %d 次锁键，两边没有真的并发起来", n, c)
	}
	if len(errs) > 0 {
		t.Fatalf("一边算锁键一边保存，%d 次里失败 %d 次，第一次：%v", n, len(errs), errs[0])
	}
	if got, _ := os.ReadFile(p); string(got) != fmt.Sprintf("a=%d\n", n-1) {
		t.Fatalf("最后存下的不是最后一次：%q", got)
	}
}

// 最后一项改成从目录列表里查以后，锁键必须和原来打开文件问出来的一模一样：
// 不然同一个文件会有两把锁，两边的读改写不再排队。
func TestPathKeySameAsHandleLookup(t *testing.T) {
	old := func(p string) string { return strings.ToUpper(canonicalEntryPath(p, handlePath)) }
	dir := t.TempDir()
	mkfile := func(p string) {
		t.Helper()
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(dir, "Server-Long-Name.Properties")
	mkfile(file)
	sub := filepath.Join(dir, "Sub Dir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mkfile(filepath.Join(sub, "inside.txt"))
	junction := filepath.Join(dir, "alias")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, sub).CombinedOutput(); err != nil {
		t.Fatalf("创建目录联接失败：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(junction) })
	long := filepath.Join(dir, strings.Repeat("x", 240)+".txt")
	if err := Write(long, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 名字末尾带空格、带两个点的文件夹，旁边有一个名字正常的。这种文件夹普通办法建不出来（解压工具、WSL 建得出），
	// 单独拿它的路径去问系统，末尾的空格和点会被去掉，问到的是旁边那个。
	twin := filepath.Join(dir, "Odd")
	if err := os.Mkdir(twin, 0o755); err != nil {
		t.Fatal(err)
	}
	mkfile(filepath.Join(twin, "inside.txt"))
	var odd []string
	for _, name := range []string{"Odd ", "Odd.."} {
		verbatim := `\\?\` + dir + `\` + name
		if err := os.Mkdir(verbatim, 0o755); err != nil {
			t.Logf("这个卷建不出名字是 %q 的文件夹，跳过这一项：%v", name, err)
			continue
		}
		mkfile(verbatim + `\inside.txt`)
		odd = append(odd, dir+`\`+name+`\inside.txt`)
	}

	type tc struct {
		name, path string
		exists     bool // 存在的路径必须真的问到了系统（设备路径），不能两边都退回原样的路径而碰巧相等
	}
	cases := []tc{
		{"文件", file, true},
		{"最后一项大小写不同", filepath.Join(dir, "server-long-name.PROPERTIES"), true},
		{"整条路径小写", strings.ToLower(file), true},
		{"不存在的文件", filepath.Join(dir, "missing.txt"), false},
		{"不存在的几级", filepath.Join(dir, "a", "b", "c.txt"), false},
		{"文件夹", sub, true},
		{"目录联接本身", junction, true},
		{"联接里的文件", filepath.Join(junction, "inside.txt"), true},
		{"联接里不存在的文件", filepath.Join(junction, "missing.txt"), false},
		{"很长的路径", long, true},
		{"卷根", filepath.VolumeName(dir) + `\`, true},
		{"末尾多一个点", file + ".", true},
		{"末尾多一个空格", file + " ", true},
		{"名字带星号", filepath.Join(dir, "Server*.Properties"), false},
		{"名字带问号", filepath.Join(dir, "Server-Long-Name.Propertie?"), false},
		{"默认数据流的写法", file + "::$DATA", true},
		{`带 \\?\ 前缀的写法`, `\\?\` + file, true},
		{"设备名", filepath.Join(dir, "nul"), false},
	}
	for _, p := range odd {
		cases = append(cases, tc{"上一级文件夹名末尾带空格或点", p, true}, tc{`上一级文件夹名末尾带空格或点，带 \\?\ 前缀`, `\\?\` + p, true})
	}
	// 指向同一个文件的各种写法，锁键都得是同一把
	same := []string{filepath.Join(dir, "server-long-name.PROPERTIES"), strings.ToLower(file), file + ".", file + " ", file + "::$DATA", `\\?\` + file}
	// 这几样平常的路径应该由目录列表查出来。要是悄悄都走了退路（退回打开句柄），
	// 锁键还是对的，TestPathKeyDoesNotBlockReplace 测的那个毛病却回来了。
	listed := []string{file, strings.ToLower(file), sub, junction, filepath.Join(junction, "inside.txt"), long}
	if short, ok := shortName(file); ok && !strings.EqualFold(short, file) {
		cases = append(cases, tc{"8.3 短名", short, true})
		listed = append(listed, short)
		same = append(same, short)
	} else {
		t.Log("这个卷没有给出不同的 8.3 短名，跳过短名一项")
	}
	link := filepath.Join(dir, "link.properties")
	dangling := filepath.Join(dir, "dangling.properties")
	switch err := os.Symlink(file, link); {
	case err == nil:
		if err := os.Symlink(filepath.Join(dir, "gone.properties"), dangling); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, tc{"文件符号链接本身", link, true}, tc{"指向不存在文件的符号链接", dangling, true})
		listed = append(listed, link, dangling)
	case os.Getenv("CI") == "" && errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD):
		t.Logf("账户没有创建符号链接的权限，跳过符号链接两项：%v", err)
	default:
		t.Fatal(err)
	}

	for _, c := range cases {
		got, want := PathKey(c.path), old(c.path)
		if got != want {
			t.Errorf("%s：%q 的锁键是 %q，原来是 %q", c.name, c.path, got, want)
		}
		if c.exists && !strings.HasPrefix(got, `\DEVICE\`) {
			t.Errorf("%s：%q 没有问到系统里的路径：%q", c.name, c.path, got)
		}
	}
	if _, err := os.Lstat(link); err == nil && PathKey(link) == PathKey(file) {
		t.Error("文件符号链接被跟随了：替换的是链接这个目录项，锁键不能是它指向的文件")
	}
	for _, p := range same {
		if got, want := PathKey(p), PathKey(file); got != want {
			t.Errorf("%q 和 %q 是同一个文件，锁键却不一样：%q、%q", p, file, got, want)
		}
	}
	for _, p := range odd {
		if PathKey(p) == PathKey(filepath.Join(twin, "inside.txt")) {
			t.Errorf("%q 的锁键和旁边名字正常的文件夹里那个文件一样：上一级被认成了另一个文件夹", p)
		}
		// 带 \\?\ 的写法不经过系统的规范化，指的是同一个文件
		if v := `\\?\` + p; PathKey(v) != PathKey(p) {
			t.Errorf("%q 和 %q 是同一个文件，锁键却不一样：%q、%q", p, v, PathKey(p), PathKey(v))
		}
	}
	for _, p := range listed {
		got, ok := listedPath(p)
		want, _ := handlePath(p, false)
		if !ok || got != want {
			t.Errorf("%q 应该由目录列表查出来：得到 %q（%v），打开句柄问到的是 %q", p, got, ok, want)
		}
	}
	for _, p := range []string{filepath.Join(dir, "missing.txt"), filepath.Join(dir, "Server*.Properties"), filepath.VolumeName(dir) + `\`} {
		if got, ok := listedPath(p); ok {
			t.Errorf("%q 在目录列表里查不准，应该交给老查法，却得到了 %q", p, got)
		}
	}
}

// listedPath 同时留了两道防线：
//  1. 通配符检查（* ? < > "）：replace_windows.go 第 251 行用来避免目录查询把它们当通配符匹配到别的文件。
//     因为正常卷上的文件名本身不含这五个字符，单看 listedPath 的返回值时这道防线会被后面的名字比对完全盖住，
//     这里只能靠「有没有调用 handlePath(parent, true) 去打开上一级」来认出它还在入口生效；
//  2. 名字比对（long / AlternateFileName）：拦下不含通配符、但 Win32 目录查询规范化后命中了别的目录项的写法（末尾空格、末尾点、以及未 Clean 的 "." / ".."）。
func TestListedPathTwoDefenses(t *testing.T) {
	t.Run("通配符在打开父目录前直接拦下", func(t *testing.T) {
		// 下面在 act 返回后立刻用零等待（WaitForSingleObject(ev, 0)）检查重叠连接事件：
		// 只有发起 ConnectNamedPipe 的系统线程和打开管道客户端的系统线程是同一个时，事件才保证当场已置位；
		// 协程若在两者之间被调度到别的系统线程，跨线程完成通知可能还没落定，正向对照会偶发误报失败。
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "cfg1.toml"), []byte("x=1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Overlapped 在堆上分配并一直保活到本子测试结束；取消后用 GetOverlappedResult(..., true) 等内核彻底写完再关句柄，避免协程栈搬移踩旧地址。
		var keepAlive []*windows.Overlapped
		t.Cleanup(func() { runtime.KeepAlive(keepAlive) })

		probePipeOpened := func(tag string, act func(pipeParent string)) bool {
			t.Helper()
			pipeParent := fmt.Sprintf(`\\.\pipe\minedeck\listedpath-%d-%s`, os.Getpid(), tag)
			uPipe, err := windows.UTF16PtrFromString(pipeParent)
			if err != nil {
				t.Fatal(err)
			}
			srv, err := windows.CreateNamedPipe(
				uPipe,
				windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED,
				windows.PIPE_TYPE_BYTE,
				1, 128, 128, 0, nil,
			)
			if err != nil {
				t.Fatalf("创建测试命名管道失败：%v", err)
			}
			ev, err := windows.CreateEvent(nil, 1, 0, nil)
			if err != nil {
				_ = windows.CloseHandle(srv)
				t.Fatalf("创建测试事件失败：%v", err)
			}
			ov := new(windows.Overlapped)
			ov.HEvent = ev
			keepAlive = append(keepAlive, ov)
			if err := windows.ConnectNamedPipe(srv, ov); err != nil && !errors.Is(err, windows.ERROR_IO_PENDING) {
				_ = windows.CloseHandle(ev)
				_ = windows.CloseHandle(srv)
				t.Fatalf("挂起命名管道等待失败：%v", err)
			}
			act(pipeParent)
			state, waitErr := windows.WaitForSingleObject(ev, 0)
			_ = windows.CancelIoEx(srv, ov)
			var transferred uint32
			if gerr := windows.GetOverlappedResult(srv, ov, &transferred, true); gerr != nil &&
				!errors.Is(gerr, windows.ERROR_OPERATION_ABORTED) && !errors.Is(gerr, windows.ERROR_PIPE_CONNECTED) {
				_ = windows.CloseHandle(ev)
				_ = windows.CloseHandle(srv)
				t.Fatalf("等待重叠 I/O 落定失败：%v", gerr)
			}
			_ = windows.CloseHandle(ev)
			_ = windows.CloseHandle(srv)
			if waitErr != nil {
				t.Fatalf("检查命名管道事件失败：%v", waitErr)
			}
			return state == windows.WAIT_OBJECT_0
		}

		// 正向对照 1：直接调 listedPath 内部打开上一级的 handlePath(parent, true)，确认这个动作确实会被管道察觉。
		if !probePipeOpened("ctrl-handle", func(pipeParent string) {
			_, _ = handlePath(pipeParent, true)
		}) {
			t.Fatal("正向对照失败：直接调用 handlePath(parent, true) 未触发命名管道连接事件")
		}
		// 正向对照 2：对不含通配符的子路径调 listedPath，确认 filepath.Dir / filepath.Abs 没有把 \\.\pipe\... 提前筛掉，避免下面变成空过。
		if !probePipeOpened("ctrl-listed", func(pipeParent string) {
			_, _ = listedPath(pipeParent + `\cfg1.toml`)
		}) {
			t.Fatal("正向对照失败：不含通配符的 listedPath 未走到 handlePath(parent, true)")
		}

		for idx, wc := range []string{"*", "?", "<", ">", `"`} {
			if got, ok := listedPath(filepath.Join(dir, "cfg"+wc+".toml")); ok {
				t.Fatalf("带通配符 %q 的路径不应由 listedPath 返回：%q", wc, got)
			}
			if probePipeOpened(fmt.Sprintf("wc-%d", idx), func(pipeParent string) {
				_, _ = listedPath(pipeParent + `\cfg` + wc + `.toml`)
			}) {
				t.Fatalf("通配符 %q 没有在打开 parent 前拦住：listedPath 调用了 handlePath(parent, true)", wc)
			}
		}
	})

	t.Run("名字比对拦下非通配符的失配目录项", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "Server-Long-Name.Properties")
		if err := os.WriteFile(file, []byte("a=1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		sub := filepath.Join(dir, "Sub Dir")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		// 以下写法都不含 * ? < > "（通配符防线一律放行），但 FindFirstFile 在 Win32 路径规范化后会命中别的目录项：
		// 末尾空格和点会命中去掉尾字符后的真文件；未 Clean 的 \.. 和 \. 会命中上一级或自身目录名，拼在 parent 后面会算出错误的路径。
		for _, p := range []string{file + " ", file + ".", sub + `\..`, sub + `\.`} {
			if got, ok := listedPath(p); ok {
				t.Errorf("非通配符失配路径 %q 不应由 listedPath 命中，却得到了 %q", p, got)
			}
		}
		// 注：下面两句测的是 PathKey 端到端的路径归一（lock.go 先调 filepath.Abs 会把 . 和 .. 清掉，到不了 listedPath），
		// 真正守住 listedPath 名字比对防线的是上面直接调 listedPath 的四条断言。
		if got, want := PathKey(sub+`\..`), PathKey(dir); got != want {
			t.Fatalf("未 Clean 的 .. 锁键算错：got=%q want=%q", got, want)
		}
		if got, want := PathKey(sub+`\.`), PathKey(sub); got != want {
			t.Fatalf("未 Clean 的 . 锁键算错：got=%q want=%q", got, want)
		}
	})
}

// A20：上一级是末尾带点或空格的特殊文件夹、旁边又有同名的正常文件夹（如 a. 与 a、a␠ 与 a）时，
// 文件建出来前后 PathKey 算出的锁键必须完全一致，不能退回上一级时把尾点或尾空格清掉落到旁边的正常文件夹上。
func TestPathKeyOddParentConsistentBeforeAndAfterCreate(t *testing.T) {
	dir := t.TempDir()
	normal := filepath.Join(dir, "a")
	if err := os.Mkdir(normal, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(normal, "server.properties"), []byte("normal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	normalKey := PathKey(filepath.Join(normal, "server.properties"))

	created := 0
	for _, oddName := range []string{"a.", "a..", "a "} {
		verbatimDir := `\\?\` + dir + `\` + oddName
		if err := os.Mkdir(verbatimDir, 0o755); err != nil {
			t.Logf("当前卷不支持创建目录 %q，跳过：%v", oddName, err)
			continue
		}
		created++
		t.Cleanup(func() { _ = os.RemoveAll(verbatimDir) })

		plainFile := dir + `\` + oddName + `\server.properties`
		verbatimFile := verbatimDir + `\server.properties`

		beforePlain := PathKey(plainFile)
		beforeVerbatim := PathKey(verbatimFile)
		if beforeVerbatim == normalKey {
			t.Errorf("%q 建文件前的锁键落到了旁边的正常文件夹 a：%q", oddName, beforeVerbatim)
		}

		// 不带 \\?\ 时，Win32 对单个尾点 "a.\server.properties" 本身就会解析到 "a\server.properties"；
		// 对双点 "a.." 和尾空格 "a " 则会直接落进特殊文件夹。先测 verbatimFile 排队不分裂，再测 plainFile。
		targetForQueue := plainFile
		if oddName == "a." {
			targetForQueue = verbatimFile
		}
		overlap := exerciseNamespaceWaiters(t, func() string { return PathKey(targetForQueue) }, func() {
			if err := os.WriteFile(verbatimFile, []byte("odd\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		})
		gotViaPlain, _ := os.ReadFile(plainFile)
		if string(gotViaPlain) == "odd\n" && beforePlain == normalKey {
			t.Errorf("%q 不带前缀在 Win32 下实际指向特殊目录，建文件前的锁键却落到了正常文件夹 a", oddName)
		}
		if overlap {
			t.Errorf("%q 在文件创建前后锁键分裂，排队的写入者没有串行化", oddName)
		}

		afterPlain := PathKey(plainFile)
		afterVerbatim := PathKey(verbatimFile)
		if beforePlain != afterPlain {
			t.Errorf("%q 不带前缀建文件前后锁键不一致：before=%q after=%q", oddName, beforePlain, afterPlain)
		}
		if beforeVerbatim != afterVerbatim {
			t.Errorf("%q 带 \\\\?\\ 前缀建文件前后锁键不一致：before=%q after=%q", oddName, beforeVerbatim, afterVerbatim)
		}
	}
	if created == 0 {
		t.Skip("当前卷不支持带尾点或尾空格的目录名")
	}
}

func shortName(p string) (string, bool) {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, 32768)
	n, err := windows.GetShortPathName(u, &buf[0], uint32(len(buf)))
	if err != nil || int(n) >= len(buf) {
		return "", false
	}
	return windows.UTF16ToString(buf[:n]), true
}
