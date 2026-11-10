package features

import (
	"errors"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestUnapplied(t *testing.T) {
	on, off := gitoperationslib.Ptr(true), gitoperationslib.Ptr(false)
	want := gitoperationslib.RepoFeatures{Actions: on, Wiki: off, Issues: on}
	got := gitoperationslib.RepoFeatures{Actions: off, Wiki: off}
	if missing := Unapplied(want, got); !slices.Equal(missing, []string{"actions on", "issues on"}) {
		t.Errorf("Unapplied = %v", missing)
	}
	if Requested(gitoperationslib.RepoFeatures{}) || !Requested(want) {
		t.Error("Requested")
	}
	repo := &gitoperationslib.Repo{Features: got}
	if back, err := Check(gitoperationslib.BackendForgejo, "repos.edit", repo, want, "why"); back != repo || !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Check = %v, %v", back, err)
	}
	if err := Refuse(gitoperationslib.BackendGitHub, "repos.edit", gitoperationslib.RepoFeatures{Packages: on}, "releases", "packages"); !errors.Is(err, gitoperationslib.ErrNotSupported) {
		t.Errorf("Refuse = %v", err)
	}
	if err := Refuse(gitoperationslib.BackendGitHub, "repos.edit", want, "releases", "packages"); err != nil {
		t.Errorf("Refuse(allowed) = %v", err)
	}
}
