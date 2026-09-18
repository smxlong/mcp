package scoretex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func session(t *testing.T) *mcp.ClientSession {
	t.Helper()
	s, er := New(Config{})
	require.NoError(t, er)
	st, ct := mcp.NewInMemoryTransports()
	ss, er := s.Connect(context.Background(), st, nil)
	require.NoError(t, er)
	cs, er := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).Connect(context.Background(), ct, nil)
	require.NoError(t, er)
	t.Cleanup(func() { _ = cs.Close(); _ = ss.Close() })
	return cs
}
func call(t *testing.T, cs *mcp.ClientSession, name string, args Object) Object {
	t.Helper()
	r, er := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "score." + name, Arguments: args})
	require.NoError(t, er)
	require.False(t, r.IsError, "%v", r.Content)
	b, er := json.Marshal(r.StructuredContent)
	require.NoError(t, er)
	var out Object
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}
func TestToolsExposeStructuredErrorsAndSchemas(t *testing.T) {
	cs := session(t)
	n := 0
	for tool, er := range cs.Tools(context.Background(), nil) {
		require.NoError(t, er)
		assert.NotEmpty(t, tool.Description)
		assert.NotNil(t, tool.InputSchema)
		assert.NotNil(t, tool.OutputSchema)
		n++
	}
	assert.Equal(t, 10, n)
	r, er := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "score.inspect", Arguments: Object{"score_id": "absent"}})
	require.NoError(t, er)
	require.True(t, r.IsError)
	b, _ := json.Marshal(r.StructuredContent)
	assert.Contains(t, string(b), "SCORE_NOT_FOUND")
}
func TestConcurrentEditsCommitExactlyOneRevision(t *testing.T) {
	cs := session(t)
	created := call(t, cs, "create", Object{})
	var wg sync.WaitGroup
	results := make(chan *mcp.CallToolResult, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Go(func() {
			r, er := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "score.patch", Arguments: Object{"score_id": created["score_id"], "base_revision_id": created["revision_id"], "operations": []any{Object{"op": "set", "id": created["root_id"], "path": "/metadata", "value": Object{"title": "Concurrent"}}}}})
			results <- r
			errs <- er
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for er := range errs {
		require.NoError(t, er)
	}
	success := 0
	for r := range results {
		if !r.IsError {
			success++
		} else {
			b, _ := json.Marshal(r.StructuredContent)
			assert.Contains(t, string(b), "REVISION_CONFLICT")
		}
	}
	assert.Equal(t, 1, success)
}
func TestConcurrentRetriesReturnSameIDs(t *testing.T) {
	cs := session(t)
	var wg sync.WaitGroup
	results := make(chan *mcp.CallToolResult, 8)
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			r, er := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "score.create", Arguments: Object{"idempotency_key": "once"}})
			if er != nil {
				t.Error(er)
				return
			}
			results <- r
		})
	}
	wg.Wait()
	close(results)
	var first string
	for r := range results {
		require.False(t, r.IsError)
		b, _ := json.Marshal(r.StructuredContent)
		if first == "" {
			first = string(b)
		}
		assert.Equal(t, first, string(b))
	}
}
func TestRejectedEditsAndSaveFailureLeaveStateUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	e, er := NewEngine(path)
	require.NoError(t, er)
	ctx := context.Background()
	created, er := e.Call(ctx, "create", Object{})
	require.NoError(t, er)
	edit := func(ops []any) error {
		_, er := e.Call(ctx, "patch", Object{"score_id": created["score_id"], "base_revision_id": created["revision_id"], "operations": ops})
		return er
	}
	require.Error(t, edit([]any{Object{"op": "set", "id": created["root_id"], "path": "/metadata", "value": Object{"title": "partial"}}, Object{"op": "test", "id": created["root_id"], "path": "/metadata/title", "value": "other"}}))
	e.path = filepath.Join(t.TempDir(), "missing", "state.json")
	require.Error(t, edit([]any{Object{"op": "set", "id": created["root_id"], "path": "/metadata", "value": Object{"title": "not saved"}}}))
	got, er := e.Call(ctx, "inspect", Object{"score_id": created["score_id"]})
	require.NoError(t, er)
	assert.Equal(t, created["revision_id"], got["revision_id"])
	loaded, er := NewEngine(path)
	require.NoError(t, er)
	assert.True(t, equal(e.state, loaded.state))
}
func TestRationalArithmeticIsExactAndBounded(t *testing.T) {
	a, er := calc(rat(1, 3), rat(1, 6), '+')
	require.NoError(t, er)
	assert.Equal(t, rat(1, 2), a)
	_, er = calc(rat(9223372036854775807, 1), rat(1, 1), '+')
	require.Error(t, er)
	assert.Equal(t, "LIMIT_EXCEEDED", er.(*Fault).Code)
	_, er = rational(Object{"n": 1, "d": 0})
	require.Error(t, er)
}
func TestUnknownFieldsAndBrokenReferencesNeverCommit(t *testing.T) {
	e, er := NewEngine("")
	require.NoError(t, er)
	ctx := context.Background()
	c, er := e.Call(ctx, "create", Object{})
	require.NoError(t, er)
	for _, entity := range []Object{{"kind": "note", "voice_id": "missing", "onset": rat(0, 1), "duration": rat(1, 4), "pitch": Object{"step": "C", "octave": 4, "alter": rat(0, 1)}}, {"kind": "group", "group_type": "phrase", "member_ids": []any{}, "typo": true}} {
		_, er = e.Call(ctx, "patch", Object{"score_id": c["score_id"], "base_revision_id": c["revision_id"], "operations": []any{Object{"op": "create", "entity": entity}}})
		require.Error(t, er)
		assert.Equal(t, c["revision_id"], e.state.Scores[str(c["score_id"])].Current)
	}
}
func TestHistoricalIDsCannotBeReused(t *testing.T) {
	e, _ := NewEngine("")
	ctx := context.Background()
	c, er := e.Call(ctx, "create", Object{})
	require.NoError(t, er)
	sid, rev := c["score_id"], c["revision_id"]
	for _, ops := range [][]any{{Object{"op": "create", "entity": Object{"id": "old", "kind": "group", "group_type": "phrase", "member_ids": []any{}}}}, {Object{"op": "delete", "id": "old"}}} {
		r, er := e.Call(ctx, "patch", Object{"score_id": sid, "base_revision_id": rev, "operations": ops})
		require.NoError(t, er)
		rev = r["revision_id"]
	}
	_, er = e.Call(ctx, "patch", Object{"score_id": sid, "base_revision_id": rev, "operations": []any{Object{"op": "create", "entity": Object{"id": "old", "kind": "group", "group_type": "phrase", "member_ids": []any{}}}}})
	require.Error(t, er)
}

func TestExactIntegerSurvivesWireAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	server, er := New(Config{StatePath: path})
	require.NoError(t, er)
	st, ct := mcp.NewInMemoryTransports()
	ss, er := server.Connect(context.Background(), st, nil)
	require.NoError(t, er)
	defer func() { _ = ss.Close() }()
	cs, er := mcp.NewClient(&mcp.Implementation{Name: "precision", Version: "test"}, nil).Connect(context.Background(), ct, nil)
	require.NoError(t, er)
	defer func() { _ = cs.Close() }()
	c := call(t, cs, "create", Object{})
	call(t, cs, "patch", Object{"score_id": c["score_id"], "base_revision_id": c["revision_id"], "operations": []any{Object{"op": "create", "entity": Object{"id": "precise", "kind": "directive", "scope_ids": []any{c["root_id"]}, "onset": rat(9007199254740993, 2), "directive_type": "tempo", "value": Object{"bpm": 96, "beat_unit": rat(1, 4)}}}}})
	e, er := NewEngine(path)
	require.NoError(t, er)
	s := e.state.Scores[str(c["score_id"])]
	r, er := rational(s.Revisions[s.Current]["precise"]["onset"])
	require.NoError(t, er)
	assert.Equal(t, "9007199254740993/2", r.RatString())
}

