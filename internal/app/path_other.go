//go:build !windows

package app

import "path/filepath"

// SamePath 两个路径写的是不是同一个位置（类 Unix 区分大小写，与 foldRoot 口径一致）。只按字面清洗后比，不解链接。
func SamePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
