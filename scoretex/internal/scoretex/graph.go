package scoretex

import (
	"fmt"
	"sort"
	"strings"
)

var shapes = map[string]string{
	"score": "metadata part_ids", "part": "name short_name instrument_ids staff_ids", "instrument": "name transposition written_range sounding_range taxonomy playback",
	"staff": "part_id index lines voice_ids", "voice": "staff_id index", "measure": "scope_ids start duration number",
	"note": "voice_id onset duration pitch notation", "chord": "voice_id onset duration tone_ids notation", "tone": "pitch notation", "rest": "voice_id onset duration notation",
	"directive": "scope_ids onset directive_type value", "annotation": "annotation_type anchor value", "spanner": "spanner_type start_anchor end_anchor", "group": "group_type member_ids", "layout": "anchor layout_type value",
}
var mandatory = map[string]string{"score": "part_ids", "part": "name instrument_ids staff_ids", "instrument": "name", "staff": "part_id index voice_ids", "voice": "staff_id index", "measure": "scope_ids start duration", "note": "voice_id onset duration pitch", "rest": "voice_id onset duration", "chord": "voice_id onset duration tone_ids", "tone": "pitch", "directive": "scope_ids onset directive_type value", "annotation": "annotation_type anchor", "spanner": "spanner_type start_anchor end_anchor", "group": "group_type member_ids", "layout": "anchor layout_type"}
var refs = map[string]string{"part_ids": "part", "instrument_ids": "instrument", "staff_ids": "staff", "voice_ids": "voice", "tone_ids": "tone", "part_id": "part", "staff_id": "staff", "voice_id": "voice", "scope_ids": "", "member_ids": ""}

