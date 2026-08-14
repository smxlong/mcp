package mcprolog

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestSession wires a real server to a real client over an in-memory
// transport. This exercises the full path a client would take -- schema
// generation, argument validation, marshalling -- without spawning a process,
// which is why it is the SDK's recommended way to test a server.
func newTestSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	server, err := New(Config{
		StatePath:  filepath.Join(t.TempDir(), "kb.json"),
		StackLimit: "256m",
		Sandbox:    true,
		Serialize:  true,
		Version:    "test",
	})
	if err != nil {
		t.Skipf("swipl unavailable: %v", err)
	}

	st, ct := mcp.NewInMemoryTransports()
	_, err = server.Connect(ctx, st, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call invokes a tool, fails the test if it reports an error, and decodes the
// structured output into out when one is given.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err, "%s: protocol error", name)
	body := resultText(res)
	require.False(t, res.IsError, "%s: tool error: %s", name, body)
	if out != nil {
		raw, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, out), "%s: decoding structured output", name)
	}
	return body
}

// callErr invokes a tool that is expected to fail, and returns the error text.
func callErr(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err, "%s: protocol error", name)
	require.True(t, res.IsError, "%s: expected a tool error", name)
	return resultText(res)
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// bind collects one variable's binding from every solution.
func bind(solutions []map[string]string, name string) []string {
	out := make([]string, 0, len(solutions))
	for _, sol := range solutions {
		out = append(out, sol[name])
	}
	return out
}

func TestListTools(t *testing.T) {
	cs := newTestSession(t)
	var names []string
	for tool, err := range cs.Tools(context.Background(), nil) {
		require.NoError(t, err)
		names = append(names, tool.Name)
		assert.NotEmpty(t, tool.Description, "tool %s has no description", tool.Name)
		assert.NotNil(t, tool.InputSchema, "tool %s has no input schema", tool.Name)
	}
	assert.Len(t, names, 15, "tools: %v", names)
}

func TestKnowledgeBaseLifecycle(t *testing.T) {
	cs := newTestSession(t)

	call(t, cs, "prolog_create_unit", map[string]any{
		"name": "family", "description": "family relations", "load": true,
	}, nil)
	call(t, cs, "prolog_assert", map[string]any{
		"unit": "family",
		"clauses": `parent(tom, bob).
parent(bob, ann).
parent(bob, pat).
ancestor(X, Y) :- parent(X, Y).
ancestor(X, Y) :- parent(X, Z), ancestor(Z, Y).`,
	}, nil)

	var q queryOut
	call(t, cs, "prolog_query", map[string]any{"goal": "ancestor(tom, Who)"}, &q)
	assert.True(t, q.Succeeded)
	assert.Equal(t, 3, q.Count)
	assert.ElementsMatch(t, []string{"bob", "ann", "pat"}, bind(q.Solutions, "Who"))

	var p proveOut
	call(t, cs, "prolog_prove", map[string]any{"goal": "ancestor(tom, ann)"}, &p)
	assert.True(t, p.Proved, "ancestor(tom, ann) should be provable")
	call(t, cs, "prolog_prove", map[string]any{"goal": "ancestor(ann, tom)"}, &p)
	assert.False(t, p.Proved, "ancestor(ann, tom) should not be provable")

	var e explainOut
	call(t, cs, "prolog_explain", map[string]any{"goal": "ancestor(tom, ann)"}, &e)
	assert.True(t, e.Proved)
	assert.Contains(t, e.Proof, "parent(tom,bob)", "proof does not mention the parent step")
}

