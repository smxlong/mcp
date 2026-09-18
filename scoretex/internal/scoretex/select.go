package scoretex

import (
	"fmt"
	"strings"
)

func field(v any, path string) (any, bool) {
	var parts []string
	if strings.HasPrefix(path, "/") {
		parts = strings.Split(path[1:], "/")
		for i, k := range parts {
			parts[i] = strings.ReplaceAll(strings.ReplaceAll(k, "~1", "/"), "~0", "~")
		}
	} else {
		parts = strings.Split(path, ".")
	}
	for _, k := range parts {
		m := obj(v)
		var ok bool
		v, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	return v, true
}
func selectIDs(g Graph, root string, selector any) ([]string, error) {
	var validate func(any, int) error
	validate = func(v any, depth int) error {
		if v == nil {
			return nil
		}
		if depth > 32 {
			return fail("LIMIT_EXCEEDED", "selector nesting exceeds 32")
		}
		s := obj(v)
		if s == nil {
			return fail("INVALID_SELECTOR", "selector must be an object")
		}
		if e := fields(s, "ids kind within match time where all any not"); e != nil {
			return fail("INVALID_SELECTOR", e.Error())
		}
		for _, k := range []string{"all", "any"} {
			if v, ok := s[k]; ok {
				if arr(v) == nil {
					return fail("INVALID_SELECTOR", k+" must be an array")
				}
				for _, c := range arr(v) {
					if e := validate(c, depth+1); e != nil {
						return e
					}
				}
			}
		}
		for _, k := range []string{"within", "match", "not"} {
			if v, ok := s[k]; ok {
				if e := validate(v, depth+1); e != nil {
					return e
				}
			}
		}
		if v, ok := s["ids"]; ok {
			if arr(v) == nil {
				return fail("INVALID_SELECTOR", "ids must be an array")
			}
			for _, id := range arr(v) {
				if str(id) == "" {
					return fail("INVALID_SELECTOR", "ids must contain strings")
				}
			}
		}
		if v, ok := s["time"]; ok {
			t := obj(v)
			if e := fields(t, "start end relation"); e != nil {
				return e
			}
			if _, e := rational(t["start"]); e != nil {
				return fail("INVALID_SELECTOR", e.Error())
			}
			if _, e := rational(t["end"]); e != nil {
				return fail("INVALID_SELECTOR", e.Error())
			}
			if cmp(t["start"], t["end"]) >= 0 {
				return fail("INVALID_SELECTOR", "time range must have start < end")
			}
			switch text(t, "relation", "onset_in") {
			case "onset_in", "overlap", "contained", "cover":
			default:
				return fail("INVALID_SELECTOR", "unsupported time relation")
			}
		}
		if v, ok := s["where"]; ok {
			w := obj(v)
			if e := fields(w, "field op value"); e != nil {
				return e
			}
			if str(w["field"]) == "" {
				return fail("INVALID_SELECTOR", "where requires field")
			}
			switch str(w["op"]) {
			case "eq", "ne", "lt", "lte", "gt", "gte", "exists":
			case "in":
				if arr(w["value"]) == nil {
					return fail("INVALID_SELECTOR", "in requires an array")
				}
			default:
				return fail("INVALID_SELECTOR", "unsupported predicate operator")
			}
		}
		return nil
	}
	if e := validate(selector, 0); e != nil {
		return nil, e
	}
	var matches func(string, any) bool
	matches = func(id string, v any) bool {
		if v == nil {
			return true
		}
		s := obj(v)
		e := g[id]
		if v, ok := s["ids"]; ok {
			found := false
			for _, x := range arr(v) {
				if x == id {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		if k, ok := s["kind"]; ok && e["kind"] != k {
			return false
		}
		if w, ok := s["within"]; ok {
			found := false
			for rid := range g {
				if matches(rid, w) {
					d := map[string]bool{}
					descendants(g, rid, d)
					if d[id] {
						found = true
						break
					}
				}
			}
			if !found {
				return false
			}
		}
		if w, ok := s["match"]; ok && !matches(id, w) {
			return false
		}
		for _, x := range arr(s["all"]) {
			if !matches(id, x) {
				return false
			}
		}
		if v, ok := s["any"]; ok {
			yes := false
			for _, x := range arr(v) {
				yes = yes || matches(id, x)
			}
			if !yes {
				return false
			}
		}
		if v, ok := s["not"]; ok && matches(id, v) {
			return false
		}
		if t := obj(s["time"]); t != nil {
			e := temporalEntity(g, e)
			if e["onset"] == nil && e["start"] == nil {
				return false
			}
			a, b := onset(e), end(e)
			start, stop := t["start"], t["end"]
			yes := false
			switch text(t, "relation", "onset_in") {
			case "onset_in":
				yes = cmp(a, start) >= 0 && cmp(a, stop) < 0
			case "contained":
				yes = cmp(a, start) >= 0 && cmp(b, stop) <= 0
			case "overlap":
				yes = cmp(a, stop) < 0 && cmp(b, start) > 0
				if cmp(a, b) == 0 {
					yes = cmp(a, start) >= 0 && cmp(a, stop) < 0
				}
			case "cover":
				yes = cmp(a, start) <= 0 && cmp(b, stop) >= 0
			}
			if !yes {
				return false
			}
		}
		if w := obj(s["where"]); w != nil {
			a, exists := field(e, str(w["field"]))
			b := w["value"]
			yes := false
			c := strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
			if x, ok := integer(a); ok {
				if y, ok := integer(b); ok {
					c = 0
					if x < y {
						c = -1
					} else if x > y {
						c = 1
					}
				}
			}
			if obj(a) != nil && obj(b) != nil {
				c = cmp(a, b)
			}
			switch str(w["op"]) {
			case "eq":
				yes = exists && equal(a, b)
			case "ne":
				yes = !equal(a, b)
			case "lt":
				yes = exists && c < 0
			case "lte":
				yes = exists && c <= 0
			case "gt":
				yes = exists && c > 0
			case "gte":
				yes = exists && c >= 0
			case "exists":
				want := true
				if b != nil {
					want, _ = b.(bool)
				}
				yes = exists == want
			case "in":
				for _, v := range arr(b) {
					yes = yes || equal(a, v)
				}
			}
			if !yes {
				return false
			}
		}
		return true
	}
	out := []string{}
	for _, id := range ordered(g, root) {
		if matches(id, selector) {
			out = append(out, id)
		}
	}
	return out, nil
}
