package macmeta

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strconv"

	"howett.net/plist"
)

type Writer interface {
	Xattr(path, name string) ([]byte, error)
	Open(path string) (io.ReadSeekCloser, error)
	Join(dir, name string) string
	WriteXattr(path, name string, data []byte) error
	WriteFile(path string, data []byte) error
}

func BuildUserTags(color int) ([]byte, error) {
	tags := []string{}
	if color >= 1 && color <= 7 {
		tags = []string{ColorNames[color] + "\n" + strconv.Itoa(color)}
	}
	return plist.Marshal(tags, plist.BinaryFormat)
}

func FinderInfoWithColor(existing []byte, color int) []byte {
	b := make([]byte, 32)
	if len(existing) >= 10 {
		copy(b, existing)
	}
	flags := int(b[8])<<8 | int(b[9])
	flags = (flags &^ 0x0E) | ((color & 0x7) << 1)
	b[8] = byte(flags >> 8)
	b[9] = byte(flags)
	return b
}

type adEntry struct {
	id   int
	data []byte
}

func encodeAppleDouble(entries []adEntry) []byte {
	var buf bytes.Buffer
	hdr := 26 + len(entries)*12
	binary.Write(&buf, binary.BigEndian, uint32(adMagicAppleDouble))
	binary.Write(&buf, binary.BigEndian, uint32(0x00020000))
	buf.Write(make([]byte, 16))
	binary.Write(&buf, binary.BigEndian, uint16(len(entries)))
	off := hdr
	type offs struct{ entry, off, ln int }
	os := make([]offs, 0, len(entries))
	for _, e := range entries {
		os = append(os, offs{e.id, off, len(e.data)})
		off += len(e.data)
	}
	for _, o := range os {
		binary.Write(&buf, binary.BigEndian, uint32(o.entry))
		binary.Write(&buf, binary.BigEndian, uint32(o.off))
		binary.Write(&buf, binary.BigEndian, uint32(o.ln))
	}
	for _, e := range entries {
		buf.Write(e.data)
	}
	return buf.Bytes()
}

func BuildAppleDouble(fi []byte, eas map[string][]byte, comment string) []byte {
	var entries []adEntry
	if len(fi) > 0 {
		entries = append(entries, adEntry{entryFinderInfo, fi})
	}
	if comment != "" {
		entries = append(entries, adEntry{entryComment, []byte(comment)})
	}
	if len(eas) > 0 {
		names := make([]string, 0, len(eas))
		for k := range eas {
			names = append(names, k)
		}
		sort.Strings(names)
		var container []byte
		for _, name := range names {
			container = append(container, encodeAppleDouble([]adEntry{
				{entryRealName, append([]byte(name), 0)},
				{entryDataFork, eas[name]},
			})...)
		}
		entries = append(entries, adEntry{entryResourceFork, container})
	}
	return encodeAppleDouble(entries)
}

func SetLabel(fs Writer, dir, name string, color int) error {
	return SetLabelNode(fs, dir, name, color, false)
}

func SetLabelNode(fs Writer, dir, name string, color int, isDir bool) error {
	if color < 0 || color > 7 {
		return fmt.Errorf("無效的顏色編號 %d", color)
	}
	full := fs.Join(dir, name)
	fiRaw, _ := fs.Xattr(full, FinderInfoAttr)
	fi := FinderInfoWithColor(fiRaw, color)
	tags, terr := BuildUserTags(color)

	tagsOK := false
	if terr == nil {
		if err := fs.WriteXattr(full, UserTagsAttr, tags); err == nil {
			tagsOK = true
		}
	}
	wroteFI := false
	if err := fs.WriteXattr(full, FinderInfoAttr, fi); err == nil {
		wroteFI = true
	}
	forceAD := false
	if !isDir {
		if aw, ok := fs.(interface{ AppleDoubleSidecar() bool }); ok {
			forceAD = aw.AppleDoubleSidecar()
		}
	}
	if tagsOK {
		if forceAD {
			createOrUpdateAD(fs, dir, name, fi, color)
		} else if !isDir {
			updateADIfExists(fs, dir, name, fi, color)
		}
		return nil
	}
	if isDir {
		if wroteFI {
			return nil
		}
		return fmt.Errorf("此位置無法儲存資料夾色標（檔案系統不支援目錄屬性）")
	}
	if err := createOrUpdateAD(fs, dir, name, fi, color); err != nil {
		if wroteFI {
			return nil
		}
		return fmt.Errorf("此位置無法儲存色標：%w", err)
	}
	return nil
}

func readAD(fs Writer, dir, name string) *AppleDouble {
	f, err := fs.Open(fs.Join(dir, "._"+name))
	if err != nil {
		return nil
	}
	data, rerr := io.ReadAll(io.LimitReader(f, 4<<20))
	f.Close()
	if rerr != nil {
		return nil
	}
	return ParseAppleDouble(data)
}

func updateADIfExists(fs Writer, dir, name string, fi []byte, color int) {
	if ad := readAD(fs, dir, name); ad != nil {
		writeAD(fs, dir, name, fi, color, ad)
	}
}

func createOrUpdateAD(fs Writer, dir, name string, fi []byte, color int) error {
	ad := readAD(fs, dir, name)
	if ad == nil && color == 0 {
		return nil
	}
	return writeAD(fs, dir, name, fi, color, ad)
}

func writeAD(fs Writer, dir, name string, fi []byte, color int, ad *AppleDouble) error {
	eas := map[string][]byte{FinderInfoAttr: fi}
	comment := ""
	if ad != nil {
		comment = ad.Comment
		for k, v := range ad.EAs {
			if k == UserTagsAttr || k == FinderInfoAttr {
				continue
			}
			eas[k] = v
		}
	}
	if color > 0 {
		if tags, err := BuildUserTags(color); err == nil {
			eas[UserTagsAttr] = tags
		}
	}
	return fs.WriteFile(fs.Join(dir, "._"+name), BuildAppleDouble(fi, eas, comment))
}
