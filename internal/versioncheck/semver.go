package versioncheck

import (
	"regexp"
	"strconv"
	"strings"
)

var semverPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

type semver struct {
	core [3]uint64
	pre  []string
}

func parseVersion(value string) (semver, bool) {
	var parsed semver
	if len(value) > 256 {
		return parsed, false
	}
	parts := semverPattern.FindStringSubmatch(value)
	if parts == nil {
		return parsed, false
	}
	for index := range parsed.core {
		part, err := strconv.ParseUint(parts[index+1], 10, 64)
		if err != nil {
			return parsed, false
		}
		parsed.core[index] = part
	}
	if parts[4] != "" {
		parsed.pre = strings.Split(parts[4], ".")
		for _, identifier := range parsed.pre {
			if numeric(identifier) && len(identifier) > 1 && identifier[0] == '0' {
				return parsed, false
			}
		}
	}
	return parsed, true
}

func isNewer(latest, current string) bool {
	a, validA := parseVersion(latest)
	b, validB := parseVersion(current)
	if !validA || !validB {
		return false
	}
	for index, part := range a.core {
		if part != b.core[index] {
			return part > b.core[index]
		}
	}
	if len(a.pre) == 0 || len(b.pre) == 0 {
		return len(a.pre) == 0 && len(b.pre) != 0
	}
	for index, part := range a.pre {
		if index >= len(b.pre) {
			return true
		}
		other := b.pre[index]
		if part == other {
			continue
		}
		numericA, numericB := numeric(part), numeric(other)
		if numericA != numericB {
			return !numericA
		}
		if numericA && len(part) != len(other) {
			return len(part) > len(other)
		}
		return part > other
	}
	return false
}

func numeric(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return value != ""
}
