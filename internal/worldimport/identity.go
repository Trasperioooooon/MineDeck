package worldimport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	return dirIdentity(path) // 按平台，见 identity_windows.go / identity_darwin.go
}

func rememberStagedDirs(stage string, dirs []string, j *Journal) {
	j.NewDirIDs = map[string]string{}
	for _, n := range dirs {
		if id, err := directoryIdentity(filepath.Join(stage, filepath.FromSlash(n))); err == nil {
			j.NewDirIDs[strings.ToLower(n)] = id
		}
	}
}
