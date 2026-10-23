package forgejo

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
)

// Version is a Forgejo semantic version.
type Version struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
	Patch int `json:"patch"`
}

// MinimumVersion is the oldest Forgejo release this library supports.
var MinimumVersion = Version{11, 0, 10}

// ParseVersion parses a Forgejo version string such as "15.0.2+gitea-1.22.0".
// Only the part before "+" is used; a pre-release suffix ("-dev...") and a
// leading "v" are ignored.
func ParseVersion(s string) (Version, error) {
	core, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(s), "v"), "+")
	core, _, _ = strings.Cut(core, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("forgejo: invalid version %q", s)
	}
	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("forgejo: invalid version %q", s)
		}
		nums[i] = n
	}
	return Version{nums[0], nums[1], nums[2]}, nil
}

// Compare returns -1, 0 or +1 depending on whether v is older than, equal to,
// or newer than o.
func (v Version) Compare(o Version) int {
	if c := cmp.Compare(v.Major, o.Major); c != 0 {
		return c
	}
	if c := cmp.Compare(v.Minor, o.Minor); c != 0 {
		return c
	}
	return cmp.Compare(v.Patch, o.Patch)
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }
