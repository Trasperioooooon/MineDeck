package secret

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// entropy 加解密时带上的程序专用附加口令：别的程序不带它调用 CryptUnprotectData 就解不开。
// 它写在程序里，算不上秘密，只是多一道门槛。改了它，已有的密文就全部解不开，不要改。
var entropy = []byte("MineDeck/secret/v1")

var description, _ = windows.UTF16PtrFromString("MineDeck")

func blob(b []byte) windows.DataBlob {
	if len(b) == 0 {
		return windows.DataBlob{}
	}
	return windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

// takeBlob 拷出系统分配的输出缓冲区，原缓冲区清零后用 LocalFree 释放。
func takeBlob(out *windows.DataBlob) []byte {
	if out.Data == nil {
		return []byte{}
	}
	src := unsafe.Slice(out.Data, out.Size)
	b := make([]byte, len(src))
	copy(b, src)
	clear(src)
	windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return b
}

// protect 用当前 Windows 用户的 DPAPI 加密；CRYPTPROTECT_UI_FORBIDDEN：任何情况下都不弹提示框。
func protect(plain []byte) ([]byte, error) {
	in, ent := blob(plain), blob(entropy)
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, description, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return takeBlob(&out), nil
}

func unprotect(sealed []byte) ([]byte, error) {
	in, ent := blob(sealed), blob(entropy)
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return takeBlob(&out), nil
}
