# ScoreTex

A Go MCP server implementing the ten ScoreTex v1alpha1 tools over a canonical
score graph. It uses the official MCP Go SDK and provides stdio and Streamable
HTTP transports. The semantic engine is independent of the transport.

## Build, run, and validate

```sh
cd scoretex
make build
./bin/scoretex                         # stdio, process-local memory
./bin/scoretex -state ./scores.json    # atomic persistent state
./bin/scoretex -http 127.0.0.1:8080 -state ./scores.json
make validate                         # executable demonstration, Python 3 required
make test-docker                      # tests with race detector, vet, demonstration
make lint
make image
```

`make validate` builds the server and runs `scripts/validate.py`. The script
creates an isolated temporary state file, speaks actual MCP JSON-RPC over stdio,
asserts the results, restarts the process, checks persistence, and removes its
state. It covers all ten tools and transformations, all patch operations,
selector operators, pagination, context, relationships, collision policies,
replacement and deletion modes, historical reads, idempotency, failed edit
rollback, validation modes, deterministic rendering, and semantic diffs.
It exits nonzero on failure. Run it against another build with
`python3 scripts/validate.py --binary /path/to/scoretex`.

For an MCP client, configure the absolute binary path as `command` and optionally
`["-state", "/absolute/path/scores.json"]` as `args`. `SCORETEX_STATE` supplies the
default state path. HTTP exposes the MCP handler at `/`; deploy within a trusted
host environment or behind authentication. All sessions in one process share
one store. Only one process should write a given state file.

## Semantic contract

Tool names match the specification exactly:

| Tool | Purpose |
|---|---|
| `score.create` | Root, parts, instruments, staves, voices, initial defaults |
| `score.inspect` | Selective reads, projection, context, pagination |
| `score.insert` | Explicit location and collision policy |
| `score.replace` | One-to-one objects or a single-voice region |
| `score.delete` | Remove, rest replacement, fill measure, close time |
| `score.transform` | Explicit musical transformations |
| `score.patch` | Ordered atomic general edits |
| `score.validate` | Structural, notation, performance diagnostics |
| `score.render` | Deterministic `scoretex-json` serialization |
| `score.diff` | Entity changes and musical pitch changes |

Time and alterations use `{ "n": 1, "d": 4 }`, in whole-note units. Fractions
normalize at API boundaries. Arithmetic uses arbitrary precision intermediates;
canonical numerators and denominators must fit signed 64-bit integers.
Written pitch uses `step`, integer `octave`, and rational `alter`.

All edits require `score_id` and current `base_revision_id`. Mutations execute
against an isolated graph and commit exactly one revision after validation.
Failed edits and failed disk writes leave the current revision unchanged.
Concurrent edits from the same base produce one success and revision conflicts
for the others. Calls have no arrival-order guarantee; await dependent edits.

IDs are opaque. Explicit client IDs are accepted on entity creation, but deleted
IDs are never reused. Ordered ownership arrays and stored creation order govern
semantic ordering, not ID spelling. Prior revisions remain immutable.

Idempotency keys are global to the store and retained for its entire lifetime,
including restarts with `-state`. Equal normalized requests replay the original
result. Rational forms and the omitted/default structural validation mode are
normalized; other omitted defaults and explicit values are distinct requests.
Use a new key for a new intent. Memory mode lasts until process exit.

## Supported notation subset

The graph supports score, part, instrument, staff, voice, measure, pitched note,
chord with at least two owned tones, rest, directive, annotation, spanner, group,
and layout entities. Notes/rests/chords have positive duration and a valid voice.
Chord tones carry pitch and inherit timing from the chord. Grace events,
unpitched events, nested content shorthand, external import, and extension entity
kinds are outside this release's subset and are rejected.

Directives support time signatures (`numerator`, `denominator`), tempo (integer
`bpm`, rational `beat_unit`), clef (`sign`, optional `line`, `octave_change`), key
signature (`fifths`, optional `mode`), and instrument changes (`instrument_id`).
Defaults become score-scoped directives. Measures may have different staff
scopes. Annotation and layout anchors use `{entity_id}` or `{time, scope_id}`;
spanners have `start_anchor` and `end_anchor`. Tie pitches must match. Groups
have `group_type` and `member_ids`. Group membership does not own its members.

Notation supports `base`, `dots`, `time_modification: {actual, normal}`,
`notehead`, and `fingering`. Namespaced `x-` fields are preserved on unchanged
entities. Unknown unnamespaced entity/request fields are rejected. Metadata and
annotation/layout values carry user-defined content. Namespaced directive types
are preserved without interpretation.

Only `scoretex-json` rendering is supported. It includes all canonical fields and
stable IDs. A selected render is marked `selection: true` and may reference
entities outside the selection. MusicXML, MIDI, SVG, audio, and external imports
return `UNSUPPORTED_FORMAT`. There is no engraving or playback claim.

## Editing details

