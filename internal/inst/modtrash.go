package inst

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 插件 / 模组页「已安装」列表里的「删除」（可用性走查 R10）：不真的删，移进服务器文件夹里的 .minedeck/已删除/<mods|plugins>/，
// 想要回来就从那里挪回原来的文件夹。选这里而不是 Windows 回收站：回收站要调外壳接口，服务器放在移动硬盘、网络盘上时
// 系统会直接永久删除、不进回收站；放在服务器自己的文件夹里哪种盘都一样能找回，而且打开文件夹就看得到。
// .minedeck 是 MineDeck 自己的文件夹（见 control.go 的 controlDirs）：交接包不带它，别人给的包也写不进来，
// 所以删掉的东西不会跟着交接包传给别人，也不会被服务器当成插件或模组加载。

// modTrashWhy 删除插件或模组时占位用的说法（见 BeginWorldChangeInstance）：这期间不能启动、换地图。
const modTrashWhy = "删除插件或模组"

// ModTrashDir 删掉的插件或模组放在哪（相对服务器文件夹，斜杠分隔），下面再按原来的文件夹分 mods、plugins。
const ModTrashDir = ".minedeck/已删除"

// TrashMod 把这台服务器 sub（mods 或 plugins）文件夹里的 file 移进 ModTrashDir/<sub>/，返回移到的位置（相对服务器文件夹）。
// file 只能是那个文件夹里的一个 .jar 或 .jar.disabled 文件名（就是模组清单里列出来的那些）；服务器必须停着，
// 运行中这些文件被服务器占着，而且停用、删除都要重启才算数。同名的已经删过一次时，新的这份加上「 (2)」这样的编号，不覆盖旧的。
func (m *Manager) TrashMod(name, sub, file string) (string, error) {
	i, err := m.Get(name)
	if err != nil {
		return "", err
	}
	if sub != "mods" && sub != "plugins" {
		return "", fmt.Errorf("只能删除 mods 或 plugins 文件夹里的文件")
	}
	f := strings.TrimSpace(file)
	// 只认一段文件名：带路径的、Windows 的设备名和备用数据流（a.jar:x）、结尾有点或空格的（Windows 会悄悄去掉，指到别的文件）都不认
	if f == "" || f != file || f != filepath.Base(f) || strings.ContainsAny(f, `/\:`) || !filepath.IsLocal(f) || strings.TrimRight(f, ". ") != f {
		return "", fmt.Errorf("文件名不对：要的是 %s 文件夹里的一个文件名", sub)
	}
	low := strings.ToLower(f)
	ext := ""
	switch {
	case strings.HasSuffix(low, ".jar.disabled"):
		ext = f[len(f)-len(".jar.disabled"):]
	case strings.HasSuffix(low, ".jar"):
		ext = f[len(f)-len(".jar"):]
	default:
		return "", fmt.Errorf("只能删除 .jar 文件（停用的是 .jar.disabled）")
	}
	if IsControlFile(sub + "/" + f) {
		return "", fmt.Errorf("%s 是 MineDeck 自己用的文件，不能在这里删除", f)
	}
	if i.Status() != "stopped" {
		return "", fmt.Errorf("先停止服务器再删除：运行中这些文件被服务器占着，删了也要重启才算数")
	}
	// 占上位再动文件：删的这一下不能有人启动服务器（启动会去读这些 jar）、换地图或者打包
	release, err := m.BeginWorldChangeInstance(i, modTrashWhy)
	if err != nil {
		return "", err
	}
	defer release()
	src := filepath.Join(i.Dir, sub, f)
	st, err := os.Lstat(src)
	if err != nil {
		return "", fmt.Errorf("%s 文件夹里没有 %s（可能已经删掉或改了名，刷新看看）", sub, f)
	}
	if !st.Mode().IsRegular() { // 文件夹、链接、联接都不动：它们指到哪里说不清
		return "", fmt.Errorf("%s 不是普通文件，没有删除", f)
	}
	destDir := filepath.Join(i.Dir, filepath.FromSlash(ModTrashDir), sub)
	// 末级是普通文件还不够：接管来的服务器里 mods、plugins，或者 .minedeck 和它下面的几级，可以是指到外面的目录联接、
	// 符号链接。顺着它移，就把服务器文件夹外面的文件挪走了，或者在外面建文件夹、放进移走的文件（2026-11-16 Codex 复审 beta.3）。
	// 所以要移的文件和放它的文件夹都按真实落点核对，在服务器文件夹里面才动，查不清的也不动。要赶在建文件夹之前查
	if err := trashPathsInside(i.Dir, src, destDir, sub, f); err != nil {
		return "", err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("没能建好放删除文件的文件夹：%w", err)
	}
	stem := f[:len(f)-len(ext)]
	dest := filepath.Join(destDir, f)
	for k := 2; ; k++ {
		if _, err := os.Lstat(dest); os.IsNotExist(err) {
			break
		}
		if k > 999 {
			return "", fmt.Errorf("已删除的文件夹里同名的文件太多了，先清理一下 %s", filepath.FromSlash(ModTrashDir))
		}
		dest = filepath.Join(destDir, fmt.Sprintf("%s (%d)%s", stem, k, ext))
	}
	if err := os.Rename(src, dest); err != nil {
		return "", fmt.Errorf("没能移走 %s（可能被别的程序占着）：%w", f, err)
	}
	rel, err := filepath.Rel(i.Dir, dest)
	if err != nil {
		rel = dest
	}
	return rel, nil
}

// trashPathsInside 要移的文件 src 和放删除文件的文件夹 destDir，真实落点（解开目录联接、符号链接以后）是不是都在服务器文件夹 dir 里面。
// 和「没装上的文件」的清单（pendingListPath）一样用 RealPathStrict 比：destDir 还没建时按它还在的那一级上层算，
// 所以建文件夹之前就查得出 .minedeck 指到了外面；哪一样查不清都按不在里面算。sub、file 只用来说清是哪个文件。
func trashPathsInside(dir, src, destDir, sub, file string) error {
	root, err := RealPathStrict(dir)
	if err != nil {
		return fmt.Errorf("查不清服务器文件夹实际在哪，没有删除 %s", file)
	}
	for _, c := range []struct{ path, what string }{
		{src, fmt.Sprintf("%s 所在的 %s 文件夹", file, sub)},
		{destDir, "放删除文件的 .minedeck 文件夹"},
	} {
		real, err := RealPathStrict(c.path)
		if err != nil {
			return fmt.Errorf("查不清%s实际在哪（可能是断掉的目录联接），没有删除", c.what)
		}
		if rel, err := filepath.Rel(root, real); err != nil || !filepath.IsLocal(rel) || !InsideReal(root, real) {
			return fmt.Errorf("%s指到了服务器文件夹外面，没有删除", c.what)
		}
	}
	return nil
}
