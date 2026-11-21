package diskguard

import (
	"math"

	"golang.org/x/sys/windows"
)

func Free(path string) (int64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, err
	}
	if free > math.MaxInt64 {
		return math.MaxInt64, nil
	}
	return int64(free), nil
}

// volumeKey 这个已经存在的文件夹在哪个卷上：卷序列号。
func volumeKey(dir string) (uint32, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return 0, err
	}
	// A serial collision only queues unrelated volumes together; it cannot permit
	// two writers on one volume to bypass the gate.
	return info.VolumeSerialNumber, nil
}