`insert.at` accepts `voice_id` with `time` or `measure_id`/`offset`, `staff_id`
for directives, `owner_id`/`relation` for structural children, or `anchor` for
annotations/layout. Timed content is consecutive unless explicit onsets are
provided. `overlay` explicitly permits overlapping independent events;
`shift_forward` requires voice and time and rejects insertion through an event.

Object replacement requires one object per selected entity. `preserve` accepts
`onset`, `duration`, `voice`, `annotations`, `layout`. Replacement creates new
identities. Region replacement requires one voice and exactly the selected
span's duration. Relation policy defaults to `reject`; `reattach` needs a
compatible one-to-one mapping, and `cascade` removes dependent relations.
Owned children are removed with an owner. `preserve_measure_duration` fills gaps
only in affected measures and voices. `close_time` rejects unselected events
crossing the removed span and resulting collisions.

Patch objects use these forms:

```json
{"op":"test", "id":"note", "path":"/pitch/step", "value":"C"}
{"op":"set", "id":"note", "path":"/pitch/step", "value":"D"}
{"op":"unset", "id":"note", "path":"/x-color"}
{"op":"create", "entity":{"kind":"voice","staff_id":"staff","index":1}, "owner_id":"staff"}
{"op":"move", "id":"note", "to":{"voice_id":"other_voice","onset":{"n":1,"d":1}}}
{"op":"link", "id":"group", "relation":"member_ids", "target_id":"note"}
{"op":"unlink", "id":"group", "relation":"member_ids", "target_id":"note"}
{"op":"delete", "id":"note", "relation_policy":"cascade"}
```

JSON Pointer paths address object fields. Replace arrays as whole fields.
`id`, `kind`, and creation `order` are immutable. Structural movement uses
`to.owner_id` with optional `relation`; direct backlink edits must leave ownership
arrays consistent at commit. Patch is expressive enough to reproduce every
high-level edit, including relationship rewrites.

| Transform | Parameters |
|---|---|
| `transpose` | `chromatic_semitones` (integer or rational), `spelling_policy`: `sharps`, `flats`, or `preserve_interval_spelling` with integer `diatonic_steps` |
| `invert` | `axis` pitch, `space: "chromatic"`, `spelling_policy`: `sharps` or `flats` |
| `respell` | `spelling_policy`: `sharps` or `flats`, preserving pitch |
| `retrograde` | Rational `start`, `end` containing every selected event |
| `augment_duration` | Rational `factor > 1` |
| `diminish_duration` | Rational `0 < factor < 1` |
| `shift_time` | Signed rational `offset` |
| `quantize` | Positive rational `grid`, `rounding`: `floor`, `ceil`, `nearest`; optional `durations` boolean; nearest ties round forward |
| `revoice` | `mapping` from source voice IDs to destination voice IDs |
| `change_instrument` | Exactly one selected part, its `instrument_id`, rational `time`, `pitch_policy`: `preserve_written` or `preserve_sounding` |

Retrograde reverses placement and preserves duration. Entity-anchored annotations
follow their entities; absolute position anchors remain fixed and spanner
endpoints are not reversed. Duration transforms change duration only. Pitch
transforms expand selected chords to their tones. Instrument transposition is a rational semitone offset. Instrument changes
preserving sounding pitch adjust written alterations until the next explicit
instrument change; written step and octave spelling are retained.

## Inspection, validation, and limits

Selectors support IDs, kind, structural/group descendants (`within`/`match`),
`all`/`any`/`not`, half-open time relations (`onset_in`, `contained`, `overlap`,
`cover`), and field predicates (`eq`, `ne`, `lt`, `lte`, `gt`, `gte`, `in`,
`exists`). Dotted field paths and JSON Pointer object paths are accepted.

Projection supports top-level `fields` and related `annotations`, `spanners`,
`layout`. Context expands time within selected voices. Extra entities appear in
`context_items`, never as selected `items`. Pin `revision_id` while following
`next_cursor`; a cursor is bound to the revision and query arguments.

The `default` validation profile enforces graph structure and reference integrity.
Notation diagnostics cover underfilled/overfilled measures and events crossing
measure boundaries. Performance diagnostics identify fractional pitch alterations
requiring compatible playback. It does not implement comprehensive tuplet,
repeat, instrument-range, engraving, or execution validation. `structural` is
the default mutation mode; `standard` and `strict` also reject every error from
this profile. Underfilling and microtonal playback messages are warnings.

Limits: 100 scores per store; 256 revisions per score; 10,000 entities per score;
1,000 operations/content entities per call; 4 MiB encoded tool arguments,
render or diff; 1,000 inspected items per page (default 200); 1,000 context
entities; selector depth 32; at most 32 initial staves per part and 32 voices per
staff. Exceeded limits return structured errors, never silent truncation.
Revision limits do not evict history or idempotency records.

State is written as a private file through a temporary file, sync, and atomic
rename. The configured parent directory must already exist. No tool accepts
filesystem paths, shell commands, or executable score content.
