package worldimport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// directoryIdentity follows the directory object across a rename. A hash and
// byte count alone cannot distinguish two different worlds with identical metadata.
func directoryIdentity(path string) (string, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !st.IsDir() || st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return "", fmt.Errorf("世界目录不是普通文件夹")
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return "", err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return "", fmt.Errorf("世界目录的类型已改变")
	}
	if info.FileIndexHigh == 0 && info.FileIndexLow == 0 {
		return "", fmt.Errorf("磁盘没有提供可靠的目录编号")
	}
	return fmt.Sprintf("%08x:%08x%08x:%08x%08x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow, info.CreationTime.HighDateTime, info.CreationTime.LowDateTime), nil
}

func rememberStagedDirs(stage string, dirs []string, j *Journal) {
	j.NewDirIDs = map[string]string{}
	for _, n := range dirs {
		if id, err := directoryIdentity(filepath.Join(stage, filepath.FromSlash(n))); err == nil {
			j.NewDirIDs[strings.ToLower(n)] = id
		}
	}
}
