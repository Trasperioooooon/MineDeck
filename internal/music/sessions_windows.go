package music

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ListSources 列出各个播放设备上开了音频会话的进程，同一进程合并为一项；去掉系统声音、已结束的会话和本进程。
// 按「正在播放、峰值高」排在前面。某个设备列举失败时仍返回其余设备的结果，错误一并返回。
func ListSources() ([]Source, error) {
	var list []Source
	var listErr error
	err := withCOM(func() error {
		list, listErr = listSources()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return list, listErr
}

// sessionInfo 是一个音频会话。
type sessionInfo struct {
	pid   uint32
	state uint32
	name  string
	peak  float32
}

func listSources() ([]Source, error) {
	var enum *iMMDeviceEnumerator
	if hr := coCreateInstance(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator, unsafe.Pointer(&enum)); hr.failed() || enum == nil {
		return nil, hr.err("CoCreateInstance(MMDeviceEnumerator)")
	}
	defer release(enum)
	defaultID := ""
	var def *iMMDevice
	if hr := enum.GetDefaultAudioEndpoint(eRender, eConsole, &def); !hr.failed() && def != nil {
		defaultID = def.ID()
		release(def)
	}
	var coll *iMMDeviceCollection
	if hr := enum.EnumAudioEndpoints(eRender, deviceStateActive, &coll); hr.failed() || coll == nil {
		return nil, hr.err("IMMDeviceEnumerator::EnumAudioEndpoints")
	}
	defer release(coll)
	var count uint32
	if hr := coll.GetCount(&count); hr.failed() {
		return nil, hr.err("IMMDeviceCollection::GetCount")
	}

	self := uint32(os.Getpid())
	byPID := map[uint32]*Source{}
	var errs []error
	for i := uint32(0); i < count; i++ {
		var dev *iMMDevice
		if hr := coll.Item(i, &dev); hr.failed() || dev == nil {
			errs = append(errs, fmt.Errorf("第 %d 个播放设备：IMMDeviceCollection::Item → %s", i+1, hr))
			continue
		}
		name := dev.FriendlyName()
		if name == "" {
			name = "（未知设备）"
		}
		if id := dev.ID(); id != "" && id == defaultID {
			name += "（默认）"
		}
		sessions, err := deviceSessions(dev)
		release(dev)
		if err != nil {
			errs = append(errs, fmt.Errorf("播放设备「%s」：%w", name, err))
			continue
		}
		for _, ss := range sessions {
			if ss.pid == 0 || ss.pid == self {
				continue
			}
			src := byPID[ss.pid]
			if src == nil {
				src = &Source{PID: ss.pid}
				byPID[ss.pid] = src
			}
			src.Playing = src.Playing || ss.state == audioSessionStateActive
			src.Peak = max(src.Peak, ss.peak)
			if src.Name == "" {
				src.Name = ss.name
			}
			if !slices.Contains(src.Devices, name) {
				src.Devices = append(src.Devices, name)
			}
		}
	}

	list := make([]Source, 0, len(byPID))
	for _, src := range byPID {
		src.Path = processPathByPID(src.PID)
		if src.Path != "" {
			src.Exe = filepath.Base(src.Path)
		}
		list = append(list, *src)
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		switch {
		case a.Playing != b.Playing:
			return a.Playing
		case a.Peak != b.Peak:
			return a.Peak > b.Peak
		case !strings.EqualFold(a.Exe, b.Exe):
			return strings.ToLower(a.Exe) < strings.ToLower(b.Exe)
		}
		return a.PID < b.PID
	})
	return list, errors.Join(errs...)
}

// deviceSessions 枚举一个播放设备上的音频会话：IAudioSessionManager2 → 会话枚举器 → IAudioSessionControl2。
func deviceSessions(dev *iMMDevice) ([]sessionInfo, error) {
	var mgr *iAudioSessionManager2
	if hr := dev.Activate(&iidIAudioSessionManager2, unsafe.Pointer(&mgr)); hr.failed() || mgr == nil {
		return nil, hr.err("IMMDevice::Activate(IAudioSessionManager2)")
	}
	defer release(mgr)
	var se *iAudioSessionEnumerator
	if hr := mgr.GetSessionEnumerator(&se); hr.failed() || se == nil {
		return nil, hr.err("IAudioSessionManager2::GetSessionEnumerator")
	}
	defer release(se)
	var n int32
	if hr := se.GetCount(&n); hr.failed() {
		return nil, hr.err("IAudioSessionEnumerator::GetCount")
	}
	var out []sessionInfo
	for i := int32(0); i < n; i++ {
		if info, ok := sessionAt(se, i); ok {
			out = append(out, info)
		}
	}
	return out, nil
}

// sessionAt 读第 i 个会话；系统声音、已结束的会话和读不出来的返回 false。
func sessionAt(se *iAudioSessionEnumerator, i int32) (sessionInfo, bool) {
	var ctl *iUnknown
	if hr := se.GetSession(i, &ctl); hr.failed() || ctl == nil {
		return sessionInfo{}, false
	}
	defer release(ctl)
	var c2 *iAudioSessionControl2
	if hr := ctl.QueryInterface(&iidIAudioSessionControl2, unsafe.Pointer(&c2)); hr.failed() || c2 == nil {
		return sessionInfo{}, false
	}
	defer release(c2)
	if c2.IsSystemSoundsSession() == sOK {
		return sessionInfo{}, false
	}
	var info sessionInfo
	if hr := c2.GetProcessId(&info.pid); hr.failed() {
		return sessionInfo{}, false
	}
	if hr := c2.GetState(&info.state); hr.failed() || info.state == audioSessionStateExpired {
		return sessionInfo{}, false
	}
	info.name = c2.DisplayName()
	var meter *iAudioMeterInformation
	if hr := ctl.QueryInterface(&iidIAudioMeterInformation, unsafe.Pointer(&meter)); !hr.failed() && meter != nil {
		meter.GetPeakValue(&info.peak)
		release(meter)
	}
	return info, true
}

// ListDevices 列出所有启用的播放设备（整机录音时可以指定录哪一个），默认设备排在最前面。
func ListDevices() ([]Device, error) {
	var list []Device
	err := withCOM(func() error {
		var enum *iMMDeviceEnumerator
		if hr := coCreateInstance(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator, unsafe.Pointer(&enum)); hr.failed() || enum == nil {
			return hr.err("CoCreateInstance(MMDeviceEnumerator)")
		}
		defer release(enum)
		defaultID := ""
		var def *iMMDevice
		if hr := enum.GetDefaultAudioEndpoint(eRender, eConsole, &def); !hr.failed() && def != nil {
			defaultID = def.ID()
			release(def)
		}
		var coll *iMMDeviceCollection
		if hr := enum.EnumAudioEndpoints(eRender, deviceStateActive, &coll); hr.failed() || coll == nil {
			return hr.err("IMMDeviceEnumerator::EnumAudioEndpoints")
		}
		defer release(coll)
		var count uint32
		if hr := coll.GetCount(&count); hr.failed() {
			return hr.err("IMMDeviceCollection::GetCount")
		}
		for i := uint32(0); i < count; i++ {
			var dev *iMMDevice
			if hr := coll.Item(i, &dev); hr.failed() || dev == nil {
				continue
			}
			d := Device{ID: dev.ID(), Name: dev.FriendlyName()}
			release(dev)
			if d.ID == "" {
				continue
			}
			if d.Name == "" {
				d.Name = "（未知设备）"
			}
			d.Default = d.ID == defaultID
			list = append(list, d)
		}
		return nil
	})
	sort.SliceStable(list, func(i, j int) bool { return list[i].Default && !list[j].Default })
	return list, err
}

func processPathByPID(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	return processPath(h)
}

// processPath 返回进程的 exe 完整路径；权限不足等取不到时为空。
func processPath(h windows.Handle) string {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
