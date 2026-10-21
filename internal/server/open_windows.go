package server

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	shell32DLL         = syscall.NewLazyDLL("shell32.dll")
	user32DLL          = syscall.NewLazyDLL("user32.dll")
	kernel32DLL        = syscall.NewLazyDLL("kernel32.dll")
	procShellExecuteW  = shell32DLL.NewProc("ShellExecuteW")
	procDragQueryFileW = shell32DLL.NewProc("DragQueryFileW")

	procOpenClipboard              = user32DLL.NewProc("OpenClipboard")
	procCloseClipboard             = user32DLL.NewProc("CloseClipboard")
	procEmptyClipboard             = user32DLL.NewProc("EmptyClipboard")
	procGetClipboardData           = user32DLL.NewProc("GetClipboardData")
	procSetClipboardData           = user32DLL.NewProc("SetClipboardData")
	procIsClipboardFormatAvailable = user32DLL.NewProc("IsClipboardFormatAvailable")
	procGetClipboardSequenceNumber = user32DLL.NewProc("GetClipboardSequenceNumber")
	procRegisterClipboardFormatW   = user32DLL.NewProc("RegisterClipboardFormatW")

	procGlobalAlloc   = kernel32DLL.NewProc("GlobalAlloc")
	procGlobalLock    = kernel32DLL.NewProc("GlobalLock")
	procGlobalUnlock  = kernel32DLL.NewProc("GlobalUnlock")
	procGlobalFree    = kernel32DLL.NewProc("GlobalFree")
	procRtlMoveMemory = kernel32DLL.NewProc("RtlMoveMemory")
)

const (
	cfHDrop        = 15
	gmemMoveable   = 0x0002
	dropEffectCopy = 1
	dropEffectMove = 2
)

func openWith(path string) error {
	return exec.Command("rundll32.exe", "shell32.dll,OpenAs_RunDLL", path).Start()
}

func openClipboard() error {
	for i := 0; i < 20; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("無法存取剪貼簿（被其他程式占用）")
}

func clipboardSeq() uint32 {
	r, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(r)
}

func clipboardFiles() ([]string, error) {
	if err := openClipboard(); err != nil {
		return nil, err
	}
	defer procCloseClipboard.Call()
	if r, _, _ := procIsClipboardFormatAvailable.Call(cfHDrop); r == 0 {
		return nil, nil
	}
	h, _, _ := procGetClipboardData.Call(cfHDrop)
	if h == 0 {
		return nil, fmt.Errorf("剪貼簿沒有檔案")
	}
	n, _, _ := procDragQueryFileW.Call(h, 0xFFFFFFFF, 0, 0)
	if n == 0 {
		return nil, nil
	}
	out := make([]string, 0, int(n))
	for i := 0; i < int(n); i++ {
		buf := make([]uint16, 32768)
		r, _, _ := procDragQueryFileW.Call(h, uintptr(i), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if r == 0 {
			continue
		}
		out = append(out, syscall.UTF16ToString(buf[:r]))
	}
	return out, nil
}

func setClipboardFiles(paths []string, move bool) error {
	if len(paths) == 0 {
		return fmt.Errorf("沒有可複製的項目")
	}
	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	if r, _, _ := procEmptyClipboard.Call(); r == 0 {
		return fmt.Errorf("無法清空剪貼簿")
	}
	if err := setClipData(cfHDrop, buildDropFiles(paths)); err != nil {
		return err
	}
	effect := uint32(dropEffectCopy)
	if move {
		effect = dropEffectMove
	}
	name, _ := syscall.UTF16PtrFromString("Preferred DropEffect")
	f, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
	if f != 0 {
		_ = setClipData(uintptr(f), dropEffectBytes(effect))
	}
	return nil
}

func buildDropFiles(paths []string) []byte {
	var buf bytes.Buffer
	hdr := make([]byte, 20)
	binary.LittleEndian.PutUint32(hdr[0:], 20)
	binary.LittleEndian.PutUint32(hdr[16:], 1)
	buf.Write(hdr)
	var scratch [2]byte
	for _, p := range paths {
		for _, c := range utf16.Encode([]rune(p)) {
			binary.LittleEndian.PutUint16(scratch[:], c)
			buf.Write(scratch[:])
		}
		buf.Write([]byte{0, 0})
	}
	buf.Write([]byte{0, 0})
	return buf.Bytes()
}

func dropEffectBytes(effect uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], effect)
	return b[:]
}

func setClipData(format uintptr, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("剪貼簿資料為空")
	}
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return fmt.Errorf("無法配置剪貼簿記憶體")
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("無法鎖定剪貼簿記憶體")
	}
	procRtlMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	procGlobalUnlock.Call(h)
	if r, _, _ := procSetClipboardData.Call(format, h); r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("無法寫入剪貼簿")
	}
	return nil
}

func shellOpen(path string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0, 0,
		1,
	)
	if ret <= 32 {
		return fmt.Errorf("無法開啟檔案（ShellExecute 代碼 %d）", ret)
	}
	return nil
}

func revealInExplorer(path string) error {
	return exec.Command("explorer.exe", "/select,"+path).Start()
}
