//go:build !windows

package secret

import "errors"

// MineDeck 只在 Windows 上运行；其他平台只为能编译，加解密一律失败。
var errNoDPAPI = errors.New("DPAPI 只在 Windows 上可用")

func protect([]byte) ([]byte, error)   { return nil, errNoDPAPI }
func unprotect([]byte) ([]byte, error) { return nil, errNoDPAPI }
