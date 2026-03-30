package rules

import (
	"os"
	"regexp"
	"sort"
	"strings"
)

var (
	CopyRe = regexp.MustCompile(`\(\d+\)|[-_ ]Copy|_[0-9]+$`)
	PendRe = regexp.MustCompile(`[\\/]\.pending_`)
	EdRe   = regexp.MustCompile(`(?i)edited|filtered|_edit`)
	HmRe   = regexp.MustCompile(`[\\/]\._`)
	WaRe   = regexp.MustCompile(`(?i)whatsapp`)
	RjRe   = regexp.MustCompile(`(?i)\.jpe?g$`)
)

func IsUnder(path, dir string) bool {
	return strings.HasPrefix(path, dir+string(os.PathSeparator))
}

func PreferDelete(path string) int {
	switch {
	case PendRe.MatchString(path):
		return 0
	case HmRe.MatchString(path):
		return 1
	case CopyRe.MatchString(path):
		return 2
	case EdRe.MatchString(path):
		return 3
	case WaRe.MatchString(path):
		return 4
	case RjRe.MatchString(path):
		return 5
	default:
		return 6
	}
}

func SortByPreference(paths []string) []string {
	out := append([]string(nil), paths...)
	sort.Slice(out, func(i, j int) bool {
		ai := PreferDelete(out[i])
		aj := PreferDelete(out[j])
		if ai != aj {
			return ai < aj
		}
		return out[i] < out[j]
	})
	return out
}

func SelectKeepDelete(preferred []string, origin, likely string) (toKeep, toDelete []string) {
	switch {
	case origin == "" && likely == "":
		toKeep = preferred[:1]
		if len(preferred) > 1 {
			toDelete = preferred[1:]
		}

	case origin != "" && likely == "":
		var inOrigin, outside []string
		for _, p := range preferred {
			if IsUnder(p, origin) {
				inOrigin = append(inOrigin, p)
			} else {
				outside = append(outside, p)
			}
		}
		if len(inOrigin) > 0 {
			toKeep = append(toKeep, inOrigin...)
			toDelete = append(toDelete, outside...)
		} else {
			toKeep = preferred[:1]
			if len(preferred) > 1 {
				toDelete = preferred[1:]
			}
		}

	case origin == "" && likely != "":
		var inLikely, outside []string
		for _, p := range preferred {
			if IsUnder(p, likely) {
				inLikely = append(inLikely, p)
			} else {
				outside = append(outside, p)
			}
		}
		if len(outside) > 0 {
			toKeep = append(toKeep, outside[:1]...)
			if len(outside) > 1 {
				toDelete = append(toDelete, outside[1:]...)
			}
			toDelete = append(toDelete, inLikely...)
		} else {
			toKeep = preferred[:1]
			if len(preferred) > 1 {
				toDelete = preferred[1:]
			}
		}

	case origin != "" && likely != "":
		var inOrigin, inLikely, outside []string
		for _, p := range preferred {
			switch {
			case IsUnder(p, origin):
				inOrigin = append(inOrigin, p)
			case IsUnder(p, likely):
				inLikely = append(inLikely, p)
			default:
				outside = append(outside, p)
			}
		}
		if len(inOrigin) > 0 {
			toKeep = append(toKeep, inOrigin...)
			toDelete = append(toDelete, inLikely...)
			toDelete = append(toDelete, outside...)
		} else if len(outside) > 0 {
			toKeep = append(toKeep, outside[:1]...)
			if len(outside) > 1 {
				toDelete = append(toDelete, outside[1:]...)
			}
			toDelete = append(toDelete, inLikely...)
		} else {
			toKeep = preferred[:1]
			if len(preferred) > 1 {
				toDelete = preferred[1:]
			}
		}
	}
	return
}
