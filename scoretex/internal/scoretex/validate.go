package scoretex

import "strings"

func entityValues(e Object, g Graph) error {
	if (e["kind"] == "measure" || e["kind"] == "directive") && len(arr(e["scope_ids"])) == 0 {
		return invalid("measure and directive require nonempty scope_ids")
	}
	if timed(e) {
		if _, er := calc(e["onset"], e["duration"], '+'); er != nil {
			return er
		}
	}
	for _, k := range []string{"name", "short_name", "number", "directive_type", "annotation_type", "spanner_type", "group_type", "layout_type"} {
		if v, ok := e[k]; ok {
			if _, ok := v.(string); !ok {
				return invalid(k + " must be a string")
			}
		}
	}
	for _, k := range []string{"index", "lines"} {
		if v, ok := e[k]; ok {
			n, ok := integer(v)
			if !ok || n < 0 {
				return invalid(k + " must be a nonnegative integer")
			}
		}
	}
	if v, ok := e["metadata"]; ok && v != nil && obj(v) == nil {
		return invalid("metadata must be an object")
	}
	if n, ok := e["notation"]; ok {
		m := obj(n)
		if m == nil {
			return invalid("notation must be an object")
		}
		if er := fields(m, "base dots time_modification notehead fingering"); er != nil {
			return er
		}
		if v, ok := m["base"]; ok {
			r, er := rational(v)
			if er != nil || r.Sign() <= 0 {
				return invalid("notation base must be positive rational")
			}
		}
		if v, ok := m["dots"]; ok {
			n, ok := integer(v)
			if !ok || n < 0 || n > 8 {
				return invalid("dots must be 0..8")
			}
		}
		if v, ok := m["time_modification"]; ok {
			tm := obj(v)
			if er := fields(tm, "actual normal"); er != nil {
				return er
			}
			for _, k := range []string{"actual", "normal"} {
				n, ok := integer(tm[k])
				if !ok || n <= 0 {
					return invalid("time modification actual and normal must be positive integers")
				}
			}
		}
	}
	if e["kind"] == "instrument" {
		if v, ok := e["transposition"]; ok {
			if _, er := rational(v); er != nil {
				return er
			}
		}
		for _, k := range []string{"written_range", "sounding_range"} {
			if v, ok := e[k]; ok {
				r := obj(v)
				if er := fields(r, "low high"); er != nil {
					return er
				}
				low, er := pitchNumber(obj(r["low"]))
				if er != nil {
					return er
				}
				high, er := pitchNumber(obj(r["high"]))
				if er != nil {
					return er
				}
				if low.Cmp(high) > 0 {
					return invalid("range low must not exceed high")
				}
			}
		}
	}
	if e["kind"] == "directive" {
		v := obj(e["value"])
		switch e["directive_type"] {
		case "time_signature":
			if er := fields(v, "numerator denominator"); er != nil {
				return er
			}
			for _, k := range []string{"numerator", "denominator"} {
				n, ok := integer(v[k])
				if !ok || n <= 0 {
					return invalid("time signature requires positive integer numerator and denominator")
				}
			}
		case "tempo":
			if er := fields(v, "bpm beat_unit"); er != nil {
				return er
			}
			n, ok := integer(v["bpm"])
			if !ok || n <= 0 {
				return invalid("tempo bpm must be a positive integer")
			}
			r, er := rational(v["beat_unit"])
			if er != nil || r.Sign() <= 0 {
				return invalid("tempo beat_unit must be a positive rational")
			}
		case "instrument_change":
			if er := fields(v, "instrument_id"); er != nil {
				return er
			}
			target := g[str(v["instrument_id"])]
			if target == nil || target["kind"] != "instrument" {
				return fail("STRUCTURAL_VIOLATION", "instrument change requires an existing instrument")
			}
		case "clef":
			if er := fields(v, "sign line octave_change"); er != nil {
				return er
			}
			if str(v["sign"]) == "" {
				return invalid("clef requires sign")
			}
		case "key_signature":
			if er := fields(v, "fifths mode"); er != nil {
				return er
			}
			if _, ok := integer(v["fifths"]); !ok {
				return invalid("key signature requires integer fifths")
			}
		case "barline", "rehearsal_mark", "navigation_mark", "staff_configuration", "transposition_state":
			return fail("UNSUPPORTED_OPERATION", "directive type is outside the supported notation subset")
		default:
			if !strings.HasPrefix(str(e["directive_type"]), "x-") {
				return fail("UNSUPPORTED_OPERATION", "unsupported directive_type")
			}
		}
	}
	return nil
}
