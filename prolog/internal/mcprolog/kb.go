package mcprolog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// preamble heads every program handed to SWI-Prolog.
//
// style_check(-discontiguous) is set because units are an organising device
// chosen by the agent: the same predicate may legitimately be defined across
// several units. Singleton warnings are deliberately left on, because they are
// almost always a real bug and are surfaced to the agent as diagnostics.
const preamble = ":- style_check(-discontiguous).\n\n"

// Clause is a single Prolog clause, stored as source text including its
// terminating '.'.
type Clause struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

// Unit is a named collection of clauses. Units are the organising principle the
// tools expose to agents: an agent groups related predicates into a unit and
// then brings units in and out of query scope.
type Unit struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Clauses     []Clause `json:"clauses"`
	NextID      int      `json:"nextId"`
}

// Source renders the unit as a Prolog source file.
func (u *Unit) Source() string {
	var b strings.Builder
	for _, c := range u.Clauses {
		b.WriteString(c.Text)
		b.WriteString("\n")
	}
	return b.String()
}

func (u *Unit) add(text string) Clause {
	c := Clause{ID: u.NextID, Text: text}
	u.NextID++
	u.Clauses = append(u.Clauses, c)
	return c
}

// UnitSummary is the listing view of a unit.
type UnitSummary struct {
	Name        string `json:"name" jsonschema:"unit name"`
	Description string `json:"description,omitempty" jsonschema:"what the unit is for"`
	Clauses     int    `json:"clauses" jsonschema:"number of clauses in the unit"`
	InScope     bool   `json:"inScope" jsonschema:"whether the unit is currently loaded into query scope"`
}

// KB is the persistent knowledge base: a set of units plus the ordered list of
// units currently in query scope.
type KB struct {
	mu    sync.Mutex
	path  string
	Units map[string]*Unit `json:"units"`
	Scope []string         `json:"scope"`
}

// NewKB loads the knowledge base held in path, or creates an empty one if the
// file does not exist. An empty path keeps the knowledge base in memory only.
func NewKB(path string) (*KB, error) {
	kb := &KB{path: path, Units: map[string]*Unit{}}
	if path == "" {
		return kb, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return kb, nil
		}
		return nil, fmt.Errorf("reading state %s: %w", path, err)
	}
	if err := json.Unmarshal(data, kb); err != nil {
		return nil, fmt.Errorf("parsing state %s: %w", path, err)
	}
	if kb.Units == nil {
		kb.Units = map[string]*Unit{}
	}
	return kb, nil
}

// save writes the state to disk. Callers must hold kb.mu.
func (kb *KB) save() error {
	if kb.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(kb.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(kb, "", "  ")
	if err != nil {
		return err
	}
	// Write-then-rename so a crash mid-write cannot truncate the knowledge base.
	tmp := kb.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, kb.path)
}

// mutate applies a change to the units or the scope under the lock, persists
// it, and reports the resulting scope.
func (kb *KB) mutate(f func() error) ([]string, error) {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	if err := f(); err != nil {
		return nil, err
	}
	if err := kb.save(); err != nil {
		return nil, err
	}
	return slices.Clone(kb.Scope), nil
}

// updateUnit applies f to an existing unit and persists the result. It is the
// one place that reports an unknown unit, so every mutating operation phrases
// that error the same way.
func (kb *KB) updateUnit(name string, f func(*Unit) error) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	u, ok := kb.Units[name]
	if !ok {
		return unknownUnit(name)
	}
	if err := f(u); err != nil {
		return err
	}
	return kb.save()
}

func unknownUnit(name string) error {
	return fmt.Errorf("unit %q does not exist", name)
}

// validUnitName restricts unit names to characters that are safe in file names
// and readable in Prolog comments.
func validUnitName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("unit name must not be empty")
	case len(name) > 64:
		return fmt.Errorf("unit name must be at most 64 characters")
	}
	for _, r := range name {
		if !isAlnum(r) && r != '-' {
			return fmt.Errorf("unit name %q contains invalid character %q; use letters, digits, '_' and '-'", name, r)
		}
	}
	return nil
}

// CreateUnit adds a new, empty unit and returns the current scope.
func (kb *KB) CreateUnit(name, description string) ([]string, error) {
	if err := validUnitName(name); err != nil {
		return nil, err
	}
	return kb.mutate(func() error {
		if _, ok := kb.Units[name]; ok {
			return fmt.Errorf("unit %q already exists", name)
		}
		kb.Units[name] = &Unit{Name: name, Description: description, NextID: 1}
		return nil
	})
}

// DeleteUnit removes a unit, takes it out of scope, and returns the scope that
// remains.
func (kb *KB) DeleteUnit(name string) ([]string, error) {
	return kb.mutate(func() error {
		if _, ok := kb.Units[name]; !ok {
			return unknownUnit(name)
		}
		delete(kb.Units, name)
		kb.Scope = removeName(kb.Scope, name)
		return nil
	})
}

