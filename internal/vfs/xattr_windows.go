package vfs

import (
	"io"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const (
	maxStreamSize   = 16 << 20
	backupSemantics = 0x02000000
)

func streamAlt(stream string) string {
	return strings.ReplaceAll(stream, ":", "\uF022")
}

func longPath(p string) string {
	if strings.HasPrefix(p, `\\?\`) {
		return p
	}
	if strings.HasPrefix(p, `\\`) {
		return `\\?\UNC\` + p[2:]
	}
	return `\\?\` + p
}

func readStream(p, stream string) ([]byte, error) {
	b, err := readStreamRaw(p, stream)
	if err != nil && strings.Contains(stream, ":") {
		return readStreamRaw(p, streamAlt(stream))
	}
	return b, err
}

func readStreamRaw(p, stream string) ([]byte, error) {
	full := longPath(p) + ":" + stream
	ptr, err := syscall.UTF16PtrFromString(full)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(ptr,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|backupSemantics,
		0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)

	var out []byte
	buf := make([]byte, 64*1024)
	for len(out) < maxStreamSize {
		var n uint32
		if err := windows.ReadFile(h, buf, &n, nil); err != nil {
			return nil, err
		}
		if n == 0 {
			break
		}
		out = append(out, buf[:n]...)
	}
	if len(out) == 0 {
		return nil, ErrXattrNotSupported
	}
	return out, nil
}

func writeStream(p, stream string, data []byte) error {
	err := writeStreamRaw(p, stream, data)
	if err != nil && strings.Contains(stream, ":") {
		return writeStreamRaw(p, streamAlt(stream), data)
	}
	return err
}

func writeStreamRaw(p, stream string, data []byte) error {
	full := longPath(p) + ":" + stream + ":$DATA"
	ptr, err := syscall.UTF16PtrFromString(full)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(ptr,
		windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.CREATE_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL|backupSemantics,
		0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var written uint32
	if err := windows.WriteFile(h, data, &written, nil); err != nil {
		return err
	}
	if int(written) != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

func setHiddenAttr(p string) {
	ptr, err := syscall.UTF16PtrFromString(longPath(p))
	if err != nil {
		return
	}
	attrs, err := windows.GetFileAttributes(ptr)
	if err != nil {
		return
	}
	windows.SetFileAttributes(ptr, attrs|windows.FILE_ATTRIBUTE_HIDDEN)
}

func isHidden(p string) bool {
	ptr, err := syscall.UTF16PtrFromString(longPath(p))
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(ptr)
	if err != nil {
		return false
	}
	return attrs&windows.FILE_ATTRIBUTE_HIDDEN != 0
}
