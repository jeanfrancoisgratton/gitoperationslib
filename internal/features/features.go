// Package features checks repository features against what a server reports, as
// some servers answer success while keeping their own value.
package features

import (
	"strings"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// Each lists every feature with its name, so the backends handle them alike.
func Each(f *gitoperationslib.RepoFeatures) []struct {
	Name  string
	Value **bool
} {
	return []struct {
		Name  string
		Value **bool
	}{
		{"actions", &f.Actions}, {"issues", &f.Issues}, {"wiki", &f.Wiki}, {"pull requests", &f.PullRequests},
		{"projects", &f.Projects}, {"releases", &f.Releases}, {"packages", &f.Packages}, {"discussions", &f.Discussions},
	}
}

// Requested reports whether any feature is set.
func Requested(f gitoperationslib.RepoFeatures) bool {
	for _, x := range Each(&f) {
		if *x.Value != nil {
			return true
		}
	}
	return false
}

// Unapplied names the features want sets to a value got does not report, such as
// "wiki on". A feature got does not report at all counts too.
func Unapplied(want, got gitoperationslib.RepoFeatures) []string {
	wanted, reported := Each(&want), Each(&got)
	var out []string
	for i, x := range wanted {
		w, g := *x.Value, *reported[i].Value
		if w != nil && (g == nil || *g != *w) {
			state := "off"
			if *w {
				state = "on"
			}
			out = append(out, x.Name+" "+state)
		}
	}
	return out
}

// Check returns the repository with ErrNotSupported when the server kept its own
// value for a requested feature. why says what may cause it.
func Check(backend gitoperationslib.Backend, op string, repo *gitoperationslib.Repo, want gitoperationslib.RepoFeatures, why string) (*gitoperationslib.Repo, error) {
	if missing := Unapplied(want, repo.Features); len(missing) > 0 {
		return repo, gitoperationslib.NotSupported(backend, op, "the server did not apply "+strings.Join(missing, ", ")+"; "+why)
	}
	return repo, nil
}

// Refuse returns ErrNotSupported if want sets one of names, which the backend
// does not have.
func Refuse(backend gitoperationslib.Backend, op string, want gitoperationslib.RepoFeatures, names ...string) error {
	for _, x := range Each(&want) {
		for _, n := range names {
			if x.Name == n && *x.Value != nil {
				return gitoperationslib.NotSupported(backend, op, "the "+n+" feature")
			}
		}
	}
	return nil
}
