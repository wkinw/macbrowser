package exif

import (
	"bytes"
	"encoding/binary"
	"strings"
	"time"
)

func Taken(data []byte) string {
	if t := jpegTaken(data); t != "" {
		return t
	}
	return pngTaken(data)
}

func Rating(data []byte) int {
	if r := xmpRating(data); r >= 0 {
		return r
	}
	return exifRating(data)
}

func xmpRating(data []byte) int {
	for _, key := range []string{"xmp:Rating", "MicrosoftPhoto:Rating"} {
		off := 0
		for off < len(data) {
			i := bytes.Index(data[off:], []byte(key))
			if i < 0 {
				break
			}
			i += off
			j := i + len(key)
			for j < len(data) && (data[j] == ' ' || data[j] == '\t' || data[j] == '\r' || data[j] == '\n') {
				j++
			}
			var raw []byte
			if j < len(data) && data[j] == '=' {
				j++
				for j < len(data) && (data[j] == ' ' || data[j] == '\t') {
					j++
				}
				if j < len(data) && (data[j] == '"' || data[j] == '\'') {
					q := data[j]
					j++
					start := j
					for j < len(data) && data[j] != q {
						j++
					}
					if j > start {
						raw = data[start:j]
					}
				}
			} else if j < len(data) && data[j] == '>' {
				j++
				start := j
				for j < len(data) && data[j] != '<' && data[j] != '"' && data[j] != '\'' {
					j++
				}
				raw = data[start:j]
			}
			if raw != nil {
				if v, ok := parseRatingValue(raw); ok {
					return v
				}
			}
			off = i + len(key)
		}
	}
	return -1
}

func parseRatingValue(raw []byte) (int, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || len(s) > 3 {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n < 0 || n > 5 {
		return 0, false
	}
	return n, true
}

func exifRating(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 0
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 0
		}
		for i < len(b) && b[i] == 0xFF {
			i++
		}
		if i >= len(b) {
			return 0
		}
		m := b[i]
		i++
		if m == 0xD9 || m == 0xDA {
			return 0
		}
		if i+2 > len(b) {
			return 0
		}
		ln := int(binary.BigEndian.Uint16(b[i:]))
		if ln < 2 || i+ln > len(b) {
			return 0
		}
		if m == 0xE1 {
			seg := b[i+2 : i+ln]
			if len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
				if r := tiffRating(seg[6:]); r > 0 {
					return r
				}
			}
		}
		i += ln
	}
	return 0
}

func tiffRating(t []byte) int {
	if len(t) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	if bo.Uint16(t[2:]) != 42 {
		return 0
	}
	ifd0 := int(bo.Uint32(t[4:]))
	if typ, count, data, ok := ifdEntry(t, bo, ifd0, 0x4746); ok && count >= 1 {
		if typ == 3 && len(data) >= 2 {
			if v := int(bo.Uint16(data)); v >= 0 && v <= 5 {
				return v
			}
		}
		if typ == 4 && len(data) >= 4 {
			if v := int(bo.Uint32(data)); v >= 0 && v <= 5 {
				return v
			}
		}
	}
	return 0
}

func jpegTaken(b []byte) string {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return ""
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return ""
		}
		for i < len(b) && b[i] == 0xFF {
			i++
		}
		if i >= len(b) {
			return ""
		}
		m := b[i]
		i++
		if m == 0xD9 || m == 0xDA {
			return ""
		}
		if i+2 > len(b) {
			return ""
		}
		ln := int(binary.BigEndian.Uint16(b[i:]))
		if ln < 2 || i+ln > len(b) {
			return ""
		}
		if m == 0xE1 {
			seg := b[i+2 : i+ln]
			if len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
				if t := tiffTaken(seg[6:]); t != "" {
					return t
				}
			}
		}
		i += ln
	}
	return ""
}

func pngTaken(b []byte) string {
	if len(b) < 16 || string(b[:8]) != "\x89PNG\r\n\x1a\n" {
		return ""
	}
	i := 8
	for i+12 <= len(b) {
		ln := int(binary.BigEndian.Uint32(b[i:]))
		typ := string(b[i+4 : i+8])
		if ln < 0 || i+12+ln > len(b) {
			return ""
		}
		if typ == "eXIf" {
			return tiffTaken(b[i+8 : i+8+ln])
		}
		if typ == "IDAT" || typ == "IEND" {
			return ""
		}
		i += 12 + ln
	}
	return ""
}

func tiffTaken(t []byte) string {
	if len(t) < 8 {
		return ""
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return ""
	}
	if bo.Uint16(t[2:]) != 42 {
		return ""
	}
	ifd0 := int(bo.Uint32(t[4:]))
	if typ, count, data, ok := ifdEntry(t, bo, ifd0, 0x8769); ok && typ == 4 && count == 1 && len(data) >= 4 {
		sub := int(bo.Uint32(data))
		if sub > 0 {
			if _, _, v, ok := ifdEntry(t, bo, sub, 0x9003); ok {
				if s := formatTime(v); s != "" {
					return s
				}
			}
		}
	}
	if _, _, v, ok := ifdEntry(t, bo, ifd0, 0x0132); ok {
		return formatTime(v)
	}
	return ""
}

func ifdEntry(b []byte, bo binary.ByteOrder, off int, tag uint16) (uint16, uint32, []byte, bool) {
	if off < 0 || off+2 > len(b) {
		return 0, 0, nil, false
	}
	n := int(bo.Uint16(b[off:]))
	base := off + 2
	if n < 0 || base+n*12 > len(b) {
		return 0, 0, nil, false
	}
	for k := 0; k < n; k++ {
		e := base + k*12
		if bo.Uint16(b[e:]) != tag {
			continue
		}
		typ := bo.Uint16(b[e+2:])
		count := bo.Uint32(b[e+4:])
		size := 0
		switch typ {
		case 1, 2, 6, 7:
			size = int(count)
		case 3, 8:
			size = int(count) * 2
		case 4, 9, 11:
			size = int(count) * 4
		case 5, 10, 12:
			size = int(count) * 8
		default:
			size = int(count)
		}
		if size < 0 || size > 1<<20 {
			return 0, 0, nil, false
		}
		if size <= 4 {
			return typ, count, b[e+8 : e+8+size], true
		}
		ofs := int(bo.Uint32(b[e+8:]))
		if ofs <= 0 || ofs+size > len(b) {
			return 0, 0, nil, false
		}
		return typ, count, b[ofs : ofs+size], true
	}
	return 0, 0, nil, false
}

func formatTime(v []byte) string {
	s := strings.TrimSpace(strings.TrimRight(string(v), "\x00"))
	if len(s) < 16 || strings.HasPrefix(s, "0000") {
		return ""
	}
	for _, layout := range []string{
		"2006:01:02 15:04:05",
		"2006-01-02 15:04:05",
		"2006:01:02 15:04:05Z07:00",
		"2006:01:02 15:04:05-07:00",
		"2006:01:02 15:04:05+07:00",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02 15:04")
		}
	}
	if len(s) >= 16 {
		d := strings.ReplaceAll(s[:10], ":", "-")
		if len(d) == 10 {
			return d + " " + s[11:16]
		}
	}
	return ""
}
