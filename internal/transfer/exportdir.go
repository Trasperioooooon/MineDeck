package transfer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"minedeck/internal/inst"
	"minedeck/internal/winname"
)

// ErrExportDirMissing 导出交接包时填的文件夹不在。报错的前半句仍是「导出文件夹不可用」：以前认这句的地方（界面、测试）照旧认得，
// 界面再按括号里这几个字给「新建并导出」（可用性走查 R20）。
const ErrExportDirMissing = "导出文件夹不可用（找不到这个文件夹）"

// MakeExportDir 「新建并导出」：替用户把导出文件夹建出来，只建最后一级，上一级必须已经在。
//
// 不替人建一串文件夹：路径里打错一个字时，悄悄建出一整串错地方的文件夹，比报一句「上一级不在」更难发现。
// 建之前先把导出那边会拒绝的情况查掉（服务器在运行、文件夹落在这台服务器的文件夹里），免得建好了又导不出、
// 留下一个空文件夹。已经在的文件夹原样用（created 为 false），是文件不是文件夹的报错。
//
// 只该由本机的请求调用（docs/设计与参考/API.md「主机路径仅本机可指定」）：局域网会话不能决定在主机哪里建文件夹，由网页层把关。
// 返回清理过的路径，和这次是不是真的新建了（导出没成时网页层据此把空文件夹删掉）。
func MakeExportDir(m *inst.Manager, instName, dest string) (dir string, created bool, err error) {
	dest = strings.TrimSpace(dest)
	if dest == "" || !filepath.IsAbs(dest) {
		return "", false, errors.New("请填完整的文件夹路径，比如 D:\\交接包，或者清空用默认位置")
	}
	dest = filepath.Clean(dest)
	i, err := m.Get(instName)
	if err != nil {
		return "", false, err
	}
	if i.Status() != "stopped" {
		return "", false, fmt.Errorf("交接前请先停止服务器（运行中打包会不完整）")
	}
	if rel, err := filepath.Rel(i.Dir, dest); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false, fmt.Errorf("导出文件夹不能放在这台服务器的文件夹里面，请另选一个位置")
	}
	if st, err := os.Stat(dest); err == nil {
		if !st.IsDir() {
			return "", false, fmt.Errorf("「%s」已经是一个文件，不是文件夹，请换一个名字", dest)
		}
		return dest, false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", false, fmt.Errorf("导出文件夹不可用: %w", err)
	}
	parent, name := filepath.Dir(dest), filepath.Base(dest)
	if parent == dest || !winname.OK(name) {
		return "", false, fmt.Errorf("「%s」不能用作文件夹名，请换一个名字", name)
	}
	if st, err := os.Stat(parent); err != nil || !st.IsDir() {
		return "", false, fmt.Errorf("上一级文件夹「%s」不在。MineDeck 只新建最后一级，请检查路径有没有打错，或者点「浏览…」选一个已有的文件夹", parent)
	}
	if err := os.Mkdir(dest, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) { // 刚刚被别的程序建好了：照常用
			if st, e := os.Stat(dest); e == nil && st.IsDir() {
				return dest, false, nil
			}
		}
		return "", false, fmt.Errorf("没能新建导出文件夹: %w", err)
	}
	return dest, true, nil
}