func TestScopeControlsVisibility(t *testing.T) {
	cs := newTestSession(t)

	call(t, cs, "prolog_create_unit", map[string]any{"name": "facts", "load": true}, nil)
	call(t, cs, "prolog_assert", map[string]any{"unit": "facts", "clauses": "bird(tweety)."}, nil)
	call(t, cs, "prolog_create_unit", map[string]any{"name": "defaults"}, nil)
	call(t, cs, "prolog_assert", map[string]any{"unit": "defaults", "clauses": "flies(X) :- bird(X)."}, nil)

	// flies/1 is defined in a unit that is not in scope, so the goal must not
	// merely fail: it must be an error about an unknown procedure.
	msg := callErr(t, cs, "prolog_query", map[string]any{"goal": "flies(tweety)"})
	assert.Contains(t, msg, "flies", "expected an unknown-procedure error")

	call(t, cs, "prolog_load_unit", map[string]any{"unit": "defaults"}, nil)
	var p proveOut
	call(t, cs, "prolog_prove", map[string]any{"goal": "flies(tweety)"}, &p)
	assert.True(t, p.Proved, "flies(tweety) should be provable once defaults is in scope")

	// Narrowing the scope withdraws the conclusion again.
	call(t, cs, "prolog_unload_unit", map[string]any{"unit": "defaults"}, nil)
	callErr(t, cs, "prolog_query", map[string]any{"goal": "flies(tweety)"})

	var sc scopeOut
	call(t, cs, "prolog_set_scope", map[string]any{"units": []string{"defaults", "facts"}}, &sc)
	assert.Equal(t, []string{"defaults", "facts"}, sc.Scope, "scope not set in the requested order")
}

func TestRetractAndFind(t *testing.T) {
	cs := newTestSession(t)
	call(t, cs, "prolog_create_unit", map[string]any{"name": "facts", "load": true}, nil)
	call(t, cs, "prolog_assert", map[string]any{
		"unit": "facts",
		"clauses": `parent(tom, bob).
parent(tom, liz).
parent(bob, ann).`,
	}, nil)

	var f findOut
	call(t, cs, "prolog_find_clauses", map[string]any{"pattern": "parent(tom, _)"}, &f)
	assert.Equal(t, 2, f.Total, "matches: %+v", f.Results)

	var r retractOut
	call(t, cs, "prolog_retract", map[string]any{
		"unit": "facts", "pattern": "parent(tom, _)", "all": true,
	}, &r)
	assert.Len(t, r.Removed, 2)
	assert.Equal(t, 1, r.Remaining)

	var q queryOut
	call(t, cs, "prolog_query", map[string]any{"goal": "parent(P, C)"}, &q)
	assert.Equal(t, []string{"bob"}, bind(q.Solutions, "P"), "only parent(bob, ann) should remain")

	// An indicator selects by name and arity.
	call(t, cs, "prolog_retract", map[string]any{"unit": "facts", "pattern": "parent/2", "all": true}, &r)
	assert.Zero(t, r.Remaining, "parent/2 should have removed everything")
}

func TestSyntaxErrorsDoNotMutate(t *testing.T) {
	cs := newTestSession(t)
	call(t, cs, "prolog_create_unit", map[string]any{"name": "u", "load": true}, nil)
	call(t, cs, "prolog_assert", map[string]any{"unit": "u", "clauses": "good(1)."}, nil)

	// Unterminated clause: rejected by the Go lexer before Prolog is involved.
	callErr(t, cs, "prolog_assert", map[string]any{"unit": "u", "clauses": "bad(1)"})
	// Syntactically terminated but unparseable: rejected by SWI-Prolog.
	callErr(t, cs, "prolog_assert", map[string]any{"unit": "u", "clauses": "bad( ,)."})

	var s showUnitOut
	call(t, cs, "prolog_show_unit", map[string]any{"unit": "u"}, &s)
	assert.Len(t, s.Clauses, 1, "failed asserts must not change the unit")
}

func TestSandboxRejectsUnsafeGoals(t *testing.T) {
	cs := newTestSession(t)
	call(t, cs, "prolog_create_unit", map[string]any{"name": "u", "load": true}, nil)
	call(t, cs, "prolog_assert", map[string]any{"unit": "u", "clauses": "safe(1)."}, nil)

	msg := callErr(t, cs, "prolog_query", map[string]any{"goal": `shell('id')`})
	assert.Contains(t, msg, "sandbox", "expected a sandbox rejection")

	// Directives run at load time, before the goal sandbox can see them, so they
	// are filtered separately.
	msg = callErr(t, cs, "prolog_assert", map[string]any{"unit": "u", "clauses": `:- shell('id').`})
	assert.Contains(t, msg, "not permitted", "expected the directive to be refused")

	// Harmless declarations are still allowed.
	call(t, cs, "prolog_assert", map[string]any{"unit": "u", "clauses": ":- dynamic counter/1."}, nil)
}

