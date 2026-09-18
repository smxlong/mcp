package scoretex

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const MaxEntities = 10000
const MaxOperations = 1000
const MaxPayload = 4 << 20

type Score struct {
	Root      string           `json:"root"`
	Current   string           `json:"current"`
	Revisions map[string]Graph `json:"revisions"`
	Used      map[string]bool  `json:"used"`
}
type replay struct {
	Request string `json:"request"`
	Result  Object `json:"result"`
}
type database struct {
	Scores map[string]*Score `json:"scores"`
	Keys   map[string]replay `json:"keys"`
}
type Engine struct {
	mu    sync.Mutex
	state database
	path  string
}

func NewEngine(path string) (*Engine, error) {
	e := &Engine{path: path, state: database{Scores: map[string]*Score{}, Keys: map[string]replay{}}}
	if path != "" {
		b, er := os.ReadFile(path)
		if er != nil && !os.IsNotExist(er) {
			return nil, er
		}
		if er == nil {
			decoder := json.NewDecoder(bytes.NewReader(b))
			decoder.UseNumber()
			if er = decoder.Decode(&e.state); er != nil {
				return nil, er
			}
			if e.state.Scores == nil || e.state.Keys == nil {
				return nil, fmt.Errorf("invalid state file")
			}
			for _, s := range e.state.Scores {
				if s == nil || s.Revisions[s.Current] == nil || s.Used == nil {
					return nil, fmt.Errorf("invalid score history")
				}
				for _, g := range s.Revisions {
					if er := checkGraph(g, s.Root); er != nil {
						return nil, er
					}
				}
			}
		}
	}
	return e, nil
}
func uid(prefix string) string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return prefix + "_" + hex.EncodeToString(b)
}
func (e *Engine) save(state database) error {
	if e.path == "" {
		return nil
	}
	b, er := json.Marshal(state)
	if er != nil {
		return er
	}
	f, er := os.CreateTemp(filepath.Dir(e.path), ".scoretex-*")
	if er != nil {
		return er
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, er = f.Write(b); er == nil {
		er = f.Sync()
	}
	closeErr := f.Close()
	if er != nil {
		return er
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), e.path)
}

var requestFields = map[string]string{
	"create": "metadata defaults parts source", "inspect": "revision_id selector projection context limit cursor", "insert": "at content collision_policy", "replace": "selector content mode preserve relation_policy", "delete": "selector mode relation_policy", "transform": "selector operation parameters", "patch": "operations", "validate": "revision_id selector levels profile", "render": "revision_id selector format options include_diagnostics", "diff": "revision_a revision_b selector granularity",
}

