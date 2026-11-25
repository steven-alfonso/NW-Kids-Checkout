package conditional

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_matches(t *testing.T) {
	const etag = `W/"abc123"`

	tests := []struct {
		name        string
		ifNoneMatch string
		want        bool
	}{
		{"absent", "", false},
		{"exact weak match", `W/"abc123"`, true},
		{"strong form of the same opaque tag", `"abc123"`, true},
		{"one of several", `"other", W/"abc123"`, true},
		{"several, match last", `"a", "b", W/"abc123"`, true},
		{"wildcard", "*", true},
		{"wildcard with padding", " * ", true},
		{"padding around the tag", `  W/"abc123"  `, true},
		{"trailing comma", `W/"abc123",`, true},
		{"different tag", `W/"other"`, false},
		{"prefix of the tag", `W/"abc12"`, false},
		{"tag with trailing junk", `"abc123"junk`, false},
		{"unquoted", `abc123`, false},
		{"quoted wildcard", `"*"`, false},
		{"lowercase weak prefix is a different token", `w/"abc123"`, false},
		{"uppercase hex does not match", `W/"ABC123"`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, matches(tc.ifNoneMatch, etag))
		})
	}
}
