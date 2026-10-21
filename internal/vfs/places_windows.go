package vfs

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

func LocalPlaces() []Place {
	var places []Place
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		places = append(places, Place{Path: home, Name: "個人資料夾", Type: "home"})
		known := []struct{ name, sub string }{
			{"桌面", "Desktop"},
			{"下載", "Downloads"},
			{"圖片", "Pictures"},
			{"文件", "Documents"},
			{"影片", "Videos"},
		}
		for _, k := range known {
			p := filepath.Join(home, k.sub)
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				places = append(places, Place{Path: p, Name: k.name, Type: "folder"})
			}
		}
	}
	return places
}

func LocalDrives() []Place {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var places []Place
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		letter := rune('A' + i)
		root := string(letter) + ":\\"
		ptr, err := syscall.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		kind := driveTypeName(windows.GetDriveType(ptr))
		name := fmt.Sprintf("%s (%c:)", kind, letter)
		label := volumeLabel(ptr)
		if label != "" {
			name = fmt.Sprintf("%s (%c:)", label, letter)
		}
		places = append(places, Place{Path: root, Name: name, Type: "drive"})
	}
	return places
}

func driveTypeName(t uint32) string {
	switch t {
	case windows.DRIVE_REMOVABLE:
		return "卸除式磁碟"
	case windows.DRIVE_FIXED:
		return "本機磁碟"
	case windows.DRIVE_REMOTE:
		return "網路磁碟"
	case windows.DRIVE_CDROM:
		return "光碟機"
	case windows.DRIVE_RAMDISK:
		return "RAM 磁碟"
	}
	return "磁碟機"
}

func volumeLabel(ptr *uint16) string {
	nameBuf := make([]uint16, 261)
	fsBuf := make([]uint16, 261)
	err := windows.GetVolumeInformation(ptr,
		&nameBuf[0], uint32(len(nameBuf)),
		nil, nil, nil,
		&fsBuf[0], uint32(len(fsBuf)))
	if err != nil {
		return ""
	}
	return windows.UTF16ToString(nameBuf)
}
