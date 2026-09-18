package scoretex

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
)

func changes(a, b Graph) Object {
	created, changed, deleted := []any{}, []any{}, []any{}
	out := []any{}
	ids := map[string]bool{}
	for id := range a {
		ids[id] = true
	}
	for id := range b {
		ids[id] = true
	}
	keys := []string{}
	for id := range ids {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		switch {
		case a[id] == nil:
			created = append(created, id)
			out = append(out, Object{"type": "created", "entity_id": id, "after": b[id]})
		case b[id] == nil:
			deleted = append(deleted, id)
			out = append(out, Object{"type": "deleted", "entity_id": id, "before": a[id]})
		case !equal(a[id], b[id]):
			changed = append(changed, id)
			fields := []string{}
			ks := map[string]bool{}
			for k := range a[id] {
				ks[k] = true
			}
			for k := range b[id] {
				ks[k] = true
			}
			for k := range ks {
				if !equal(a[id][k], b[id][k]) {
					fields = append(fields, k)
				}
			}
			sort.Strings(fields)
			for _, k := range fields {
				kind := "field_change"
				if k == "pitch" {
					kind = "pitch_change"
				}
				out = append(out, Object{"type": kind, "entity_id": id, "path": "/" + k, "before": a[id][k], "after": b[id][k]})
			}
		}
	}
	return Object{"created_ids": created, "changed_ids": changed, "deleted_ids": deleted, "changes": out}
}
func read(tool string, a Object, sid, rev string, s *Score, g Graph) (Object, error) {
	ids, er := selectIDs(g, s.Root, a["selector"])
	if er != nil {
		return nil, er
	}
	base := Object{"score_id": sid, "revision_id": rev}
	switch tool {
	case "inspect":
		limit := int64(200)
		if v, ok := a["limit"]; ok {
			var valid bool
			limit, valid = integer(v)
			if !valid || limit < 1 || limit > 1000 {
				return nil, invalid("limit must be 1..1000")
			}
		}
		projection := obj(a["projection"])
		if er := fields(projection, "fields include_relations"); er != nil {
			return nil, er
		}
		signature := clone(a)
		delete(signature, "cursor")
		signature["revision_id"] = rev
		b, _ := json.Marshal(signature)
		hash := fmt.Sprintf("%x", sha256.Sum256(b))
		offset := int64(0)
		if cursor := str(a["cursor"]); cursor != "" {
			raw, er := base64.RawURLEncoding.DecodeString(cursor)
			if er != nil {
				return nil, invalid("invalid cursor")
			}
			var c struct {
				Hash   string
				Offset int64
			}
			if json.Unmarshal(raw, &c) != nil || c.Hash != hash || c.Offset < 0 || c.Offset > int64(len(ids)) {
				return nil, invalid("cursor does not match revision and query")
			}
			offset = c.Offset
		}
		stop := min(int64(len(ids)), offset+limit)
		items, contextItems := []any{}, []any{}
		project := func(e Object) Object {
			if len(arr(projection["fields"])) == 0 {
				return clone(e)
			}
			m := Object{"id": e["id"], "kind": e["kind"]}
			for _, v := range arr(projection["fields"]) {
				if x, ok := e[str(v)]; ok {
					m[str(v)] = clone(x)
				}
			}
			return m
		}
		selected := map[string]bool{}
		for _, id := range ids[offset:stop] {
			selected[id] = true
			items = append(items, project(g[id]))
		}
		extra := map[string]bool{}
		for _, rel := range arr(projection["include_relations"]) {
			if rel != "annotations" && rel != "spanners" && rel != "layout" {
				return nil, invalid("include_relations supports annotations, spanners, layout")
			}
			for id, e := range g {
				kind := str(e["kind"])
				if kind+"s" != rel && kind != rel {
					continue
				}
				for target := range selected {
					if anchored(e, target) {
						extra[id] = true
					}
				}
			}
		}
		if c := obj(a["context"]); c != nil {
			if er := fields(c, "before after"); er != nil {
				return nil, er
			}
			before, after := any(rat(0, 1)), any(rat(0, 1))
			if c["before"] != nil {
				before = c["before"]
			}
			if c["after"] != nil {
				after = c["after"]
			}
			for _, v := range []any{before, after} {
				r, er := rational(v)
				if er != nil || r.Sign() < 0 {
					return nil, invalid("context values must be nonnegative rationals")
				}
			}
			for target := range selected {
				n := g[target]
				if !timed(n) {
					continue
				}
				start, _ := calc(onset(n), before, '-')
				stop, _ := calc(end(n), after, '+')
				for id, e := range g {
					if timed(e) && e["voice_id"] == n["voice_id"] && cmp(onset(e), stop) < 0 && cmp(end(e), start) > 0 {
						extra[id] = true
					}
				}
			}
		}
		for _, id := range ordered(g, s.Root) {
			if extra[id] && !selected[id] {
				contextItems = append(contextItems, project(g[id]))
			}
		}
		if len(contextItems) > 1000 {
			return nil, fail("LIMIT_EXCEEDED", "context exceeds 1000 entities; narrow context")
		}
		base["items"], base["context_items"], base["next_cursor"] = items, contextItems, nil
		if stop < int64(len(ids)) {
			b, _ := json.Marshal(struct {
				Hash   string
				Offset int64
			}{hash, stop})
			base["next_cursor"] = base64.RawURLEncoding.EncodeToString(b)
		}
		return base, nil
	case "validate":
		if text(a, "profile", "default") != "default" {
			return nil, fail("UNSUPPORTED_OPERATION", "only default validation profile is supported")
		}
		levels := arr(a["levels"])
		if levels == nil {
			levels = []any{"structural", "notation", "performance"}
		}
		for _, l := range levels {
			if l != "structural" && l != "notation" && l != "performance" {
				return nil, invalid("unknown validation level")
			}
		}
		ds := diagnostics(g, s.Root, levels)
		if a["selector"] != nil {
			scope := map[string]bool{}
			for _, id := range ids {
				scope[id] = true
				descendants(g, id, scope)
			}
			filtered := []Object{}
			for _, d := range ds {
				for _, id := range d["entity_ids"].([]string) {
					if scope[id] {
						filtered = append(filtered, d)
						break
					}
				}
			}
			ds = filtered
		}
		base["diagnostics"] = ds
		return base, nil
	case "render":
		if str(a["format"]) != "scoretex-json" {
			return nil, fail("UNSUPPORTED_FORMAT", "supported format: scoretex-json")
		}
		if len(obj(a["options"])) != 0 {
			return nil, invalid("scoretex-json accepts no render options")
		}
		items := []any{}
		for _, id := range ids {
			items = append(items, g[id])
		}
		doc := Object{"specification": "v1alpha1", "score_id": sid, "revision_id": rev, "root_id": s.Root, "entities": items, "selection": a["selector"] != nil}
		b, _ := json.Marshal(doc)
		if len(b) > MaxPayload {
			return nil, fail("LIMIT_EXCEEDED", "render exceeds 4 MiB; select a smaller region")
		}
		base["format"], base["media_type"], base["text"] = "scoretex-json", "application/json", string(b)
		if a["include_diagnostics"] == true {
			base["diagnostics"] = diagnostics(g, s.Root, []any{"notation", "performance"})
		}
		return base, nil
	case "diff":
		ga, gb := s.Revisions[str(a["revision_a"])], s.Revisions[str(a["revision_b"])]
		if ga == nil || gb == nil {
			return nil, fail("REVISION_NOT_FOUND", "both diff revisions must exist in this score")
		}
		granularity := text(a, "granularity", "musical")
		if granularity != "musical" && granularity != "entity" {
			return nil, invalid("granularity must be entity or musical")
		}
		if a["selector"] != nil {
			ia, er := selectIDs(ga, s.Root, a["selector"])
			if er != nil {
				return nil, er
			}
			ib, er := selectIDs(gb, s.Root, a["selector"])
			if er != nil {
				return nil, er
			}
			scope := map[string]bool{}
			for _, id := range append(ia, ib...) {
				scope[id] = true
			}
			aa, bb := Graph{}, Graph{}
			for id := range scope {
				if ga[id] != nil {
					aa[id] = ga[id]
				}
				if gb[id] != nil {
					bb[id] = gb[id]
				}
			}
			ga, gb = aa, bb
		}
		cs := changes(ga, gb)
		base["revision_a"], base["revision_b"], base["changes"], base["summary"] = a["revision_a"], a["revision_b"], cs["changes"], fmt.Sprintf("Created %d, changed %d, deleted %d entities.", len(arr(cs["created_ids"])), len(arr(cs["changed_ids"])), len(arr(cs["deleted_ids"])))
		b, _ := json.Marshal(base)
		if len(b) > MaxPayload {
			return nil, fail("LIMIT_EXCEEDED", "diff exceeds 4 MiB; use a selector")
		}
		return base, nil
	}
	return nil, invalid("unknown read tool")
}
