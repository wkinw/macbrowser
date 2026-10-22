//go:build windows

package secret

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	Prefix                  = "dpapi:v1:"
	cryptProtectUIForbidden = 0x1
)

var (
	crypt32                = windows.NewLazySystemDLL("crypt32.dll")
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFree          = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newBlob(b []byte) dataBlob {
	if len(b) == 0 {
		return dataBlob{}
	}
	return dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func blobBytes(b dataBlob) []byte {
	if b.cbData == 0 || b.pbData == nil {
		return nil
	}
	return unsafe.Slice(b.pbData, int(b.cbData))
}

func freeBlob(b dataBlob) {
	if b.pbData != nil {
		procLocalFree.Call(uintptr(unsafe.Pointer(b.pbData)))
	}
}

func Protect(plain string) (string, error) {
	in := newBlob([]byte(plain))
	var out dataBlob
	ok, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if ok == 0 {
		return "", fmt.Errorf("CryptProtectData 失敗：%w", err)
	}
	defer freeBlob(out)
	return Prefix + base64.RawURLEncoding.EncodeToString(blobBytes(out)), nil
}

func Unprotect(enc string) (string, error) {
	if !strings.HasPrefix(enc, Prefix) {
		return "", errors.New("不是 DPAPI 加密格式")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(enc, Prefix))
	if err != nil {
		return "", fmt.Errorf("解析加密資料失敗：%w", err)
	}
	in := newBlob(raw)
	var out dataBlob
	ok, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if ok == 0 {
		return "", fmt.Errorf("CryptUnprotectData 失敗：%w", err)
	}
	defer freeBlob(out)
	return string(blobBytes(out)), nil
}