func children(g Graph, id string) []string {
	e := g[id]
	out := []string{}
	for _, k := range []string{"part_ids", "instrument_ids", "staff_ids", "voice_ids", "tone_ids", "member_ids"} {
		for _, v := range arr(e[k]) {
			out = append(out, str(v))
		}
	}
	if e["kind"] == "voice" {
		for eid, v := range g {
			if v["voice_id"] == id {
				out = append(out, eid)
			}
		}
	}
	return out
}
func descendants(g Graph, id string, seen map[string]bool) {
	for _, c := range children(g, id) {
		if !seen[c] {
			seen[c] = true
			descendants(g, c, seen)
		}
	}
}
func anchored(v any, id string) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if (k == "entity_id" || k == "instrument_id") && v == id {
				return true
			}
			if anchored(v, id) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if anchored(v, id) {
				return true
			}
		}
	}
	return false
}
func checkGraph(g Graph, root string) error {
	if g[root] == nil || g[root]["kind"] != "score" {
		return fail("STRUCTURAL_VIOLATION", "score root must exist")
	}
	owners := map[string]string{}
	for _, id := range ordered(g, root) {
		e := g[id]
		kind := str(e["kind"])
		shape, ok := shapes[kind]
		if !ok {
			return fail("UNSUPPORTED_OPERATION", "unsupported entity kind: "+kind)
		}
		if str(e["id"]) != id {
			return invalid("entity ID is immutable")
		}
		if er := fields(e, "id kind order "+shape); er != nil {
			return er
		}
		if er := required(e, mandatory[kind]); er != nil {
			return er
		}
		if er := entityValues(e, g); er != nil {
			return er
		}
		if e["pitch"] != nil {
			if er := pitch(e["pitch"]); er != nil {
				return er
			}
		}
		for _, k := range []string{"onset", "start", "duration"} {
			if v, ok := e[k]; ok {
				r, er := rational(v)
				if er != nil {
					return er
				}
				if r.Sign() < 0 || (k == "duration" && r.Sign() == 0) {
					return invalid(k + " must be nonnegative; ordinary durations must be positive")
				}
			}
		}
		for k, want := range refs {
			v, exists := e[k]
			if !exists {
				continue
			}
			values := []any{v}
			if strings.HasSuffix(k, "_ids") {
				values = arr(v)
				if values == nil {
					return invalid(k + " must be an array")
				}
			}
			seen := map[string]bool{}
			for _, v := range values {
				rid := str(v)
				target := g[rid]
				if target == nil || (want != "" && target["kind"] != want) {
					return fail("STRUCTURAL_VIOLATION", fmt.Sprintf("%s.%s references missing or incompatible entity %s", id, k, rid))
				}
				if seen[rid] {
					return invalid("duplicate reference in " + k)
				}
				seen[rid] = true
			}
		}
		if kind == "score" && id != root {
			return invalid("only one score root is allowed")
		}
		if kind == "chord" && len(arr(e["tone_ids"])) < 2 {
			return fail("STRUCTURAL_VIOLATION", "chord requires at least two tones")
		}
		for _, k := range []string{"part_ids", "instrument_ids", "staff_ids", "voice_ids", "tone_ids"} {
			for _, v := range arr(e[k]) {
				cid := str(v)
				if owners[cid] != "" {
					return fail("STRUCTURAL_VIOLATION", "entity has multiple owners: "+cid)
				}
				owners[cid] = id
			}
		}
		for _, k := range []string{"anchor", "start_anchor", "end_anchor"} {
			if v, ok := e[k]; ok {
				a := obj(v)
				if er := fields(a, "entity_id time scope_id"); er != nil {
					return er
				}
				if a["entity_id"] != nil {
					if g[str(a["entity_id"])] == nil {
						return fail("STRUCTURAL_VIOLATION", "anchor entity does not exist")
					}
				} else {
					if _, er := rational(a["time"]); er != nil {
						return invalid("anchor requires entity_id or exact time")
					}
					if g[str(a["scope_id"])] == nil {
						return invalid("position anchor requires valid scope_id")
					}
				}
			}
		}
		if kind == "spanner" && e["spanner_type"] == "tie" {
			a := g[str(obj(e["start_anchor"])["entity_id"])]
			b := g[str(obj(e["end_anchor"])["entity_id"])]
			if a["pitch"] == nil || !equal(a["pitch"], b["pitch"]) {
				return fail("STRUCTURAL_VIOLATION", "tie requires compatible pitched anchors")
			}
		}
	}
	for id, e := range g {
		k := str(e["kind"])
		switch k {
		case "part", "instrument", "staff", "voice", "tone":
			if owners[id] == "" {
				return fail("STRUCTURAL_VIOLATION", "entity lacks structural owner: "+id)
			}
		}
		if k == "staff" && owners[id] != e["part_id"] {
			return invalid("staff ownership disagrees with part_id")
		}
		if k == "voice" && owners[id] != e["staff_id"] {
			return invalid("voice ownership disagrees with staff_id")
		}
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var walk func(string) error
	walk = func(id string) error {
		if visiting[id] {
			return fail("STRUCTURAL_VIOLATION", "ownership cycle")
		}
		if done[id] {
			return nil
		}
		visiting[id] = true
		for _, c := range children(g, id) {
			if er := walk(c); er != nil {
				return er
			}
		}
		visiting[id] = false
		done[id] = true
		return nil
	}
	for id := range g {
		if er := walk(id); er != nil {
			return er
		}
	}
	return nil
}
func temporalEntity(g Graph, e Object) Object {
	if e["kind"] != "tone" {
		return e
	}
	for _, owner := range g {
		if owner["kind"] == "chord" {
			for _, tone := range arr(owner["tone_ids"]) {
				if tone == e["id"] {
					return owner
				}
			}
		}
	}
	return e
}

func ordered(g Graph, root string) []string {
	ranks := map[string]int{}
	seen := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		ranks[id] = len(ranks)
		for _, k := range []string{"part_ids", "instrument_ids", "staff_ids", "voice_ids", "tone_ids"} {
			for _, v := range arr(g[id][k]) {
				walk(str(v))
			}
		}
	}
	walk(root)
	ids := make([]string, 0, len(g))
	for id := range g {
		ids = append(ids, id)
	}
	rank := func(id string) int {
		e := temporalEntity(g, g[id])
		if v := str(e["voice_id"]); v != "" {
			return ranks[v]
		}
		if n, ok := ranks[id]; ok {
			return n
		}
		return len(ranks)
	}
	sort.SliceStable(ids, func(i, j int) bool {
		a, b := ids[i], ids[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if c := cmp(onset(temporalEntity(g, g[a])), onset(temporalEntity(g, g[b]))); c != 0 {
			return c < 0
		}
		x, _ := integer(g[a]["order"])
		y, _ := integer(g[b]["order"])
		return x < y
	})
	return ids
}
func diagnostics(g Graph, root string, levels []any) []Object {
	out := []Object{}
	has := func(s string) bool {
		for _, v := range levels {
			if v == s {
				return true
			}
		}
		return false
	}
	for _, id := range ordered(g, root) {
		e := g[id]
		if has("performance") && e["pitch"] != nil {
			p := obj(e["pitch"])
			r, _ := rational(p["alter"])
			if r != nil && !r.IsInt() {
				out = append(out, Object{"rule_id": "performance.microtonal.mapping", "severity": "warning", "message": "Fractional alteration requires a compatible playback renderer.", "entity_ids": []string{id}})
			}
		}
		if has("notation") && e["kind"] == "measure" {
			for vid, v := range g {
				if v["kind"] != "voice" {
					continue
				}
				scoped := false
				for _, s := range arr(e["scope_ids"]) {
					if v["staff_id"] == s || vid == s {
						scoped = true
					}
				}
				if !scoped {
					continue
				}
				total := any(rat(0, 1))
				for _, n := range g {
					if n["voice_id"] == vid && timed(n) && overlap(n, e) {
						if cmp(onset(n), onset(e)) < 0 || cmp(end(n), end(e)) > 0 {
							out = append(out, Object{"rule_id": "notation.measure.cross_boundary", "severity": "error", "message": "Event crosses a measure boundary.", "entity_ids": []string{str(n["id"]), id}})
						}
						total, _ = calc(total, n["duration"], '+')
					}
				}
				c := cmp(total, e["duration"])
				if c != 0 {
					rule, sev := "underfilled", "warning"
					if c > 0 {
						rule, sev = "overfilled", "error"
					}
					out = append(out, Object{"rule_id": "notation.measure." + rule, "severity": sev, "message": "Voice is " + rule + " in measure.", "entity_ids": []string{vid, id}})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return fmt.Sprint(a["rule_id"], a["entity_ids"]) < fmt.Sprint(b["rule_id"], b["entity_ids"])
	})
	return out
}
