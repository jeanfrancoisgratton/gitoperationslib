// Package features checks repository settings against what a server reports, as
// some servers answer success while keeping their own value. It works on structs of
// pointer fields, such as RepoFeatures and MergeSettings, named by their JSON tags.
package features

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/jeanfrancoisgratton/gitoperationslib"
)

// fields lists a settings struct's pointer fields with their JSON names.
func fields(v reflect.Value) []struct {
	Name  string
	Value reflect.Value
} {
	var out []struct {
		Name  string
		Value reflect.Value
	}
	for i := range v.NumField() {
		name, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("json"), ",")
		out = append(out, struct {
			Name  string
			Value reflect.Value
		}{name, v.Field(i)})
	}
	return out
}

// Requested reports whether any field of the settings is set.
func Requested(settings ...any) bool {
	for _, s := range settings {
		for _, f := range fields(reflect.ValueOf(s)) {
			if !f.Value.IsNil() {
				return true
			}
		}
	}
	return false
}

// Unapplied names the fields want sets to a value got does not report, such as
// "wiki on". A field got does not report at all counts too.
func Unapplied(want, got any) []string {
	w, g := fields(reflect.ValueOf(want)), fields(reflect.ValueOf(got))
	var out []string
	for i, f := range w {
		if f.Value.IsNil() {
			continue
		}
		have := g[i].Value
		if have.IsNil() || !reflect.DeepEqual(f.Value.Elem().Interface(), have.Elem().Interface()) {
			out = append(out, describe(f.Name, f.Value.Elem()))
		}
	}
	return out
}

func describe(name string, v reflect.Value) string {
	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			return name + " on"
		}
		return name + " off"
	case reflect.String:
		return fmt.Sprintf("%s %q", name, v.String())
	}
	return name
}

// Check returns the repository with ErrNotSupported when the server kept its own
// value for a requested setting. pairs alternate what was asked and what the
// repository reports; why says what may cause it.
func Check(backend gitoperationslib.Backend, op string, repo *gitoperationslib.Repo, why string, pairs ...any) (*gitoperationslib.Repo, error) {
	var missing []string
	for i := 0; i+1 < len(pairs); i += 2 {
		missing = append(missing, Unapplied(pairs[i], pairs[i+1])...)
	}
	if len(missing) > 0 {
		return repo, gitoperationslib.NotSupported(backend, op, "the server did not apply "+strings.Join(missing, ", ")+"; "+why)
	}
	return repo, nil
}

// Refuse returns ErrNotSupported if settings sets one of names, which the backend
// does not have.
func Refuse(backend gitoperationslib.Backend, op string, settings any, names ...string) error {
	for _, f := range fields(reflect.ValueOf(settings)) {
		for _, n := range names {
			if f.Name == n && !f.Value.IsNil() {
				return gitoperationslib.NotSupported(backend, op, "the "+n+" setting")
			}
		}
	}
	return nil
}
