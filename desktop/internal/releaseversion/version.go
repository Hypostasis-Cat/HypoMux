// Package releaseversion defines the release formats shared by builds and updates.
package releaseversion

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var releasePattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(beta|rc)\.([1-9][0-9]*))?$`)
var legacyPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){1,3}$`)

type Version struct {
	Major, Minor, Patch int
	Stage               string
	Sequence            int
}

func Parse(value string) (Version, error) {
	match := releasePattern.FindStringSubmatch(value)
	if match == nil {
		return Version{}, fmt.Errorf("invalid release version %q: use X.Y.Z, X.Y.Z-beta.N or X.Y.Z-rc.N", value)
	}
	v := Version{Stage: match[4]}
	for i, dest := range []*int{&v.Major, &v.Minor, &v.Patch} {
		n, err := strconv.Atoi(match[i+1])
		if err != nil || n > 65535 {
			return Version{}, fmt.Errorf("version components must be between 0 and 65535")
		}
		*dest = n
	}
	if v.Stage != "" {
		n, err := strconv.Atoi(match[5])
		if err != nil || n > 29999 {
			return Version{}, fmt.Errorf("beta/rc sequence must be between 1 and 29999")
		}
		v.Sequence = n
	}
	return v, nil
}

func (v Version) String() string {
	value := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Stage != "" {
		value += fmt.Sprintf("-%s.%d", v.Stage, v.Sequence)
	}
	return value
}

func (v Version) Prerelease() bool { return v.Stage != "" }

func (v Version) Channel() string {
	if v.Prerelease() {
		return "update-channel-preview"
	}
	return "update-channel"
}

// Windows uses four 16-bit numeric components; stable sorts after beta and RC.
func (v Version) Windows() string {
	revision := 65535
	if v.Stage == "beta" {
		revision = v.Sequence
	}
	if v.Stage == "rc" {
		revision = 30000 + v.Sequence
	}
	return fmt.Sprintf("%d.%d.%d.%d", v.Major, v.Minor, v.Patch, revision)
}

// Key retains the old updater's two-to-four-part numeric versions while adding
// the strictly validated beta/rc formats. Invalid versions never compare newer.
func Key(value string) []int {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "v") || strings.HasPrefix(value, "V") {
		value = value[1:]
	}
	if v, err := Parse(value); err == nil {
		rank := 2
		if v.Stage == "beta" {
			rank = 0
		}
		if v.Stage == "rc" {
			rank = 1
		}
		return []int{v.Major, v.Minor, v.Patch, 0, rank, v.Sequence}
	}
	if !legacyPattern.MatchString(value) {
		return nil
	}
	key := []int{0, 0, 0, 0, 2, 0}
	for i, part := range strings.Split(value, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		key[i] = n
	}
	return key
}

func IsPrerelease(value string) bool {
	key := Key(value)
	return key != nil && key[4] < 2
}
