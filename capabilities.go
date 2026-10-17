package gitoperationslib

import "strings"

// Capability is a bit set of features a Client supports.
type Capability uint64

const (
	CapRepos Capability = 1 << iota
	CapMirrors
	CapMigrate
	CapAdmin
	CapWebhooks
	CapSystemWebhooks
	CapReleases
	CapPullRequests
	CapExternalAssets
)

var capabilityNames = []struct {
	cap  Capability
	name string
}{
	{CapRepos, "repos"},
	{CapMirrors, "mirrors"},
	{CapMigrate, "migrate"},
	{CapAdmin, "admin"},
	{CapWebhooks, "webhooks"},
	{CapSystemWebhooks, "system-webhooks"},
	{CapReleases, "releases"},
	{CapPullRequests, "pull-requests"},
	{CapExternalAssets, "external-assets"},
}

// Has reports whether c includes every capability in want.
func (c Capability) Has(want Capability) bool { return c&want == want }

// String lists the named capabilities in c, separated by "|".
func (c Capability) String() string {
	var names []string
	for _, n := range capabilityNames {
		if c.Has(n.cap) {
			names = append(names, n.name)
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, "|")
}
