package macmeta

import "strings"

const (
	FinderInfoAttr = "com.apple.FinderInfo"
	UserTagsAttr   = "com.apple.metadata:_kMDItemUserTags"
	AfpInfoStream  = "AFP_AfpInfo"
	ResourceStream = "AFP_Resource"
)

var ColorNames = map[int]string{
	1: "灰色",
	2: "綠色",
	3: "紫色",
	4: "藍色",
	5: "黃色",
	6: "紅色",
	7: "橘色",
}

func ColorFromFinderInfo(fi []byte) int {
	if len(fi) < 10 {
		return 0
	}
	flags := int(fi[8])<<8 | int(fi[9])
	color := (flags >> 1) & 0x7
	if color < 0 || color > 7 {
		return 0
	}
	return color
}

func ColorFromAfpInfo(b []byte) int {
	if len(b) >= 48 {
		return ColorFromFinderInfo(b[16:48])
	}
	return ColorFromFinderInfo(b)
}

func stripColorSuffix(tag string) (string, int) {
	name := tag
	color := 0
	if i := strings.LastIndexByte(tag, '\n'); i >= 0 {
		name = tag[:i]
		suffix := strings.TrimSpace(tag[i+1:])
		if n, ok := atoiStrict(suffix); ok && n >= 0 && n <= 7 {
			color = n
		} else {
			name = tag
		}
	}
	return name, color
}

func atoiStrict(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
		if n > 1000 {
			return 0, false
		}
	}
	return n, true
}