func mutation(tool string) bool {
	return tool == "create" || tool == "insert" || tool == "replace" || tool == "delete" || tool == "transform" || tool == "patch"
}
func (e *Engine) Call(ctx context.Context, tool string, args Object) (Object, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if er := ctx.Err(); er != nil {
		return nil, er
	}
	allowed, ok := requestFields[tool]
	if !ok {
		return nil, fail("UNSUPPORTED_OPERATION", "unknown tool")
	}
	if args == nil {
		return nil, invalid("arguments must be an object")
	}
	args = clone(args)
	if er := fields(args, allowed+" score_id base_revision_id idempotency_key validation_mode"); er != nil {
		return nil, er
	}
	raw, _ := json.Marshal(args)
	if len(raw) > MaxPayload {
		return nil, fail("LIMIT_EXCEEDED", "request exceeds 4 MiB")
	}
	if er := normalize(args); er != nil {
		return nil, er
	}
	if mutation(tool) {
		if args["validation_mode"] == nil {
			args["validation_mode"] = "structural"
		}
		switch args["validation_mode"] {
		case "structural", "standard", "strict":
		default:
			return nil, invalid("validation_mode must be structural, standard or strict")
		}
	}
	raw, _ = json.Marshal(args)
	request := tool + ":" + string(raw)
	key := str(args["idempotency_key"])
	if mutation(tool) && key != "" {
		if r, ok := e.state.Keys[key]; ok {
			if r.Request != request {
				return nil, fail("IDEMPOTENCY_CONFLICT", "idempotency key already used for a different request")
			}
			return clone(r.Result), nil
		}
	}
	if tool == "create" {
		if len(e.state.Scores) >= 100 {
			return nil, fail("LIMIT_EXCEEDED", "store contains 100 scores")
		}
		return e.create(ctx, args, key, request)
	}
	sid := str(args["score_id"])
	s := e.state.Scores[sid]
	if s == nil {
		return nil, fail("SCORE_NOT_FOUND", "score_id does not exist")
	}
	rev := text(args, "revision_id", s.Current)
	g := s.Revisions[rev]
	if g == nil {
		return nil, fail("REVISION_NOT_FOUND", "revision_id does not exist")
	}
	if !mutation(tool) {
		return read(tool, args, sid, rev, s, g)
	}
	if str(args["base_revision_id"]) != s.Current {
		return nil, &Fault{Code: "REVISION_CONFLICT", Message: "base_revision_id must equal current revision", Retryable: true, Path: "/base_revision_id", Details: Object{"current_revision_id": s.Current}, Diagnostics: []Object{}}
	}
	if len(s.Revisions) >= 256 {
		return nil, fail("LIMIT_EXCEEDED", "score contains 256 revisions")
	}
	tx := &transaction{g: clone(s.Revisions[s.Current]), root: s.Root, used: clone(s.Used)}
	if er := tx.apply(tool, args); er != nil {
		return nil, er
	}
	if len(tx.g) > MaxEntities {
		return nil, fail("LIMIT_EXCEEDED", "score exceeds 10000 entities")
	}
	if er := checkGraph(tx.g, s.Root); er != nil {
		return nil, structural(er)
	}
	ds := diagnostics(tx.g, s.Root, []any{"notation", "performance"})
	if args["validation_mode"] != "structural" {
		for _, d := range ds {
			if d["severity"] == "error" {
				return nil, &Fault{Code: "VALIDATION_FAILED", Message: "mutation rejected by validation mode", Details: Object{}, Diagnostics: ds}
			}
		}
	}
	next := uid("rev")
	changes := changes(s.Revisions[s.Current], tx.g)
	out := Object{"score_id": sid, "previous_revision_id": s.Current, "revision_id": next, "created_ids": changes["created_ids"], "changed_ids": changes["changed_ids"], "deleted_ids": changes["deleted_ids"], "diagnostics": ds, "summary": fmt.Sprintf("Committed %s with %d entity changes.", tool, len(arr(changes["changes"])))}
	state := clone(e.state)
	state.Scores[sid].Current = next
	state.Scores[sid].Revisions[next] = tx.g
	state.Scores[sid].Used = tx.used
	return e.commit(ctx, state, key, request, out)
}
func structural(er error) error {
	if f, ok := er.(*Fault); ok && f.Code == "INVALID_ARGUMENT" {
		f.Code = "STRUCTURAL_VIOLATION"
	}
	return er
}
func (e *Engine) commit(ctx context.Context, state database, key, request string, out Object) (Object, error) {
	if er := ctx.Err(); er != nil {
		return nil, er
	}
	if key != "" {
		state.Keys[key] = replay{request, clone(out)}
	}
	if er := e.save(state); er != nil {
		return nil, fail("INTERNAL_ERROR", "could not persist transaction: "+er.Error())
	}
	e.state = state
	return clone(out), nil
}
func (e *Engine) create(ctx context.Context, a Object, key, request string) (Object, error) {
	if a["source"] != nil {
		return nil, fail("UNSUPPORTED_FORMAT", "external import is not supported")
	}
	g := Graph{}
	tx := &transaction{g: g, used: map[string]bool{}}
	root, _ := tx.add(Object{"kind": "score", "metadata": a["metadata"], "part_ids": []any{}})
	tx.root = root
	for _, v := range arr(a["parts"]) {
		p := obj(v)
		if er := fields(p, "name staves voices_per_staff"); er != nil {
			return nil, er
		}
		staves, voices := int64(1), int64(1)
		if v, ok := p["staves"]; ok {
			staves, _ = integer(v)
		}
		if v, ok := p["voices_per_staff"]; ok {
			voices, _ = integer(v)
		}
		if staves < 1 || voices < 1 || staves > 32 || voices > 32 {
			return nil, invalid("staves and voices_per_staff must be between 1 and 32")
		}
		inst, _ := tx.add(Object{"kind": "instrument", "name": text(p, "name", "Instrument")})
		part, _ := tx.add(Object{"kind": "part", "name": text(p, "name", "Part"), "instrument_ids": []any{inst}, "staff_ids": []any{}})
		g[root]["part_ids"] = append(arr(g[root]["part_ids"]), part)
		for i := int64(0); i < staves; i++ {
			staff, _ := tx.add(Object{"kind": "staff", "part_id": part, "index": i, "lines": 5, "voice_ids": []any{}})
			g[part]["staff_ids"] = append(arr(g[part]["staff_ids"]), staff)
			for j := int64(0); j < voices; j++ {
				voice, _ := tx.add(Object{"kind": "voice", "staff_id": staff, "index": j})
				g[staff]["voice_ids"] = append(arr(g[staff]["voice_ids"]), voice)
			}
		}
	}
	defaults := obj(a["defaults"])
	if er := fields(defaults, "time_signature tempo"); er != nil {
		return nil, er
	}
	for _, kind := range []string{"time_signature", "tempo"} {
		if v, ok := defaults[kind]; ok {
			if _, er := tx.add(Object{"kind": "directive", "directive_type": kind, "scope_ids": []any{root}, "onset": rat(0, 1), "value": v}); er != nil {
				return nil, er
			}
		}
	}
	if len(g) > MaxEntities {
		return nil, fail("LIMIT_EXCEEDED", "score exceeds 10000 entities")
	}
	if er := checkGraph(g, root); er != nil {
		return nil, structural(er)
	}
	sid, rev := uid("score"), uid("rev")
	state := clone(e.state)
	state.Scores[sid] = &Score{Root: root, Current: rev, Revisions: map[string]Graph{rev: g}, Used: tx.used}
	out := Object{"score_id": sid, "revision_id": rev, "root_id": root, "created_ids": ordered(g, root), "diagnostics": []Object{}}
	return e.commit(ctx, state, key, request, out)
}
