package macmeta

import (
	"howett.net/plist"
)

type UserTags struct {
	Names []string
	Color int
}

func ParseUserTags(data []byte) *UserTags {
	if len(data) == 0 {
		return nil
	}
	var tags []string
	if _, err := plist.Unmarshal(data, &tags); err != nil {
		var single string
		if _, err2 := plist.Unmarshal(data, &single); err2 != nil {
			return nil
		}
		tags = []string{single}
	}
	if len(tags) == 0 {
		return nil
	}
	ut := &UserTags{Names: make([]string, 0, len(tags))}
	for _, t := range tags {
		name, color := stripColorSuffix(t)
		if name == "" {
			continue
		}
		ut.Names = append(ut.Names, name)
		if ut.Color == 0 && color >= 1 && color <= 7 {
			ut.Color = color
		}
	}
	return ut
}
