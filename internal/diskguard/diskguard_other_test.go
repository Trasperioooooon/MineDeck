//go:build !windows

package diskguard

func testVolumeSerial(string) (uint32, bool) { return 0, false }
