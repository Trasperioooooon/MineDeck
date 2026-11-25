package app

import (
	"path/filepath"
	"strings"
)

// SamePath 两个路径写的是不是同一个位置（Windows 上路径不分大小写）。只按字面清洗后比，不解链接。
func SamePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
