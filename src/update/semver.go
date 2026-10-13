// Package update checks GitHub for a newer knoxmgr release. It only reports; it never downloads.
package update

import (
	"regexp"
	"strconv"
)

var (
	// A release tag is exactly vX.Y.Z; anything else from GitHub is ignored.
	tagRe = regexp.MustCompile(`^v(\d{1,9})\.(\d{1,9})\.(\d{1,9})$`)
	// The running version may be a git-describe string such as v0.3.0-2-gabc-dirty; only the base counts.
	currentRe = regexp.MustCompile(`^v?(\d{1,9})\.(\d{1,9})\.(\d{1,9})(?:-.+)?$`)
)

type semver [3]int

func parse(re *regexp.Regexp, s string) (semver, bool) {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return semver{}, false
	}
	var v semver
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1]) // digits only and at most 9 of them
	}
	return v, true
}

// compare returns -1, 0 or 1 as a is older than, equal to or newer than b.
func compare(a, b semver) int {
	for i := range a {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}
