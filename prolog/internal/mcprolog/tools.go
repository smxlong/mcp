package mcprolog

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// service holds everything the tool handlers need.
type service struct {
	kb              *KB
	runner          *Runner
	maxSolutions    int
	allowDirectives bool
}

// Annotations describing a tool's effect, so that a client can decide what to
// confirm with the user. No tool touches anything outside the knowledge base,
// hence OpenWorldHint is false throughout.
var (
	readOnly    = &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: ptr(false)}
	mutating    = &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(false)}
	destructive = &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(false)}
)

// register adds every tool to the server.
//
// Tools are registered through the generic mcp.AddTool, which derives the input
// and output JSON schemas from the In and Out type parameters, validates
// arguments before the handler runs, and turns a returned error into a tool
// error (IsError) rather than a protocol error. That last point matters: a tool
// error is visible to the model and can be corrected, whereas a protocol error
// is not.
func (s *service) register(server *mcp.Server) {
	addTool(server, readOnly, "prolog_list_units", "List units", s.listUnits,
		"List every unit in the knowledge base with its clause count, and report which units are currently in query scope. Start here to orient yourself.")

	addTool(server, mutating, "prolog_create_unit", "Create a unit", s.createUnit,
		"Create a new, empty unit. A unit is a named group of related clauses (facts and rules). Group a domain's predicates into one unit so it can be brought in and out of query scope as a whole.")

	addTool(server, destructive, "prolog_delete_unit", "Delete a unit", s.deleteUnit,
		"Permanently delete a unit and all of its clauses, and remove it from scope. This cannot be undone.")

	addTool(server, readOnly, "prolog_show_unit", "Show a unit", s.showUnit,
		"Show a unit's full Prolog source together with the 1-based position of each clause. Use the positions with prolog_retract.")

	addTool(server, destructive, "prolog_set_unit_source", "Set unit source", s.setUnitSource,
		"Replace a unit's entire source with new Prolog text. Use this to rewrite a unit wholesale; use prolog_assert to add individual clauses. The new source is syntax-checked before it is committed.")

	addTool(server, mutating, "prolog_assert", "Assert clauses", s.assert,
		"Add one or more clauses to a unit. Accepts any number of facts and rules in ordinary Prolog syntax, each terminated by '.'. The result is syntax-checked before it is committed, so a malformed clause changes nothing.")

	addTool(server, destructive, "prolog_retract", "Retract clauses", s.retract,
		"Remove clauses from a unit whose head unifies with a pattern. The pattern may be a term such as parent(tom, _) or an indicator such as parent/2. By default only the first match is removed; set all=true to remove every match.")

	addTool(server, readOnly, "prolog_find_clauses", "Find clauses", s.findClauses,
		"Search the knowledge base for clauses whose head unifies with a pattern, such as ancestor(_, _) or ancestor/2. Searches every unit unless specific units are named. Use this to locate a definition before changing it.")

	addTool(server, readOnly, "prolog_check", "Check for errors", s.check,
		"Load a unit (or the whole current scope) and report syntax errors, singleton-variable warnings, and the predicates it defines. Run this after a substantial edit and before relying on a query.")

	addTool(server, mutating, "prolog_load_unit", "Load a unit into scope", s.loadUnit,
		"Bring a unit into query scope. Only clauses from in-scope units are visible to prolog_query, prolog_prove and prolog_explain.")

	addTool(server, mutating, "prolog_unload_unit", "Remove a unit from scope", s.unloadUnit,
		"Take a unit out of query scope without deleting it. Use this to reason under a narrower set of assumptions, or to resolve a conflict between two units.")

	addTool(server, mutating, "prolog_set_scope", "Set query scope", s.setScope,
		"Replace the entire query scope with an explicit, ordered list of units. Use this to set up a specific reasoning context in one step, for example to compare conclusions under two different sets of assumptions.")

	addTool(server, readOnly, "prolog_query", "Query", s.query,
		"Run a Prolog goal against the units currently in scope and return the bindings of every named variable, for up to 'limit' solutions. Variables whose name begins with '_' are not reported. This is the main reasoning tool.")

	addTool(server, readOnly, "prolog_prove", "Prove a goal", s.prove,
		"Ask whether a goal is provable from the units in scope. Returns a yes/no answer plus the first solution's bindings. Cheaper than prolog_query when you only need to know whether something holds.")

	addTool(server, readOnly, "prolog_explain", "Explain a proof", s.explain,
		"Prove a goal and return the proof tree showing which clauses were used, so you can justify the conclusion or diagnose an unexpected one.")
}