func TestQueryLimitAndTimeout(t *testing.T) {
	cs := newTestSession(t)
	call(t, cs, "prolog_create_unit", map[string]any{"name": "u", "load": true}, nil)
	call(t, cs, "prolog_assert", map[string]any{
		"unit": "u", "clauses": "nat(0).\nnat(N) :- nat(M), N is M + 1.\nfinite(a).\nfinite(b).",
	}, nil)

	// "truncated" means "there may be more", so it is set exactly when the
	// solutions ran up against the limit.
	for _, tc := range []struct {
		goal      string
		limit     int
		count     int
		truncated bool
	}{
		{"nat(N)", 5, 5, true},
		{"finite(X)", 1, 1, true},
		{"finite(X)", 2, 2, false},
		{"finite(X)", 3, 2, false},
	} {
		t.Run(fmt.Sprintf("%s limit %d", tc.goal, tc.limit), func(t *testing.T) {
			var q queryOut
			call(t, cs, "prolog_query", map[string]any{"goal": tc.goal, "limit": tc.limit}, &q)
			assert.Equal(t, tc.count, q.Count)
			assert.Equal(t, tc.truncated, q.Truncated)
		})
	}

	// A goal that cannot succeed and never terminates must be stopped by the
	// in-Prolog time limit rather than hanging the server.
	msg := callErr(t, cs, "prolog_query", map[string]any{"goal": "nat(N), N < 0", "timeoutMs": 800})
	assert.Contains(t, msg, "time_limit_exceeded")
}

// TestUndefinedPredicateInRuleBodyIsFalse covers the closed-world behaviour: a
// rule may refer to a predicate defined in a unit that is not currently in
// scope, and that reference should simply fail rather than raise an error. A
// predicate the caller names directly must still be reported, so typos surface.
func TestUndefinedPredicateInRuleBodyIsFalse(t *testing.T) {
	cs := newTestSession(t)
	call(t, cs, "prolog_create_unit", map[string]any{"name": "policy", "load": true}, nil)
	call(t, cs, "prolog_assert", map[string]any{
		"unit": "policy",
		"clauses": `person(ana).
allowed(P) :- person(P), \+ banned(P).`,
	}, nil)

	// banned/1 is not defined anywhere, but it is only reached through a rule.
	var q queryOut
	call(t, cs, "prolog_query", map[string]any{"goal": "allowed(P)"}, &q)
	assert.Equal(t, []string{"ana"}, bind(q.Solutions, "P"))

	// Naming an undefined predicate directly is still an error.
	msg := callErr(t, cs, "prolog_query", map[string]any{"goal": "bnned(X)"})
	assert.Contains(t, msg, "bnned", "expected the typo to be reported")

	// Defining it in another unit changes the answer once that unit is loaded.
	call(t, cs, "prolog_create_unit", map[string]any{"name": "sanctions"}, nil)
	call(t, cs, "prolog_assert", map[string]any{"unit": "sanctions", "clauses": "banned(ana)."}, nil)
	call(t, cs, "prolog_load_unit", map[string]any{"unit": "sanctions"}, nil)
	call(t, cs, "prolog_query", map[string]any{"goal": "allowed(P)"}, &q)
	assert.Empty(t, q.Solutions, "with sanctions loaded, nobody is allowed")
}

func TestExplainDepthLimitPreservesTruth(t *testing.T) {
	cs := newTestSession(t)
	call(t, cs, "prolog_create_unit", map[string]any{"name": "u", "load": true}, nil)
	call(t, cs, "prolog_assert", map[string]any{
		"unit": "u", "clauses": `fact.
verified :- fact.
claimed :- missing.
cut_false :- !, fail.
cut_false.
if_false :- (true -> fail ; true).`,
	}, nil)

	var e explainOut
	call(t, cs, "prolog_explain", map[string]any{"goal": "verified", "depth": 1}, &e)
	assert.True(t, e.Proved, "a true goal stays proved when its subtree is elided")
	assert.Contains(t, e.Proof, "depth limit reached")

	for _, args := range []map[string]any{
		{"goal": "claimed", "depth": 1}, // a depth cutoff must not prove a false goal
		{"goal": "cut_false"},           // native cut semantics
		{"goal": "if_false"},            // native if-then-else semantics
	} {
		call(t, cs, "prolog_explain", args, &e)
		assert.False(t, e.Proved, "%s is false but was proved:\n%s", args["goal"], e.Proof)
	}
}

