package mcprolog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitClauses(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{"simple", "a(1). a(2).", []string{"a(1).", "a(2)."}},
		{"rule over lines", "p(X) :-\n  q(X),\n  r(X).", []string{"p(X) :-\n  q(X),\n  r(X)."}},
		{"dot in quoted atom", `a('x.y').`, []string{`a('x.y').`}},
		{"dot in string", `a("x. y").`, []string{`a("x. y").`}},
		{"escaped quote", `a('it\'s').`, []string{`a('it\'s').`}},
		{"doubled quote", `a('it''s').`, []string{`a('it''s').`}},
		{"char code dot", `a(0'.).`, []string{`a(0'.).`}},
		{"char code quote", `a(0'').`, []string{`a(0'').`}},
		{"line comment", "% hi\na(1).", []string{"% hi\na(1)."}},
		{"block comment", "/* a. b. */\na(1).", []string{"/* a. b. */\na(1)."}},
		{"decimal is not a terminator", "a(1.5).", []string{"a(1.5)."}},
		{"trailing comment only", "a(1).\n% done", []string{"a(1)."}},
		{"nothing but a comment", "% todo", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SplitClauses(tc.src)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSplitClausesRejectsMalformedSource(t *testing.T) {
	for _, src := range []string{"a(1)", "a('x).", "/* unterminated"} {
		t.Run(src, func(t *testing.T) {
			_, err := SplitClauses(src)
			assert.Error(t, err)
		})
	}
}
