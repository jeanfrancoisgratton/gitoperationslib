package forgejo

import (
	"context"
	"io"
	"net/http"
	"slices"
	"testing"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

func TestRemoveLabelOrganisationLabel(t *testing.T) {
	const base = "/api/v1/repos/acme/widget/issues/5/labels/"
	tests := []struct {
		name    string
		label   string
		refused bool // the server refuses the label's ID
		want    []string
		wantErr int
	}{
		{name: "repository label goes by ID", label: "bug", want: []string{"1"}},
		{name: "organisation label falls back on its name", label: "org-only", refused: true, want: []string{"10", "org-only"}},
		{name: "a numeric name could be taken for an ID", label: "123", refused: true, want: []string{"11"}, wantErr: 422},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var deletes []string
			c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/repos/acme/widget/labels":
					io.WriteString(w, `[{"id":1,"name":"bug"}]`)
				case r.URL.Path == "/api/v1/orgs/acme/labels":
					io.WriteString(w, `[{"id":10,"name":"org-only"},{"id":11,"name":"123"}]`)
				case r.Method == http.MethodDelete:
					id := r.URL.Path[len(base):]
					deletes = append(deletes, id)
					if tt.refused && len(deletes) == 1 {
						w.WriteHeader(http.StatusUnprocessableEntity)
						io.WriteString(w, `{"message":"label does not exist"}`)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
			})
			err := c.Issues().RemoveLabel(context.Background(), "acme", "widget", 5, tt.label)
			if got := gitoperationslib.StatusCode(err); got != tt.wantErr {
				t.Errorf("err = %v, want status %d", err, tt.wantErr)
			}
			if !slices.Equal(deletes, tt.want) {
				t.Errorf("deletes = %q, want %q", deletes, tt.want)
			}
		})
	}
}

func TestLabelIDsOrganisationLabels(t *testing.T) {
	const repoLabels, orgLabels = "/api/v1/repos/acme/widget/labels", "/api/v1/orgs/acme/labels"
	const inRepo = `[{"id":1,"name":"bug"},{"id":2,"name":"shared"}]`
	const inOrg = `[{"id":10,"name":"org-only"},{"id":20,"name":"shared"}]`

	tests := []struct {
		name         string
		names        []string
		orgStatus    int
		want         []int64
		wantStatus   int
		wantOrgAsked bool
	}{
		{name: "repository labels need no second listing", names: []string{"bug"}, orgStatus: 200, want: []int64{1}},
		{name: "organisation label", names: []string{"org-only"}, orgStatus: 200, want: []int64{10}, wantOrgAsked: true},
		{name: "both kinds, in order", names: []string{"org-only", "bug"}, orgStatus: 200, want: []int64{10, 1}, wantOrgAsked: true},
		{name: "the repository's label wins", names: []string{"org-only", "shared"}, orgStatus: 200, want: []int64{10, 2}, wantOrgAsked: true},
		{name: "unknown everywhere", names: []string{"nope"}, orgStatus: 200, wantStatus: 404, wantOrgAsked: true},
		{name: "owner is a user", names: []string{"nope"}, orgStatus: 404, wantStatus: 404, wantOrgAsked: true},
		{name: "organisation listing fails", names: []string{"nope"}, orgStatus: 403, wantStatus: 403, wantOrgAsked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orgAsked := false
			c := fakeForgejo(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case repoLabels:
					io.WriteString(w, inRepo)
				case orgLabels:
					orgAsked = true
					w.WriteHeader(tt.orgStatus)
					if tt.orgStatus == http.StatusOK {
						io.WriteString(w, inOrg)
					}
				default:
					t.Errorf("unexpected %s", r.URL.Path)
				}
			})
			ids, err := c.labelIDs(context.Background(), "acme", "widget", tt.names)
			if got := gitoperationslib.StatusCode(err); got != tt.wantStatus {
				t.Errorf("err = %v, want status %d", err, tt.wantStatus)
			}
			if !slices.Equal(ids, tt.want) {
				t.Errorf("ids = %v, want %v", ids, tt.want)
			}
			if orgAsked != tt.wantOrgAsked {
				t.Errorf("organisation listed = %v, want %v", orgAsked, tt.wantOrgAsked)
			}
		})
	}
}
