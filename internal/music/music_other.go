//go:build !windows

// 非 Windows 平台桩：本项目仅面向 Windows，此桩只为让依赖本包的代码跨平台可编译。
package music

import "errors"

var errUnsupported = errors.New("音频采集仅支持 Windows")

// Detect 在非 Windows 平台返回零值（Mode 为 0，表示不能采集）。
func Detect() Capability { return Capability{} }

func ListSources() ([]Source, error) { return nil, errUnsupported }

func ListDevices() ([]Device, error) { return nil, errUnsupported }

func Start(opts Options) (*Capture, error) {
	if err := validate(opts); err != nil {
		return nil, err
	}
	return nil, errUnsupported
}