func addTool[In, Out any](server *mcp.Server, ann *mcp.ToolAnnotations, name, title string, h mcp.ToolHandlerFor[In, Out], description string) {
	mcp.AddTool(server, &mcp.Tool{Name: name, Title: title, Description: description, Annotations: ann}, h)
}

func ptr[T any](v T) *T { return &v }

// ---------------------------------------------------------------------------
// Unit management
// ---------------------------------------------------------------------------

type listUnitsIn struct{}

type listUnitsOut struct {
	Units []UnitSummary `json:"units" jsonschema:"every unit in the knowledge base"`
	Scope []string      `json:"scope" jsonschema:"units currently in query scope, in load order"`
}

func (s *service) listUnits(context.Context, *mcp.CallToolRequest, listUnitsIn) (*mcp.CallToolResult, listUnitsOut, error) {
	units, scope := s.kb.ListUnits()
	out := listUnitsOut{Units: units, Scope: scope}
	if len(units) == 0 {
		return text("The knowledge base is empty. Create a unit with prolog_create_unit."), out, nil
	}
	var b strings.Builder
	for _, u := range units {
		mark := " "
		if u.InScope {
			mark = "*"
		}
		fmt.Fprintf(&b, "%s %-24s %3d clauses  %s\n", mark, u.Name, u.Clauses, u.Description)
	}
	fmt.Fprintf(&b, "\n(* = in scope; scope order: %s)", strings.Join(scope, ", "))
	return text(b.String()), out, nil
}

type createUnitIn struct {
	Name        string `json:"name" jsonschema:"unit name; letters, digits, '_' and '-' only"`
	Description string `json:"description,omitempty" jsonschema:"what this unit is for, for your own later reference"`
	Load        bool   `json:"load,omitempty" jsonschema:"also bring the new unit into query scope"`
}

type createUnitOut struct {
	Unit  string   `json:"unit" jsonschema:"the created unit's name"`
	Scope []string `json:"scope" jsonschema:"units currently in query scope"`
}

func (s *service) createUnit(_ context.Context, _ *mcp.CallToolRequest, in createUnitIn) (*mcp.CallToolResult, createUnitOut, error) {
	scope, err := s.kb.CreateUnit(in.Name, in.Description)
	if err != nil {
		return nil, createUnitOut{}, err
	}
	if in.Load {
		if scope, err = s.kb.LoadUnit(in.Name); err != nil {
			return nil, createUnitOut{}, err
		}
	}
	return text(fmt.Sprintf("Created unit %q.", in.Name)), createUnitOut{Unit: in.Name, Scope: scope}, nil
}

type unitNameIn struct {
	Unit string `json:"unit" jsonschema:"name of the unit"`
}

type scopeOut struct {
	Scope []string `json:"scope" jsonschema:"units currently in query scope, in load order"`
}

func (s *service) deleteUnit(_ context.Context, _ *mcp.CallToolRequest, in unitNameIn) (*mcp.CallToolResult, scopeOut, error) {
	scope, err := s.kb.DeleteUnit(in.Unit)
	if err != nil {
		return nil, scopeOut{}, err
	}
	return text(fmt.Sprintf("Deleted unit %q.", in.Unit)), scopeOut{Scope: scope}, nil
}

type showUnitOut struct {
	Unit        string   `json:"unit" jsonschema:"unit name"`
	Description string   `json:"description,omitempty" jsonschema:"the unit's description"`
	Source      string   `json:"source" jsonschema:"the unit's full Prolog source"`
	Clauses     []Clause `json:"clauses" jsonschema:"the unit's clauses in order; the 1-based position of each is its index in this list"`
	InScope     bool     `json:"inScope" jsonschema:"whether the unit is in query scope"`
}

