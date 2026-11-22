//go:build windows

package diskguard

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// testVolumeSerial 不经过被测的 volumeOf / volumeKey，独立用 GetVolumePathName 与 GetVolumeInformation 取路径所在卷的序列号。
func testVolumeSerial(path string) (uint32, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, false
	}
	u, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return 0, false
	}
	rootBuf := make([]uint16, len(abs)+260)
	if err := windows.GetVolumePathName(u, &rootBuf[0], uint32(len(rootBuf))); err != nil {
		return 0, false
	}
	var serial uint32
	if err := windows.GetVolumeInformation(&rootBuf[0], nil, 0, &serial, nil, nil, nil, 0); err != nil {
		return 0, false
	}
	return serial, true
}