// ListUnits returns every unit in name order, plus the current scope.
func (kb *KB) ListUnits() ([]UnitSummary, []string) {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	out := make([]UnitSummary, 0, len(kb.Units))
	for _, n := range kb.names() {
		u := kb.Units[n]
		out = append(out, UnitSummary{
			Name:        u.Name,
			Description: u.Description,
			Clauses:     len(u.Clauses),
			InScope:     slices.Contains(kb.Scope, n),
		})
	}
	return out, slices.Clone(kb.Scope)
}

// UnitNames returns the names of every unit, in order.
func (kb *KB) UnitNames() []string {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	return kb.names()
}

// names returns the sorted unit names. Callers must hold kb.mu.
func (kb *KB) names() []string {
	names := make([]string, 0, len(kb.Units))
	for n := range kb.Units {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

func removeName(names []string, drop string) []string {
	return slices.DeleteFunc(names, func(n string) bool { return n == drop })
}

// UnitView is a consistent snapshot of one unit.
type UnitView struct {
	Description string
	Source      string
	Clauses     []Clause
	InScope     bool
}

// Unit returns a snapshot of the named unit. The clauses are numbered by
// position, which is what prolog_retract and prolog_find_clauses report.
func (kb *KB) Unit(name string) (UnitView, error) {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	u, ok := kb.Units[name]
	if !ok {
		return UnitView{}, unknownUnit(name)
	}
	clauses := make([]Clause, len(u.Clauses))
	for i, c := range u.Clauses {
		clauses[i] = Clause{ID: i + 1, Text: c.Text}
	}
	return UnitView{
		Description: u.Description,
		Source:      u.Source(),
		Clauses:     clauses,
		InScope:     slices.Contains(kb.Scope, name),
	}, nil
}

// AddClauses appends clauses parsed from src to a unit.
func (kb *KB) AddClauses(name, src string) ([]Clause, error) {
	texts, err := SplitClauses(src)
	if err != nil {
		return nil, err
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("no clauses found; every clause must end with '.'")
	}
	added := make([]Clause, 0, len(texts))
	err = kb.updateUnit(name, func(u *Unit) error {
		for _, t := range texts {
			added = append(added, u.add(t))
		}
		return nil
	})
	return added, err
}

// ReplaceSource replaces the entire contents of a unit.
func (kb *KB) ReplaceSource(name, src string) ([]Clause, error) {
	texts, err := SplitClauses(src)
	if err != nil {
		return nil, err
	}
	var clauses []Clause
	err = kb.updateUnit(name, func(u *Unit) error {
		u.Clauses, u.NextID = nil, 1
		for _, t := range texts {
			u.add(t)
		}
		clauses = slices.Clone(u.Clauses)
		return nil
	})
	return clauses, err
}

// RemoveAt removes the clauses at the given 1-based positions.
func (kb *KB) RemoveAt(name string, positions []int) ([]Clause, error) {
	var removed []Clause
	err := kb.updateUnit(name, func(u *Unit) error {
		drop := map[int]bool{}
		for _, p := range positions {
			if p < 1 || p > len(u.Clauses) {
				return fmt.Errorf("clause position %d out of range 1..%d", p, len(u.Clauses))
			}
			drop[p] = true
		}
		var kept []Clause
		for i, c := range u.Clauses {
			if drop[i+1] {
				removed = append(removed, c)
			} else {
				kept = append(kept, c)
			}
		}
		u.Clauses = kept
		return nil
	})
	return removed, err
}

// SetScope replaces the query scope with the given units, in order, ignoring
// repeats.
func (kb *KB) SetScope(names []string) ([]string, error) {
	return kb.mutate(func() error {
		scope := make([]string, 0, len(names))
		for _, n := range names {
			if _, ok := kb.Units[n]; !ok {
				return unknownUnit(n)
			}
			if !slices.Contains(scope, n) {
				scope = append(scope, n)
			}
		}
		kb.Scope = scope
		return nil
	})
}

// LoadUnit appends a unit to the query scope.
func (kb *KB) LoadUnit(name string) ([]string, error) {
	return kb.mutate(func() error {
		if _, ok := kb.Units[name]; !ok {
			return unknownUnit(name)
		}
		if !slices.Contains(kb.Scope, name) {
			kb.Scope = append(kb.Scope, name)
		}
		return nil
	})
}

// UnloadUnit removes a unit from the query scope without deleting it.
func (kb *KB) UnloadUnit(name string) ([]string, error) {
	return kb.mutate(func() error {
		if !slices.Contains(kb.Scope, name) {
			return fmt.Errorf("unit %q is not in scope", name)
		}
		kb.Scope = removeName(kb.Scope, name)
		return nil
	})
}

// Program renders the in-scope units as one Prolog source file, and returns the
// units it was built from.
func (kb *KB) Program() (string, []string) {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	var b strings.Builder
	b.WriteString(preamble)
	var used []string
	for _, n := range kb.Scope {
		u, ok := kb.Units[n]
		if !ok {
			continue
		}
		used = append(used, n)
		fmt.Fprintf(&b, "%% ---- unit %s ----\n", n)
		b.WriteString(u.Source())
		b.WriteString("\n")
	}
	return b.String(), used
}
