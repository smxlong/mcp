package scoretex

import (
	"math/big"
	"strings"
)

var naturals = []int64{0, 2, 4, 5, 7, 9, 11}

func pitchNumber(p Object) (*big.Rat, error) {
	if er := pitch(p); er != nil {
		return nil, er
	}
	oct, _ := integer(p["octave"])
	if oct < -1000 || oct > 1000 {
		return nil, invalid("pitch octave outside supported range -1000..1000")
	}
	n := naturals[strings.Index("CDEFGAB", str(p["step"]))] + 12*(oct+1)
	a, _ := rational(p["alter"])
	return a.Add(a, new(big.Rat).SetInt64(n)), nil
}
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b < 0 {
		q--
	}
	return q
}
func spelled(number *big.Rat, policy string) (Object, error) {
	if policy != "sharps" && policy != "flats" {
		return nil, invalid("spelling_policy must be sharps or flats")
	}
	q := new(big.Int).Quo(number.Num(), number.Denom())
	if number.Sign() < 0 && !number.IsInt() {
		q.Sub(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return nil, fail("LIMIT_EXCEEDED", "pitch exceeds range")
	}
	n := q.Int64()
	oct := floorDiv(n, 12) - 1
	pc := n - 12*(oct+1)
	steps := "CCDDEFFGGAAB"
	if policy == "flats" {
		steps = "CDDEEFGGAABB"
	}
	step := string(steps[pc])
	base := 12*(oct+1) + naturals[strings.Index("CDEFGAB", step)]
	alter, er := encoded(new(big.Rat).Sub(number, new(big.Rat).SetInt64(base)))
	return Object{"step": step, "octave": oct, "alter": alter}, er
}
func semitones(v any) (*big.Rat, error) {
	if n, ok := integer(v); ok {
		return new(big.Rat).SetInt64(n), nil
	}
	return rational(v)
}
func (t *transaction) transform(ids []string, operation string, p Object) error {
	allowed := map[string]string{"transpose": "chromatic_semitones diatonic_steps spelling_policy", "invert": "axis space spelling_policy", "retrograde": "start end", "augment_duration": "factor", "diminish_duration": "factor", "shift_time": "offset", "quantize": "grid rounding durations", "respell": "spelling_policy", "revoice": "mapping", "change_instrument": "instrument_id time pitch_policy"}
	keys, ok := allowed[operation]
	if !ok {
		return fail("UNSUPPORTED_OPERATION", "unknown transform: "+operation)
	}
	if er := fields(p, keys); er != nil {
		return er
	}
	targets := append([]string{}, ids...)
	seen := map[string]bool{}
	for _, id := range ids {
		for _, tone := range arr(t.g[id]["tone_ids"]) {
			targets = append(targets, str(tone))
		}
	}
	count := 0
	if operation == "change_instrument" {
		if len(ids) != 1 || t.g[ids[0]]["kind"] != "part" {
			return fail("SELECTION_AMBIGUOUS", "change_instrument requires exactly one part")
		}
		inst := str(p["instrument_id"])
		if t.g[inst] == nil || t.g[inst]["kind"] != "instrument" {
			return fail("ENTITY_NOT_FOUND", "instrument does not exist")
		}
		if p["pitch_policy"] != "preserve_written" && p["pitch_policy"] != "preserve_sounding" {
			return invalid("pitch_policy must be preserve_written or preserve_sounding")
		}
		if _, er := rational(p["time"]); er != nil {
			return er
		}
		part := t.g[ids[0]]
		found := false
		for _, v := range arr(part["instrument_ids"]) {
			found = found || v == inst
		}
		if !found {
			return invalid("instrument must belong to selected part")
		}
		if p["pitch_policy"] == "preserve_sounding" {
			members := map[string]bool{}
			descendants(t.g, ids[0], members)
			newTrans := t.g[inst]["transposition"]
			if newTrans == nil {
				newTrans = rat(0, 1)
			}
			for id := range members {
				event := t.g[id]
				if !timed(event) || cmp(onset(event), p["time"]) < 0 {
					continue
				}
				active := str(arr(part["instrument_ids"])[0])
				latest := any(rat(0, 1))
				for _, did := range ordered(t.g, t.root) {
					d := t.g[did]
					if d["kind"] != "directive" || d["directive_type"] != "instrument_change" {
						continue
					}
					scoped := false
					for _, scope := range arr(d["scope_ids"]) {
						scoped = scoped || scope == ids[0]
					}
					if scoped && cmp(onset(d), onset(event)) <= 0 && cmp(onset(d), latest) >= 0 {
						active = str(obj(d["value"])["instrument_id"])
						latest = onset(d)
					}
				}
				// Later explicit changes remain active, so only rewrite the replaced interval.
				if cmp(latest, p["time"]) > 0 {
					continue
				}
				oldTrans := t.g[active]["transposition"]
				if oldTrans == nil {
					oldTrans = rat(0, 1)
				}
				delta, er := calc(oldTrans, newTrans, '-')
				if er != nil {
					return er
				}
				pitches := []Object{event}
				for _, tone := range arr(event["tone_ids"]) {
					pitches = append(pitches, t.g[str(tone)])
				}
				for _, pitched := range pitches {
					if pitched["pitch"] == nil {
						continue
					}
					pitch := obj(pitched["pitch"])
					alter, er := calc(pitch["alter"], delta, '+')
					if er != nil {
						return er
					}
					pitch["alter"] = alter
				}
			}
		}
		_, er := t.add(Object{"kind": "directive", "directive_type": "instrument_change", "scope_ids": []any{ids[0]}, "onset": p["time"], "value": Object{"instrument_id": inst}})
		return er
	}
	for _, id := range targets {
		if seen[id] {
			continue
		}
		seen[id] = true
		e := t.g[id]
		switch operation {
		case "transpose", "invert", "respell":
			if e["pitch"] == nil {
				continue
			}
			n, er := pitchNumber(obj(e["pitch"]))
			if er != nil {
				return er
			}
			old := obj(e["pitch"])
			policy := str(p["spelling_policy"])
			if operation == "transpose" {
				delta, er := semitones(p["chromatic_semitones"])
				if er != nil {
					return invalid("transpose requires chromatic_semitones")
				}
				n.Add(n, delta)
				if policy == "preserve_interval_spelling" {
					steps, ok := integer(p["diatonic_steps"])
					if !ok || steps < -10000 || steps > 10000 {
						return invalid("interval spelling requires bounded diatonic_steps")
					}
					oct, _ := integer(old["octave"])
					diat := oct*7 + int64(strings.Index("CDEFGAB", str(old["step"]))) + steps
					oct = floorDiv(diat, 7)
					idx := diat - oct*7
					alter, er := encoded(new(big.Rat).Sub(n, new(big.Rat).SetInt64(12*(oct+1)+naturals[idx])))
					if er != nil {
						return er
					}
					e["pitch"] = Object{"step": string("CDEFGAB"[idx]), "octave": oct, "alter": alter}
					count++
					continue
				}
			}
			if operation == "invert" {
				if p["space"] != "chromatic" {
					return fail("UNSUPPORTED_OPERATION", "invert supports explicit chromatic space")
				}
				axis, er := pitchNumber(obj(p["axis"]))
				if er != nil {
					return er
				}
				n.Sub(axis.Mul(axis, big.NewRat(2, 1)), n)
			}
			e["pitch"], er = spelled(n, policy)
			if er != nil {
				return er
			}
			count++
		case "augment_duration", "diminish_duration":
			if !timed(e) {
				continue
			}
			factor, er := rational(p["factor"])
			if er != nil {
				return er
			}
			c := factor.Cmp(big.NewRat(1, 1))
			if factor.Sign() <= 0 || (operation == "augment_duration" && c <= 0) || (operation == "diminish_duration" && c >= 0) {
				return invalid("augmentation factor must exceed 1; diminution factor must lie between 0 and 1")
			}
			e["duration"], er = calc(e["duration"], p["factor"], '*')
			if er != nil {
				return er
			}
			count++
		case "shift_time":
			k := "onset"
			if e[k] == nil {
				k = "start"
			}
			if e[k] == nil {
				continue
			}
			v, er := calc(e[k], p["offset"], '+')
			if er != nil {
				return er
			}
			e[k] = v
			count++
		case "retrograde":
			if !timed(e) {
				continue
			}
			if _, er := rational(p["start"]); er != nil {
				return er
			}
			if _, er := rational(p["end"]); er != nil {
				return er
			}
			if cmp(p["start"], p["end"]) >= 0 || cmp(onset(e), p["start"]) < 0 || cmp(end(e), p["end"]) > 0 {
				return invalid("retrograde requires an explicit range containing every event")
			}
			sum, er := calc(p["start"], p["end"], '+')
			if er != nil {
				return er
			}
			e["onset"], er = calc(sum, end(e), '-')
			if er != nil {
				return er
			}
			count++
		case "quantize":
			if !timed(e) {
				continue
			}
			grid, er := rational(p["grid"])
			if er != nil || grid.Sign() <= 0 {
				return invalid("quantize requires a positive rational grid")
			}
			round := str(p["rounding"])
			if round != "floor" && round != "ceil" && round != "nearest" {
				return invalid("rounding must be floor, ceil or nearest (ties forward)")
			}
			keys := []string{"onset"}
			if p["durations"] == true {
				keys = append(keys, "duration")
			}
			for _, k := range keys {
				v, _ := rational(e[k])
				v.Quo(v, grid)
				q, r := new(big.Int), new(big.Int)
				q.QuoRem(v.Num(), v.Denom(), r)
				if round == "ceil" && r.Sign() > 0 || round == "nearest" && new(big.Int).Mul(r, big.NewInt(2)).Cmp(v.Denom()) >= 0 {
					q.Add(q, big.NewInt(1))
				}
				e[k], er = encoded(new(big.Rat).Mul(new(big.Rat).SetInt(q), grid))
				if er != nil {
					return er
				}
			}
			count++
		case "revoice":
			if !timed(e) {
				continue
			}
			mapping := obj(p["mapping"])
			dest := str(mapping[str(e["voice_id"])])
			if dest == "" {
				return invalid("mapping must specify every selected source voice")
			}
			e["voice_id"] = dest
			count++
		}
	}
	if count == 0 {
		return fail("SELECTION_EMPTY", "selection contains no entities eligible for transform")
	}
	return nil
}
