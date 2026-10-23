package gitoperationslib

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestJSONTags holds every exported field of every exported struct, in all
// public packages, to a camelCase json tag (or "-"). Embedded structs stay
// untagged so their fields are flattened.
func TestJSONTags(t *testing.T) {
	camel := regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	for _, dir := range []string{".", "webhook", "forgejo", "github", "bitbucket"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				spec, ok := n.(*ast.TypeSpec)
				if !ok || !spec.Name.IsExported() {
					return true
				}
				st, ok := spec.Type.(*ast.StructType)
				if !ok {
					return true
				}
				for _, f := range st.Fields.List {
					if len(f.Names) == 0 { // embedded
						continue
					}
					for _, name := range f.Names {
						if !name.IsExported() {
							continue
						}
						where := fset.Position(name.Pos()).String() + ": " + spec.Name.Name + "." + name.Name
						if f.Tag == nil {
							t.Errorf("%s has no json tag", where)
							continue
						}
						key, _, _ := strings.Cut(reflect.StructTag(strings.Trim(f.Tag.Value, "`")).Get("json"), ",")
						if key != "-" && !camel.MatchString(key) {
							t.Errorf("%s: json key %q is not camelCase", where, key)
						}
					}
				}
				return true
			})
		}
	}
}

func marshalMap(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestJSONCurrentUser(t *testing.T) {
	me := CurrentUser{
		User:       User{ID: 3, Login: "alice", HTMLURL: "https://h/alice", CreatedAt: time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)},
		Emails:     []Email{{Address: "a@example.com", Primary: true}},
		Incomplete: errors.New("teams: HTTP 403"),
		Missing:    []PartError{{Part: "teams", Status: 403, Message: "HTTP 403"}, {Part: "GPG keys", Message: "not supported"}},
	}
	m := marshalMap(t, me)

	// Embedded User is flattened; common fields are present even when empty.
	for _, key := range []string{"id", "login", "htmlUrl", "createdAt", "email", "fullName", "followers", "emails", "repos"} {
		if _, ok := m[key]; !ok {
			t.Errorf("key %q missing", key)
		}
	}
	// Forge-specific fields, nil pointers and zero times are omitted, and so
	// is the error itself.
	for _, key := range []string{"User", "Incomplete", "incomplete", "lastLogin", "company", "pronouns", "plan",
		"twoFactorAuth", "settings", "uuid", "updatedAt"} {
		if _, ok := m[key]; ok {
			t.Errorf("key %q present", key)
		}
	}
	missing, _ := m["missing"].([]any)
	if len(missing) != 2 {
		t.Fatalf("missing = %v", m["missing"])
	}
	first, second := missing[0].(map[string]any), missing[1].(map[string]any)
	if first["part"] != "teams" || first["status"] != 403.0 || first["message"] != "HTTP 403" {
		t.Errorf("missing[0] = %v", first)
	}
	if _, ok := second["status"]; ok {
		t.Errorf("missing[1] has a status: %v", second)
	}

	// A false two-factor flag is reported; only nil is omitted.
	me.TwoFactorAuth = Ptr(false)
	me.Missing = nil
	m = marshalMap(t, me)
	if v, ok := m["twoFactorAuth"]; !ok || v != false {
		t.Errorf("twoFactorAuth = %v, %v", v, ok)
	}
	if _, ok := m["missing"]; ok {
		t.Error("missing present on a complete result")
	}
}

func TestJSONOptions(t *testing.T) {
	// Secrets are never serialised.
	m := marshalMap(t, CreateWebhookOptions{URL: "https://h", Secret: "s3cret", AuthorizationHeader: "Bearer x", Events: []HookEvent{HookEventPush}})
	if got := slices.Sorted(maps.Keys(m)); !slices.Equal(got, []string{"events", "url"}) {
		t.Errorf("CreateWebhookOptions keys = %v", got)
	}
	if b, _ := json.Marshal(EditUserOptions{Password: Ptr("pw")}); string(b) != "{}" {
		t.Errorf("EditUserOptions = %s", b)
	}

	// Options load from camelCase JSON; absent fields stay nil.
	var edit EditRepoOptions
	if err := json.Unmarshal([]byte(`{"defaultBranch": "main", "visibility": "private"}`), &edit); err != nil {
		t.Fatal(err)
	}
	if edit.DefaultBranch == nil || *edit.DefaultBranch != "main" || edit.Visibility == nil || *edit.Visibility != VisibilityPrivate || edit.Name != nil {
		t.Errorf("EditRepoOptions = %+v", edit)
	}

	// Embedded ListOptions is flattened.
	var list ListIssuesOptions
	if err := json.Unmarshal([]byte(`{"page": 2, "perPage": 10, "state": "closed"}`), &list); err != nil {
		t.Fatal(err)
	}
	if list.Page != 2 || list.PerPage != 10 || list.State != IssueStateClosed {
		t.Errorf("ListIssuesOptions = %+v", list)
	}
}
