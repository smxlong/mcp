// Package scoretex implements the ScoreTex v1alpha1 semantic engine.
package scoretex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

type Object = map[string]any
type Graph map[string]Object

type Fault struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Retryable   bool     `json:"retryable"`
	Path        string   `json:"path,omitempty"`
	Details     Object   `json:"details"`
	Diagnostics []Object `json:"diagnostics"`
}

func (e *Fault) Error() string { return e.Message }
func fail(code, message string) error {
	return &Fault{Code: code, Message: message, Details: Object{}, Diagnostics: []Object{}}
}
func invalid(message string) error { return fail("INVALID_ARGUMENT", message) }
func obj(v any) Object             { m, _ := v.(map[string]any); return m }
func str(v any) string             { s, _ := v.(string); return s }
func arr(v any) []any              { a, _ := v.([]any); return a }
func text(m Object, k, def string) string {
	if v, ok := m[k]; ok {
		return str(v)
	}
	return def
}
func clone[T any](v T) T {
	b, _ := json.Marshal(v)
	var out T
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	_ = d.Decode(&out)
	return out
}
func integer(v any) (int64, bool) {
	switch n := v.(type) {
	case json.Number:
		x, e := n.Int64()
		return x, e == nil
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), n == float64(int64(n))
	}
	return 0, false
}
func rational(v any) (*big.Rat, error) {
	m := obj(v)
	if len(m) != 2 {
		return nil, invalid("rational requires integer n and positive integer d")
	}
	n, ok := integer(m["n"])
	d, ok2 := integer(m["d"])
	if !ok || !ok2 || d <= 0 {
		return nil, invalid("rational requires integer n and positive integer d")
	}
	return new(big.Rat).SetFrac(big.NewInt(n), big.NewInt(d)), nil
}
func rat(n, d int64) Object { return Object{"n": n, "d": d} }
func encoded(r *big.Rat) (Object, error) {
	if !r.Num().IsInt64() || !r.Denom().IsInt64() {
		return nil, fail("LIMIT_EXCEEDED", "rational exceeds signed 64-bit representation")
	}
	return rat(r.Num().Int64(), r.Denom().Int64()), nil
}
func calc(a, b any, op byte) (Object, error) {
	x, e := rational(a)
	if e != nil {
		return nil, e
	}
	y, e := rational(b)
	if e != nil {
		return nil, e
	}
	switch op {
	case '+':
		x.Add(x, y)
	case '-':
		x.Sub(x, y)
	case '*':
		x.Mul(x, y)
	}
	return encoded(x)
}
func cmp(a, b any) int {
	x, e := rational(a)
	y, f := rational(b)
	if e != nil || f != nil {
		return 0
	}
	return x.Cmp(y)
}
func normalize(v any) error {
	switch x := v.(type) {
	case map[string]any:
		if _, ok := x["n"]; ok {
			if _, ok := x["d"]; ok {
				r, e := rational(x)
				if e != nil {
					return e
				}
				m, e := encoded(r)
				if e != nil {
					return e
				}
				x["n"], x["d"] = m["n"], m["d"]
				return nil
			}
		}
		for k, v := range x {
			if strings.HasPrefix(k, "x-") {
				continue
			}
			if e := normalize(v); e != nil {
				return e
			}
		}
	case []any:
		for _, v := range x {
			if e := normalize(v); e != nil {
				return e
			}
		}
	}
	return nil
}
func fields(m Object, allowed string) error {
	for k := range m {
		if !strings.HasPrefix(k, "x-") && !strings.Contains(" "+allowed+" ", " "+k+" ") {
			return invalid("unknown field: " + k)
		}
	}
	return nil
}
func equal(a, b any) bool { x, _ := json.Marshal(a); y, _ := json.Marshal(b); return bytes.Equal(x, y) }
func pitch(v any) error {
	p := obj(v)
	if e := fields(p, "step octave alter"); e != nil {
		return e
	}
	if len(str(p["step"])) != 1 || !strings.Contains("CDEFGAB", str(p["step"])) {
		return invalid("pitch.step must be C D E F G A or B")
	}
	if _, ok := integer(p["octave"]); !ok {
		return invalid("pitch.octave must be an integer")
	}
	_, e := rational(p["alter"])
	return e
}
func required(m Object, keys string) error {
	for _, k := range strings.Fields(keys) {
		if _, ok := m[k]; !ok {
			return invalid(fmt.Sprintf("%s requires %s", text(m, "kind", "object"), k))
		}
	}
	return nil
}
func timed(e Object) bool { return e["onset"] != nil && e["duration"] != nil }
func onset(e Object) any {
	if e["onset"] != nil {
		return e["onset"]
	}
	if e["start"] != nil {
		return e["start"]
	}
	return rat(0, 1)
}
func end(e Object) any {
	if e["duration"] == nil {
		return onset(e)
	}
	v, _ := calc(onset(e), e["duration"], '+')
	return v
}
func overlap(a, b Object) bool { return cmp(onset(a), end(b)) < 0 && cmp(onset(b), end(a)) < 0 }