func TestTransformsPreserveExactPitchAndTime(t *testing.T) {
	for _, tt := range []struct {
		name       string
		parameters Object
		field      string
		want       any
	}{
		{"transpose", Object{"chromatic_semitones": 2, "diatonic_steps": 1, "spelling_policy": "preserve_interval_spelling"}, "pitch", Object{"step": "D", "octave": 4, "alter": rat(0, 1)}},
		{"invert", Object{"axis": Object{"step": "D", "octave": 4, "alter": rat(0, 1)}, "space": "chromatic", "spelling_policy": "sharps"}, "pitch", Object{"step": "E", "octave": 4, "alter": rat(0, 1)}},
		{"augment_duration", Object{"factor": rat(3, 2)}, "duration", rat(3, 8)},
		{"diminish_duration", Object{"factor": rat(2, 3)}, "duration", rat(1, 6)},
		{"shift_time", Object{"offset": rat(1, 3)}, "onset", rat(1, 3)},
		{"retrograde", Object{"start": rat(0, 1), "end": rat(1, 1)}, "onset", rat(3, 4)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tx := &transaction{g: Graph{"note": Object{"id": "note", "kind": "note", "voice_id": "voice", "onset": rat(0, 1), "duration": rat(1, 4), "pitch": Object{"step": "C", "octave": 4, "alter": rat(0, 1)}, "x-test": Object{"n": 3, "d": 9}}}}
			require.NoError(t, tx.transform([]string{"note"}, tt.name, tt.parameters))
			assert.True(t, equal(tt.want, tx.g["note"][tt.field]), "got %v", tx.g["note"][tt.field])
			assert.True(t, equal(Object{"n": 3, "d": 9}, tx.g["note"]["x-test"]))
		})
	}
}

func TestInstrumentChangePreservesSoundingPitch(t *testing.T) {
	tx := &transaction{root: "root", used: map[string]bool{}, g: Graph{
		"root":     {"id": "root", "kind": "score", "part_ids": []any{"part"}},
		"part":     {"id": "part", "kind": "part", "instrument_ids": []any{"concert", "clarinet"}, "staff_ids": []any{"staff"}},
		"concert":  {"id": "concert", "kind": "instrument", "transposition": rat(0, 1)},
		"clarinet": {"id": "clarinet", "kind": "instrument", "transposition": rat(-2, 1)},
		"staff":    {"id": "staff", "kind": "staff", "voice_ids": []any{"voice"}},
		"voice":    {"id": "voice", "kind": "voice"},
		"note":     {"id": "note", "kind": "note", "voice_id": "voice", "onset": rat(0, 1), "duration": rat(1, 4), "pitch": Object{"step": "C", "octave": 4, "alter": rat(0, 1)}},
	}}
	require.NoError(t, tx.transform([]string{"part"}, "change_instrument", Object{"instrument_id": "clarinet", "time": rat(0, 1), "pitch_policy": "preserve_sounding"}))
	assert.True(t, equal(rat(2, 1), obj(tx.g["note"]["pitch"])["alter"]))
}

func TestStreamableHTTPUsesSameSemanticEngine(t *testing.T) {
	s, er := New(Config{})
	require.NoError(t, er)
	h := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
	defer h.Close()
	cs, er := mcp.NewClient(&mcp.Implementation{Name: "http-test", Version: "test"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: h.URL}, nil)
	require.NoError(t, er)
	defer func() { _ = cs.Close() }()
	created := call(t, cs, "create", Object{})
	got := call(t, cs, "inspect", Object{"score_id": created["score_id"]})
	assert.Equal(t, created["revision_id"], got["revision_id"])
}