func TestExplainHandlesImportedLibraryPredicates(t *testing.T) {
	cs := newTestSession(t)
	call(t, cs, "prolog_create_unit", map[string]any{"name": "facts"}, nil)
	call(t, cs, "prolog_create_unit", map[string]any{"name": "rules"}, nil)
	call(t, cs, "prolog_assert", map[string]any{
		"unit":    "facts",
		"clauses": "link(hq, a, 2).\nlink(a, backup, 3).",
	}, nil)
	call(t, cs, "prolog_assert", map[string]any{
		"unit": "rules",
		"clauses": `route(From, To, Path, Cost) :- route(From, To, [From], RevPath, 0, Cost), reverse(RevPath, Path).
route(To, To, Visited, Visited, Cost, Cost).
route(From, To, Visited, Path, Acc, Cost) :- link(From, Next, Step), \+ member(Next, Visited), Acc1 is Acc + Step, route(Next, To, [Next|Visited], Path, Acc1, Cost).`,
	}, nil)
	call(t, cs, "prolog_set_scope", map[string]any{"units": []string{"facts", "rules"}}, nil)

	var e explainOut
	call(t, cs, "prolog_explain", map[string]any{
		"goal": "route(hq, backup, [hq,a,backup], 5)", "depth": 16, "timeoutMs": 2000,
	}, &e)
	assert.True(t, e.Proved, "proof: %s", e.Proof)
	assert.Contains(t, e.Proof, "library(lists)", "imported library predicates should be labelled")
}

// TestConcurrentQueriesLeaveTheKnowledgeBaseIntact exercises the path a client
// takes when it issues a batch of calls: the SDK runs them concurrently, and
// the serialising middleware has to keep them from tripping over each other.
func TestConcurrentQueriesLeaveTheKnowledgeBaseIntact(t *testing.T) {
	cs := newTestSession(t)
	call(t, cs, "prolog_create_unit", map[string]any{"name": "u", "load": true}, nil)
	call(t, cs, "prolog_assert", map[string]any{"unit": "u", "clauses": "n(1).\nn(2).\nn(3)."}, nil)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      "prolog_query",
				Arguments: map[string]any{"goal": "n(X)"},
			})
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	var q queryOut
	call(t, cs, "prolog_query", map[string]any{"goal": "n(X)"}, &q)
	assert.Equal(t, []string{"1", "2", "3"}, bind(q.Solutions, "X"))
}

// TestConcurrentRetractsRemoveTheRightClauses is the case the serialising
// middleware exists for. prolog_retract is a read-modify-write: it asks
// SWI-Prolog which clause positions match, then removes those positions. The
// positions are only valid while the unit is unchanged, so two retracts that
// interleave can delete the wrong clauses.
func TestConcurrentRetractsRemoveTheRightClauses(t *testing.T) {
	cs := newTestSession(t)
	call(t, cs, "prolog_create_unit", map[string]any{"name": "u", "load": true}, nil)

	const n = 10
	var src strings.Builder
	for i := range n {
		fmt.Fprintf(&src, "fact(%d).\n", i)
	}
	call(t, cs, "prolog_assert", map[string]any{"unit": "u", "clauses": src.String()}, nil)

	// Concurrently retract the even-numbered facts.
	var wg sync.WaitGroup
	for i := 0; i < n; i += 2 {
		wg.Go(func() {
			_, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "prolog_retract",
				Arguments: map[string]any{
					"unit": "u", "pattern": fmt.Sprintf("fact(%d)", i), "all": true,
				},
			})
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	var q queryOut
	call(t, cs, "prolog_query", map[string]any{"goal": "fact(X)", "limit": 50}, &q)
	assert.ElementsMatch(t, []string{"1", "3", "5", "7", "9"}, bind(q.Solutions, "X"),
		"exactly the odd-numbered facts should have survived")
}
