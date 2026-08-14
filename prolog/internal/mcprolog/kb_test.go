package mcprolog

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatePersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kb.json")

	kb, err := NewKB(path)
	require.NoError(t, err)
	_, err = kb.CreateUnit("u", "desc")
	require.NoError(t, err)
	_, err = kb.AddClauses("u", "a(1).\na(2).")
	require.NoError(t, err)
	_, err = kb.LoadUnit("u")
	require.NoError(t, err)

	reopened, err := NewKB(path)
	require.NoError(t, err)
	units, scope := reopened.ListUnits()
	assert.Equal(t, []UnitSummary{{Name: "u", Description: "desc", Clauses: 2, InScope: true}}, units)
	assert.Equal(t, []string{"u"}, scope)
}