func (s *service) showUnit(_ context.Context, _ *mcp.CallToolRequest, in unitNameIn) (*mcp.CallToolResult, showUnitOut, error) {
	u, err := s.kb.Unit(in.Unit)
	if err != nil {
		return nil, showUnitOut{}, err
	}
	var b strings.Builder
	for _, c := range u.Clauses {
		fmt.Fprintf(&b, "%3d  %s\n", c.ID, c.Text)
	}
	if len(u.Clauses) == 0 {
		b.WriteString("(unit is empty)")
	}
	return text(b.String()), showUnitOut{
		Unit: in.Unit, Description: u.Description, Source: u.Source,
		Clauses: u.Clauses, InScope: u.InScope,
	}, nil
}

type setSourceIn struct {
	Unit   string `json:"unit" jsonschema:"name of the unit to rewrite"`
	Source string `json:"source" jsonschema:"the complete new Prolog source for the unit; every clause must end with '.'"`
}

type editOut struct {
	Unit        string       `json:"unit" jsonschema:"the unit that was modified"`
	Clauses     []Clause     `json:"clauses" jsonschema:"the clauses that were written"`
	Diagnostics []Diagnostic `json:"diagnostics" jsonschema:"warnings reported while checking the result; errors would have aborted the edit"`
}

func (s *service) setUnitSource(ctx context.Context, _ *mcp.CallToolRequest, in setSourceIn) (*mcp.CallToolResult, editOut, error) {
	diags, err := s.validateSource(ctx, in.Source)
	if err != nil {
		return nil, editOut{}, err
	}
	clauses, err := s.kb.ReplaceSource(in.Unit, in.Source)
	if err != nil {
		return nil, editOut{}, err
	}
	return text(fmt.Sprintf("Unit %q now has %d clause(s).%s", in.Unit, len(clauses), diagSummary(diags))),
		editOut{Unit: in.Unit, Clauses: clauses, Diagnostics: diags}, nil
}

type assertIn struct {
	Unit    string `json:"unit" jsonschema:"unit to add the clauses to"`
	Clauses string `json:"clauses" jsonschema:"one or more Prolog clauses, each terminated by '.'; for example: parent(tom, bob). ancestor(X, Y) :- parent(X, Y)."`
}

func (s *service) assert(ctx context.Context, _ *mcp.CallToolRequest, in assertIn) (*mcp.CallToolResult, editOut, error) {
	u, err := s.kb.Unit(in.Unit)
	if err != nil {
		return nil, editOut{}, err
	}
	// Check the clauses in the context of the unit they are joining, so that a
	// duplicate or discontiguous definition is reported now rather than later.
	diags, err := s.validateSource(ctx, u.Source+"\n"+in.Clauses)
	if err != nil {
		return nil, editOut{}, err
	}
	added, err := s.kb.AddClauses(in.Unit, in.Clauses)
	if err != nil {
		return nil, editOut{}, err
	}
	return text(fmt.Sprintf("Added %d clause(s) to %q.%s", len(added), in.Unit, diagSummary(diags))),
		editOut{Unit: in.Unit, Clauses: added, Diagnostics: diags}, nil
}

type retractIn struct {
	Unit    string `json:"unit" jsonschema:"unit to remove clauses from"`
	Pattern string `json:"pattern" jsonschema:"a term whose unification with a clause head selects it, such as parent(tom, _), or an indicator such as parent/2"`
	All     bool   `json:"all,omitempty" jsonschema:"remove every matching clause instead of only the first"`
}

type retractOut struct {
	Unit      string   `json:"unit" jsonschema:"the unit that was modified"`
	Removed   []Clause `json:"removed" jsonschema:"the clauses that were removed"`
	Remaining int      `json:"remaining" jsonschema:"how many clauses the unit still has"`
}

