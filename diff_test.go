package gitoperationslib

import "testing"

func TestDiffFormatValid(t *testing.T) {
	for format, want := range map[DiffFormat]bool{DiffUnified: true, DiffPatch: true, "": false, "html": false} {
		if got := format.Valid(); got != want {
			t.Errorf("%q.Valid() = %v, want %v", format, got, want)
		}
	}
}

func TestCreateReviewCommentOptionsValidate(t *testing.T) {
	tests := []struct {
		name string
		opts CreateReviewCommentOptions
		ok   bool
	}{
		{"new line", CreateReviewCommentOptions{Path: "a", Body: "b", Line: 1}, true},
		{"old line", CreateReviewCommentOptions{Path: "a", Body: "b", OldLine: 1}, true},
		{"no path", CreateReviewCommentOptions{Body: "b", Line: 1}, false},
		{"no body", CreateReviewCommentOptions{Path: "a", Line: 1}, false},
		{"no line", CreateReviewCommentOptions{Path: "a", Body: "b"}, false},
		{"both lines", CreateReviewCommentOptions{Path: "a", Body: "b", Line: 1, OldLine: 1}, false},
	}
	for _, tt := range tests {
		if err := tt.opts.Validate(); (err == nil) != tt.ok {
			t.Errorf("%s: err = %v, want ok = %v", tt.name, err, tt.ok)
		}
	}
}
