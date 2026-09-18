package scoretex

import (
	"strings"
)

type transaction struct {
	g    Graph
	root string
	used map[string]bool
}

func (t *transaction) add(e Object) (string, error) {
	if e == nil {
		return "", invalid("entity must be an object")
	}
	e = clone(e)
	if v, ok := e["id"]; ok && str(v) == "" {
		return "", invalid("client entity id must be a nonempty string")
	}
	id := str(e["id"])
	if id == "" {
		id = uid("entity")
	}
	if t.used[id] {
		return "", fail("STRUCTURAL_VIOLATION", "entity ID has already been used: "+id)
	}
	if len(t.g) >= MaxEntities {
		return "", fail("LIMIT_EXCEEDED", "score exceeds 10000 entities")
	}
	e["id"] = id
	e["order"] = len(t.used)
	t.g[id] = e
	t.used[id] = true
	return id, nil
}
func (t *transaction) apply(tool string, a Object) error {
	switch tool {
	case "patch":
		return t.patch(arr(a["operations"]))
	case "insert":
		return t.insert(a)
	case "replace", "delete", "transform":
		ids, er := selectIDs(t.g, t.root, a["selector"])
		if er != nil {
			return er
		}
		if a["selector"] == nil {
			return invalid("mutation requires an explicit selector")
		}
		if len(ids) == 0 {
			return fail("SELECTION_EMPTY", "selector matches no entities")
		}
		switch tool {
		case "replace":
			return t.replace(ids, a)
		case "delete":
			return t.delete(ids, a)
		default:
			return t.transform(ids, str(a["operation"]), obj(a["parameters"]))
		}
	}
	return invalid("unknown mutation")
}
func pointer(e Object, path string, value any, unset bool) error {
	if !strings.HasPrefix(path, "/") || path == "/" {
		return invalid("path must be a nonempty JSON Pointer")
	}
	parts := strings.Split(path[1:], "/")
	m := e
	for i, k := range parts {
		k = strings.ReplaceAll(strings.ReplaceAll(k, "~1", "/"), "~0", "~")
		if i == 0 && (k == "id" || k == "kind" || k == "order") {
			return invalid("id, kind and order cannot be patched")
		}
		if i == len(parts)-1 {
			if unset {
				delete(m, k)
			} else {
				m[k] = clone(value)
			}
			return nil
		}
		m = obj(m[k])
		if m == nil {
			return invalid("path parent must exist and be an object")
		}
	}
	return nil
}
func (t *transaction) patch(ops []any) error {
	if len(ops) == 0 {
		return invalid("operations must be nonempty")
	}
	if len(ops) > MaxOperations {
		return fail("LIMIT_EXCEEDED", "patch exceeds 1000 operations")
	}
	for _, v := range ops {
		o := obj(v)
		if er := fields(o, "op id path value entity owner_id relation_policy to relation target_id"); er != nil {
			return er
		}
		op := str(o["op"])
		id := str(o["id"])
		e := t.g[id]
		if op != "create" && e == nil {
			return fail("ENTITY_NOT_FOUND", "patch entity does not exist: "+id)
		}
		switch op {
		case "create":
			created, er := t.add(obj(o["entity"]))
			if er != nil {
				return er
			}
			if owner := str(o["owner_id"]); owner != "" {
				if er := t.attach(created, owner, str(o["relation"])); er != nil {
					return er
				}
			}
		case "test":
			v, ok := field(e, str(o["path"]))
			if !ok || !equal(v, o["value"]) {
				return fail("PATCH_TEST_FAILED", "patch test failed at "+str(o["path"]))
			}
		case "set", "unset":
			if er := pointer(e, str(o["path"]), o["value"], op == "unset"); er != nil {
				return er
			}
		case "move":
			to := obj(o["to"])
			if len(to) == 0 {
				return invalid("move requires explicit to fields")
			}
			if er := fields(to, "voice_id onset staff_id part_id owner_id relation index"); er != nil {
				return er
			}
			if to["owner_id"] != nil {
				t.detach(id)
				if er := t.attach(id, str(to["owner_id"]), str(to["relation"])); er != nil {
					return er
				}
			}
			for k, v := range to {
				if k != "owner_id" && k != "relation" {
					e[k] = v
				}
			}
		case "delete":
			if er := t.remove([]string{id}, text(o, "relation_policy", "reject"), nil, nil); er != nil {
				return er
			}
		case "link", "unlink":
			rel := str(o["relation"])
			if _, ok := refs[rel]; !ok || !strings.HasSuffix(rel, "_ids") {
				return invalid("relation must be a typed ID array field")
			}
			if t.g[str(o["target_id"])] == nil {
				return fail("ENTITY_NOT_FOUND", "relationship target does not exist")
			}
			vs := arr(e[rel])
			out := []any{}
			for _, v := range vs {
				if v != o["target_id"] {
					out = append(out, v)
				}
			}
			if op == "link" {
				out = append(out, o["target_id"])
			}
			e[rel] = out
		default:
			return fail("UNSUPPORTED_OPERATION", "unsupported patch operation: "+op)
		}
	}
	return nil
}
func (t *transaction) attach(id, owner, relation string) error {
	e, p := t.g[id], t.g[owner]
	if p == nil {
		return fail("ENTITY_NOT_FOUND", "owner does not exist")
	}
	if relation == "" {
		switch e["kind"] {
		case "part":
			relation = "part_ids"
		case "staff":
			relation = "staff_ids"
			e["part_id"] = owner
		case "voice":
			relation = "voice_ids"
			e["staff_id"] = owner
		case "instrument":
			relation = "instrument_ids"
		case "tone":
			relation = "tone_ids"
		default:
			return invalid("explicit owner relation required")
		}
	}
	if _, ok := refs[relation]; !ok || !strings.HasSuffix(relation, "_ids") {
		return invalid("unsupported owner relation")
	}
	p[relation] = append(arr(p[relation]), id)
	return nil
}
func (t *transaction) detach(id string) {
	for _, e := range t.g {
		for _, k := range []string{"part_ids", "staff_ids", "voice_ids", "instrument_ids", "tone_ids", "member_ids"} {
			if vs, ok := e[k]; ok {
				out := []any{}
				for _, v := range arr(vs) {
					if v != id {
						out = append(out, v)
					}
				}
				e[k] = out
			}
		}
	}
}
func (t *transaction) position(at Object) (any, error) {
	if v, ok := at["time"]; ok {
		if _, er := rational(v); er != nil {
			return nil, er
		}
		return v, nil
	}
	if id := str(at["measure_id"]); id != "" {
		m := t.g[id]
		if m == nil || m["kind"] != "measure" {
			return nil, fail("ENTITY_NOT_FOUND", "measure does not exist")
		}
		offset := at["offset"]
		if offset == nil {
			offset = rat(0, 1)
		}
		if _, er := rational(offset); er != nil {
			return nil, er
		}
		if cmp(offset, rat(0, 1)) < 0 || cmp(offset, m["duration"]) >= 0 {
			return nil, invalid("measure offset must lie inside measure")
		}
		return calc(m["start"], offset, '+')
	}
	return nil, nil
}
func (t *transaction) insert(a Object) error {
	at := obj(a["at"])
	if er := fields(at, "voice_id staff_id time measure_id offset owner_id relation anchor"); er != nil {
		return er
	}
	pos, er := t.position(at)
	if er != nil {
		return er
	}
	content := arr(a["content"])
	if len(content) == 0 {
		return invalid("content must be nonempty")
	}
	if len(content) > MaxOperations {
		return fail("LIMIT_EXCEEDED", "content exceeds 1000 entities")
	}
	policy := text(a, "collision_policy", "reject")
	if policy != "reject" && policy != "overlay" && policy != "shift_forward" {
		return invalid("unknown collision_policy")
	}
	original := clone(t.g)
	newIDs := []string{}
	start := pos
	for _, v := range content {
		e := clone(obj(v))
		if e == nil {
			return invalid("content entities must be objects")
		}
		kind := str(e["kind"])
		if kind == "note" || kind == "rest" || kind == "chord" {
			if e["voice_id"] == nil {
				e["voice_id"] = at["voice_id"]
			}
			if e["onset"] == nil {
				e["onset"] = pos
			}
			if _, er := rational(e["onset"]); er != nil {
				return invalid("timed insertion requires time or onset")
			}
			pos, er = calc(e["onset"], e["duration"], '+')
			if er != nil {
				return er
			}
		}
		if kind == "directive" {
			if e["onset"] == nil {
				e["onset"] = pos
			}
			if e["scope_ids"] == nil && at["staff_id"] != nil {
				e["scope_ids"] = []any{at["staff_id"]}
			}
		}
		if (kind == "annotation" || kind == "layout") && e["anchor"] == nil {
			e["anchor"] = at["anchor"]
		}
		id, er := t.add(e)
		if er != nil {
			return er
		}
		newIDs = append(newIDs, id)
		if owner := str(at["owner_id"]); owner != "" {
			if er := t.attach(id, owner, str(at["relation"])); er != nil {
				return er
			}
		}
	}
	if policy == "shift_forward" {
		voice := str(at["voice_id"])
		if voice == "" || start == nil || pos == nil {
			return invalid("shift_forward requires explicit voice_id and time")
		}
		delta, er := calc(pos, start, '-')
		if er != nil {
			return er
		}
		if cmp(delta, rat(0, 1)) <= 0 {
			return invalid("shift_forward requires a positive inserted span")
		}
		for _, id := range newIDs {
			e := t.g[id]
			if timed(e) && (e["voice_id"] != voice || cmp(onset(e), start) < 0) {
				return invalid("shift_forward content must lie in the explicit voice at or after insertion time")
			}
		}
		for id, e := range original {
			if timed(e) && e["voice_id"] == voice {
				if cmp(onset(e), start) < 0 && cmp(end(e), start) > 0 {
					return fail("COLLISION", "insertion intersects an existing event")
				}
				if cmp(onset(e), start) >= 0 {
					v, er := calc(e["onset"], delta, '+')
					if er != nil {
						return er
					}
					t.g[id]["onset"] = v
				}
			}
		}
	}
	if policy != "overlay" {
		for _, id := range newIDs {
			e := t.g[id]
			if !timed(e) {
				continue
			}
			for other, n := range t.g {
				if other != id && timed(n) && n["voice_id"] == e["voice_id"] && overlap(e, n) {
					return fail("COLLISION", "inserted events overlap in one voice")
				}
			}
		}
	}
	return nil
}
func replaceRefs(v any, mapping map[string]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if k == "id" {
				continue
			}
			if s, ok := v.(string); ok {
				if n, ok := mapping[s]; ok && (strings.HasSuffix(k, "_id")) {
					x[k] = n
				}
			}
			if strings.HasSuffix(k, "_ids") {
				vs := arr(v)
				for i, v := range vs {
					if n, ok := mapping[str(v)]; ok {
						vs[i] = n
					}
				}
			}
			replaceRefs(v, mapping)
		}
	case []any:
		for _, v := range x {
			replaceRefs(v, mapping)
		}
	}
}
func (t *transaction) remove(ids []string, policy string, mapping map[string]string, preserve []any) error {
	if policy != "reject" && policy != "cascade" && policy != "reattach" {
		return invalid("unknown relation_policy")
	}
	gone := map[string]bool{}
	var owned func(string)
	owned = func(id string) {
		if gone[id] {
			return
		}
		gone[id] = true
		for _, c := range children(t.g, id) {
			if t.g[id]["kind"] != "group" {
				owned(c)
			}
		}
	}
	for _, id := range ids {
		owned(id)
	}
	if gone[t.root] {
		return invalid("cannot delete score root")
	}
	for changed := true; changed; {
		changed = false
		for id, e := range t.g {
			if gone[id] {
				continue
			}
			for target := range gone {
				dependent := anchored(e, target)
				for _, v := range arr(e["member_ids"]) {
					dependent = dependent || v == target
				}
				for _, v := range arr(e["scope_ids"]) {
					dependent = dependent || v == target
				}
				if !dependent {
					continue
				}
				keep := false
				for _, p := range preserve {
					keep = keep || (p == "annotations" && e["kind"] == "annotation") || (p == "layout" && e["kind"] == "layout")
				}
				if (policy == "reattach" || keep) && mapping[target] != "" {
					replaceRefs(e, mapping)
					continue
				}
				if policy == "cascade" {
					owned(id)
					changed = true
					break
				}
				return fail("STRUCTURAL_VIOLATION", "deletion would break a relation; use cascade or compatible reattach")
			}
		}
	}
	for id := range gone {
		t.detach(id)
		delete(t.g, id)
	}
	return nil
}
func (t *transaction) replace(ids []string, a Object) error {
	content := arr(a["content"])
	mode := text(a, "mode", "objects")
	preserve := arr(a["preserve"])
	for _, p := range preserve {
		switch p {
		case "onset", "duration", "voice", "annotations", "layout":
		default:
			return invalid("unknown preserve property")
		}
	}
	if mode == "region" {
		if len(preserve) > 0 {
			return fail("SELECTION_AMBIGUOUS", "region preservation requires an object mapping")
		}
		voice, start, stop, er := t.span(ids)
		if er != nil {
			return er
		}
		if er = t.remove(ids, text(a, "relation_policy", "reject"), nil, nil); er != nil {
			return er
		}
		before := clone(t.g)
		if er = t.insert(Object{"at": Object{"voice_id": voice, "time": start}, "content": content}); er != nil {
			return er
		}
		last := start
		for id, e := range t.g {
			if before[id] == nil && timed(e) && cmp(end(e), last) > 0 {
				last = end(e)
			}
		}
		if cmp(last, stop) != 0 {
			return invalid("region replacement must exactly fit the selected span")
		}
		return nil
	}
	if mode != "objects" {
		return invalid("mode must be objects or region")
	}
	if len(ids) != len(content) {
		return fail("CARDINALITY_MISMATCH", "object replacement requires one replacement per selected entity")
	}
	mapping := map[string]string{}
	for i, id := range ids {
		old := t.g[id]
		e := clone(obj(content[i]))
		if e == nil {
			return invalid("replacement must be an object")
		}
		for _, p := range preserve {
			k := str(p)
			if k == "voice" {
				k = "voice_id"
			}
			if k != "annotations" && k != "layout" {
				if v, ok := old[k]; ok {
					e[k] = v
				} else {
					return fail("SELECTION_AMBIGUOUS", "preserved property is absent: "+k)
				}
			}
		}
		if e["voice_id"] == nil && old["voice_id"] != nil {
			e["voice_id"] = old["voice_id"]
		}
		nid, er := t.add(e)
		if er != nil {
			return er
		}
		mapping[id] = nid
	}
	for _, e := range t.g {
		for _, k := range []string{"part_ids", "staff_ids", "voice_ids", "instrument_ids", "tone_ids"} {
			vs := arr(e[k])
			for i, v := range vs {
				if n, ok := mapping[str(v)]; ok {
					vs[i] = n
				}
			}
		}
	}
	return t.remove(ids, text(a, "relation_policy", "reject"), mapping, preserve)
}
func (t *transaction) span(ids []string) (string, any, any, error) {
	voice := ""
	var start, stop any
	for _, id := range ids {
		e := t.g[id]
		if !timed(e) {
			return "", nil, nil, invalid("operation requires timed events")
		}
		v := str(e["voice_id"])
		if voice != "" && voice != v {
			return "", nil, nil, fail("SELECTION_AMBIGUOUS", "temporal operation requires one voice")
		}
		voice = v
		if start == nil || cmp(onset(e), start) < 0 {
			start = onset(e)
		}
		if stop == nil || cmp(end(e), stop) > 0 {
			stop = end(e)
		}
	}
	return voice, start, stop, nil
}
func (t *transaction) delete(ids []string, a Object) error {
	mode := text(a, "mode", "remove")
	policy := text(a, "relation_policy", "reject")
	if mode == "remove" {
		return t.remove(ids, policy, nil, nil)
	}
	if mode == "replace_with_rest" || mode == "preserve_measure_duration" {
		content := []any{}
		for _, id := range ids {
			e := t.g[id]
			if !timed(e) {
				return invalid("rest replacement requires timed events")
			}
			content = append(content, Object{"kind": "rest", "voice_id": e["voice_id"], "onset": e["onset"], "duration": e["duration"]})
		}
		if er := t.replace(ids, Object{"content": content, "relation_policy": policy}); er != nil {
			return er
		}
		if mode == "preserve_measure_duration" {
			return t.fillMeasures(content)
		}
		return nil
	}
	if mode != "close_time" {
		return invalid("unknown delete mode")
	}
	voice, start, stop, er := t.span(ids)
	if er != nil {
		return er
	}
	delta, er := calc(stop, start, '-')
	if er != nil {
		return er
	}
	if er = t.remove(ids, policy, nil, nil); er != nil {
		return er
	}
	for _, e := range t.g {
		if e["voice_id"] == voice && timed(e) {
			if cmp(onset(e), stop) >= 0 {
				e["onset"], er = calc(e["onset"], delta, '-')
				if er != nil {
					return er
				}
			} else if cmp(end(e), start) > 0 {
				return fail("COLLISION", "unselected event crosses removed span")
			}
		}
	}
	return t.collisions(voice)
}
func (t *transaction) collisions(voice string) error {
	ids := ordered(t.g, t.root)
	for i, id := range ids {
		a := t.g[id]
		if !timed(a) || a["voice_id"] != voice {
			continue
		}
		for _, other := range ids[i+1:] {
			b := t.g[other]
			if timed(b) && b["voice_id"] == voice && overlap(a, b) {
				return fail("COLLISION", "temporal edit creates overlapping events")
			}
		}
	}
	return nil
}
func (t *transaction) fillMeasures(content []any) error {
	done := map[string]bool{}
	for _, v := range content {
		n := obj(v)
		voice := str(n["voice_id"])
		staff := t.g[voice]["staff_id"]
		for mid, m := range clone(t.g) {
			if m["kind"] != "measure" || !overlap(n, m) {
				continue
			}
			scope := false
			for _, s := range arr(m["scope_ids"]) {
				scope = scope || s == staff || s == voice
			}
			if !scope || done[mid+voice] {
				continue
			}
			done[mid+voice] = true
			cursor := onset(m)
			for _, id := range ordered(t.g, t.root) {
				e := t.g[id]
				if e["voice_id"] != voice || !timed(e) || !overlap(e, m) {
					continue
				}
				if cmp(onset(e), cursor) > 0 {
					d, er := calc(onset(e), cursor, '-')
					if er != nil {
						return er
					}
					if _, er = t.add(Object{"kind": "rest", "voice_id": voice, "onset": cursor, "duration": d}); er != nil {
						return er
					}
				}
				if cmp(end(e), cursor) > 0 {
					cursor = end(e)
				}
			}
			if cmp(cursor, end(m)) < 0 {
				d, er := calc(end(m), cursor, '-')
				if er != nil {
					return er
				}
				if _, er = t.add(Object{"kind": "rest", "voice_id": voice, "onset": cursor, "duration": d}); er != nil {
					return er
				}
			}
		}
	}
	return nil
}