func (s *service) retract(ctx context.Context, _ *mcp.CallToolRequest, in retractIn) (*mcp.CallToolResult, retractOut, error) {
	matches, err := s.matchUnit(ctx, in.Unit, in.Pattern)
	if err != nil {
		return nil, retractOut{}, err
	}
	if len(matches) == 0 {
		return nil, retractOut{}, fmt.Errorf("no clause in unit %q has a head unifying with %s", in.Unit, in.Pattern)
	}
	if !in.All {
		matches = matches[:1]
	}
	positions := make([]int, 0, len(matches))
	for _, m := range matches {
		positions = append(positions, m.Index)
	}
	removed, err := s.kb.RemoveAt(in.Unit, positions)
	if err != nil {
		return nil, retractOut{}, err
	}
	u, err := s.kb.Unit(in.Unit)
	if err != nil {
		return nil, retractOut{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Removed %d clause(s) from %q:\n", len(removed), in.Unit)
	for _, c := range removed {
		fmt.Fprintf(&b, "  %s\n", c.Text)
	}
	return text(b.String()), retractOut{Unit: in.Unit, Removed: removed, Remaining: len(u.Clauses)}, nil
}

type findIn struct {
	Pattern string   `json:"pattern" jsonschema:"a term to unify against clause heads, such as ancestor(_, _), or an indicator such as ancestor/2"`
	Units   []string `json:"units,omitempty" jsonschema:"restrict the search to these units; omit to search every unit"`
}

type unitMatches struct {
	Unit    string  `json:"unit" jsonschema:"the unit the matches were found in"`
	Matches []Match `json:"matches" jsonschema:"matching clauses and their 1-based positions within the unit"`
}

type findOut struct {
	Results []unitMatches `json:"results" jsonschema:"one entry per unit that contained at least one match"`
	Total   int           `json:"total" jsonschema:"total number of matching clauses"`
}

func (s *service) findClauses(ctx context.Context, _ *mcp.CallToolRequest, in findIn) (*mcp.CallToolResult, findOut, error) {
	units := in.Units
	if len(units) == 0 {
		units = s.kb.UnitNames()
	}
	var out findOut
	var b strings.Builder
	for _, u := range units {
		matches, err := s.matchUnit(ctx, u, in.Pattern)
		if err != nil {
			return nil, findOut{}, err
		}
		if len(matches) == 0 {
			continue
		}
		out.Results = append(out.Results, unitMatches{Unit: u, Matches: matches})
		out.Total += len(matches)
		fmt.Fprintf(&b, "%s:\n", u)
		for _, m := range matches {
			fmt.Fprintf(&b, "  %3d  %s\n", m.Index, m.Text)
		}
	}
	if out.Total == 0 {
		fmt.Fprintf(&b, "No clause head unifies with %s.", in.Pattern)
	}
	return text(b.String()), out, nil
}

// ---------------------------------------------------------------------------
// Scope management
// ---------------------------------------------------------------------------

func (s *service) loadUnit(_ context.Context, _ *mcp.CallToolRequest, in unitNameIn) (*mcp.CallToolResult, scopeOut, error) {
	return renderScope(s.kb.LoadUnit(in.Unit))
}

func (s *service) unloadUnit(_ context.Context, _ *mcp.CallToolRequest, in unitNameIn) (*mcp.CallToolResult, scopeOut, error) {
	return renderScope(s.kb.UnloadUnit(in.Unit))
}

type setScopeIn struct {
	Units []string `json:"units" jsonschema:"the complete, ordered list of units that should be in scope; pass an empty list to clear the scope"`
}

func (s *service) setScope(_ context.Context, _ *mcp.CallToolRequest, in setScopeIn) (*mcp.CallToolResult, scopeOut, error) {
	return renderScope(s.kb.SetScope(in.Units))
}

func renderScope(scope []string, err error) (*mcp.CallToolResult, scopeOut, error) {
	switch {
	case err != nil:
		return nil, scopeOut{}, err
	case len(scope) == 0:
		return text("Scope is now empty."), scopeOut{Scope: scope}, nil
	default:
		return text("Scope: " + strings.Join(scope, ", ")), scopeOut{Scope: scope}, nil
	}
}

// ---------------------------------------------------------------------------
// Reasoning
// ---------------------------------------------------------------------------

type checkIn struct {
	Unit string `json:"unit,omitempty" jsonschema:"check this unit alone; omit to check everything currently in scope"`
}

type checkOut struct {
	OK          bool         `json:"ok" jsonschema:"whether the program loaded without errors"`
	Predicates  []Predicate  `json:"predicates" jsonschema:"predicates the program defines"`
	Diagnostics []Diagnostic `json:"diagnostics" jsonschema:"errors and warnings from the load"`
}

func (s *service) check(ctx context.Context, _ *mcp.CallToolRequest, in checkIn) (*mcp.CallToolResult, checkOut, error) {
	var program string
	if in.Unit == "" {
		program, _ = s.kb.Program()
	} else {
		u, err := s.kb.Unit(in.Unit)
		if err != nil {
			return nil, checkOut{}, err
		}
		program = preamble + u.Source
	}

	res, err := s.runner.Run(ctx, request{mode: modeCheck, program: program})
	if err != nil {
		return nil, checkOut{}, err
	}
	ok := !hasErrors(res.Diagnostics)
	var b strings.Builder
	if ok {
		fmt.Fprintf(&b, "OK: %d predicate(s) defined.\n", len(res.Predicates))
	} else {
		b.WriteString("Errors found.\n")
	}
	for _, p := range res.Predicates {
		fmt.Fprintf(&b, "  %s/%d\n", p.Name, p.Arity)
	}
	for _, d := range res.Diagnostics {
		fmt.Fprintf(&b, "  [%s] %s\n", d.Kind, strings.TrimSpace(d.Message))
	}
	return text(b.String()), checkOut{OK: ok, Predicates: res.Predicates, Diagnostics: res.Diagnostics}, nil
}

type queryIn struct {
	Goal      string `json:"goal" jsonschema:"a Prolog goal, without the trailing '.'; for example: ancestor(tom, Who)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum number of solutions to return (default 25)"`
	TimeoutMs int    `json:"timeoutMs,omitempty" jsonschema:"give up after this many milliseconds"`
}

type queryOut struct {
	Succeeded   bool                `json:"succeeded" jsonschema:"whether the goal had at least one solution"`
	Count       int                 `json:"count" jsonschema:"number of solutions returned"`
	Truncated   bool                `json:"truncated" jsonschema:"true if there may be more solutions beyond the limit"`
	Solutions   []map[string]string `json:"solutions" jsonschema:"one object per solution, mapping variable name to the term it was bound to"`
	Scope       []string            `json:"scope" jsonschema:"the units the goal was run against"`
	Output      string              `json:"output,omitempty" jsonschema:"anything the goal printed"`
	Diagnostics []Diagnostic        `json:"diagnostics" jsonschema:"errors and warnings raised while loading or running"`
}

func (s *service) query(ctx context.Context, _ *mcp.CallToolRequest, in queryIn) (*mcp.CallToolResult, queryOut, error) {
	limit := in.Limit
	if limit <= 0 || limit > s.maxSolutions {
		limit = min(s.maxSolutions, defaultLimit)
	}
	res, scope, err := s.runGoal(ctx, request{
		mode: modeQuery, goal: in.Goal, limit: limit, timeout: millis(in.TimeoutMs),
	})
	if err != nil {
		return nil, queryOut{}, err
	}
	return text(renderSolutions(res, scope)), queryOut{
		Succeeded: res.Succeeded, Count: res.Count, Truncated: res.Truncated,
		Solutions: res.Solutions, Scope: scope, Output: res.Output,
		Diagnostics: res.Diagnostics,
	}, nil
}

type proveIn struct {
	Goal      string `json:"goal" jsonschema:"a Prolog goal, without the trailing '.'"`
	TimeoutMs int    `json:"timeoutMs,omitempty" jsonschema:"give up after this many milliseconds"`
}

type proveOut struct {
	Proved      bool              `json:"proved" jsonschema:"whether the goal is provable from the units in scope"`
	Bindings    map[string]string `json:"bindings" jsonschema:"variable bindings of the first solution"`
	Scope       []string          `json:"scope" jsonschema:"the units the goal was run against"`
	Output      string            `json:"output,omitempty" jsonschema:"anything the goal printed"`
	Diagnostics []Diagnostic      `json:"diagnostics" jsonschema:"errors and warnings raised while loading or running"`
}

func (s *service) prove(ctx context.Context, _ *mcp.CallToolRequest, in proveIn) (*mcp.CallToolResult, proveOut, error) {
	res, scope, err := s.runGoal(ctx, request{mode: modeProve, goal: in.Goal, timeout: millis(in.TimeoutMs)})
	if err != nil {
		return nil, proveOut{}, err
	}
	msg := "no"
	if res.Proved {
		msg = "yes"
		if len(res.Bindings) > 0 {
			msg += " — " + renderBindings(res.Bindings)
		}
	}
	return text(msg), proveOut{
		Proved: res.Proved, Bindings: res.Bindings, Scope: scope,
		Output: res.Output, Diagnostics: res.Diagnostics,
	}, nil
}

type explainIn struct {
	Goal      string `json:"goal" jsonschema:"a Prolog goal, without the trailing '.'"`
	Depth     int    `json:"depth,omitempty" jsonschema:"maximum proof depth to explore (default 12)"`
	TimeoutMs int    `json:"timeoutMs,omitempty" jsonschema:"give up after this many milliseconds"`
}

type explainOut struct {
	Proved      bool         `json:"proved" jsonschema:"whether a proof was found"`
	Proof       string       `json:"proof" jsonschema:"the proof tree, indented; each line is a goal and its children are the goals used to prove it"`
	Scope       []string     `json:"scope" jsonschema:"the units the goal was run against"`
	Output      string       `json:"output,omitempty" jsonschema:"anything the goal printed"`
	Diagnostics []Diagnostic `json:"diagnostics" jsonschema:"errors and warnings raised while loading or running"`
}

func (s *service) explain(ctx context.Context, _ *mcp.CallToolRequest, in explainIn) (*mcp.CallToolResult, explainOut, error) {
	res, scope, err := s.runGoal(ctx, request{
		mode: modeExplain, goal: in.Goal, depth: in.Depth, timeout: millis(in.TimeoutMs),
	})
	if err != nil {
		return nil, explainOut{}, err
	}
	msg := res.Proof
	if !res.Proved {
		msg = "The goal is not provable from the units in scope."
	}
	return text(msg), explainOut{
		Proved: res.Proved, Proof: res.Proof, Scope: scope,
		Output: res.Output, Diagnostics: res.Diagnostics,
	}, nil
}

// runGoal runs a goal against the units in scope. It is shared by prolog_query,
// prolog_prove and prolog_explain, which differ only in the mode they ask for
// and the fields they read back.
func (s *service) runGoal(ctx context.Context, req request) (*Result, []string, error) {
	if strings.TrimSpace(req.goal) == "" {
		return nil, nil, errors.New("goal must not be empty")
	}
	program, scope := s.kb.Program()
	req.program = program
	res, err := s.runner.Run(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !res.OK {
		return nil, nil, fmt.Errorf("%s%s", res.Error, diagDetail(res.Diagnostics))
	}
	return res, scope, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// directiveRE extracts the principal functor of a ':- Directive.' clause.
var directiveRE = regexp.MustCompile(`^:-\s*([a-z][a-zA-Z0-9_]*)`)

// safeDirectives are the only directives a unit may contain when the sandbox is
// enabled. Directives run at load time, before any goal is checked by
// library(sandbox), so an unrestricted ':- shell(...).' would bypass the
// sandbox entirely.
var safeDirectives = map[string]bool{
	"dynamic": true, "discontiguous": true, "multifile": true,
	"table": true, "op": true, "set_prolog_flag": true, "style_check": true,
}

// validateSource rejects source that must not be committed, and returns the
// warnings raised by source that may be.
func (s *service) validateSource(ctx context.Context, source string) ([]Diagnostic, error) {
	clauses, err := SplitClauses(source)
	if err != nil {
		return nil, err
	}
	if !s.allowDirectives {
		for _, c := range clauses {
			body := stripComments(c)
			if !strings.HasPrefix(body, ":-") {
				continue
			}
			if m := directiveRE.FindStringSubmatch(body); m == nil || !safeDirectives[m[1]] {
				return nil, fmt.Errorf("directive not permitted: %s\nDirectives run at load time and are not covered by the goal sandbox. Permitted directives are: %s",
					truncate(body, 120), strings.Join(slices.Sorted(maps.Keys(safeDirectives)), ", "))
			}
		}
	}
	res, err := s.runner.Run(ctx, request{mode: modeCheck, program: preamble + source})
	if err != nil {
		return nil, err
	}
	if hasErrors(res.Diagnostics) {
		return nil, fmt.Errorf("the source did not load cleanly, so nothing was changed:%s", diagDetail(res.Diagnostics))
	}
	return res.Diagnostics, nil
}

// matchUnit reports the clauses of a unit whose head unifies with a pattern.
func (s *service) matchUnit(ctx context.Context, unit, pattern string) ([]Match, error) {
	u, err := s.kb.Unit(unit)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(u.Source) == "" {
		return nil, nil
	}
	res, err := s.runner.Run(ctx, request{mode: modeMatch, program: u.Source, goal: pattern})
	if err != nil {
		return nil, err
	}
	if !res.OK {
		return nil, fmt.Errorf("bad pattern %q: %s", pattern, res.Error)
	}
	// The Go lexer and SWI-Prolog's reader must agree on where clauses end, or
	// the returned positions would refer to different clauses than the ones the
	// knowledge base holds. Verifying the count catches any divergence.
	if res.Total != len(u.Clauses) {
		return nil, fmt.Errorf("internal error: unit %q splits into %d clauses here but %d in SWI-Prolog; refusing to act on ambiguous positions", unit, len(u.Clauses), res.Total)
	}
	return res.Matches, nil
}

func millis(ms int) time.Duration { return time.Duration(ms) * time.Millisecond }

func hasErrors(ds []Diagnostic) bool {
	return slices.ContainsFunc(ds, func(d Diagnostic) bool { return d.Kind == "error" })
}

func diagSummary(ds []Diagnostic) string {
	if len(ds) == 0 {
		return ""
	}
	return fmt.Sprintf(" %d warning(s); see diagnostics.", len(ds))
}

func diagDetail(ds []Diagnostic) string {
	var b strings.Builder
	for _, d := range ds {
		fmt.Fprintf(&b, "\n  [%s] %s", d.Kind, strings.TrimSpace(d.Message))
	}
	return b.String()
}

func renderBindings(b map[string]string) string {
	if len(b) == 0 {
		return "(no variables)"
	}
	parts := make([]string, 0, len(b))
	for _, k := range slices.Sorted(maps.Keys(b)) {
		parts = append(parts, k+" = "+b[k])
	}
	return strings.Join(parts, ", ")
}

func renderSolutions(res *Result, scope []string) string {
	if !res.Succeeded {
		return fmt.Sprintf("No solutions (scope: %s).", strings.Join(scope, ", "))
	}
	var b strings.Builder
	for i, sol := range res.Solutions {
		fmt.Fprintf(&b, "%3d. %s\n", i+1, renderBindings(sol))
	}
	if res.Truncated {
		fmt.Fprintf(&b, "\n(stopped at the limit of %d; there may be more)", res.Count)
	}
	if res.Output != "" {
		fmt.Fprintf(&b, "\noutput:\n%s", res.Output)
	}
	return b.String()
}

// text returns a CallToolResult carrying a human-readable rendering. Returning
// it alongside the typed output gives the model something compact to read while
// keeping the machine-readable structuredContent intact; if it were omitted the
// SDK would fill Content with the JSON encoding of the output value.
func text(s string) *mcp.CallToolResult {
	if strings.TrimSpace(s) == "" {
		s = "(no output)"
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}
