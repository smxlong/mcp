# ScoreTex v1alpha1 Core Tool Specification

**File:** `scoretex_v1alpha1_1.md`
**Specification version:** `v1alpha1`
**Document revision:** `1`
**Status:** Initial technical design
**Date:** 2026-09-17

## 1. Purpose

ScoreTex is a tool set for authoring, inspecting, editing, validating, comparing, and rendering musical scores through an LLM agent.

ScoreTex can run directly inside an agentic loop or behind an MCP adapter. Both forms use the same score model and the same operation semantics.

The v1alpha1 core contains ten tools:

1. `score.create`
2. `score.inspect`
3. `score.insert`
4. `score.replace`
5. `score.delete`
6. `score.transform`
7. `score.patch`
8. `score.validate`
9. `score.render`
10. `score.diff`

These tools form the backbone of ScoreTex workflows. Higher-level actions such as composition, harmonization, orchestration, reduction, reharmonization, and humanization are agent workflows built from these tools. They are not core mutations.

## 2. Scope

This specification defines:

- the canonical ScoreTex score model;
- exact musical time and pitch representation;
- object identity and revision semantics;
- selectors;
- common request and response rules;
- the behavior of each core tool;
- validation and error behavior;
- rendering and interchange expectations;
- MCP and direct-agent integration requirements;
- extension and conformance rules.

This specification does not define:

- a composition model;
- an LLM prompt format;
- an engraving algorithm;
- a synthesis engine;
- a DAW or sequencer interface;
- collaborative merge behavior;
- a graphical editor protocol.

An implementation can provide these features above the core.

## 3. Intended readers

This document is for:

- ScoreTex engine implementers;
- SDK implementers;
- MCP adapter implementers;
- agent authors;
- test authors;
- developers of importers, exporters, renderers, and notation extensions.

The reader is expected to understand common music notation concepts and structured API design.

## 4. Normative language

The words **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** state requirement strength.

- **MUST** and **MUST NOT** define conformance requirements.
- **SHOULD** and **SHOULD NOT** define recommended behavior. An implementation can depart from them when it has a specific reason.
- **MAY** defines optional behavior.

## 5. Design principles

### 5.1 Use one canonical semantic score

ScoreTex MUST operate on a semantic score model. It MUST NOT use SVG coordinates, MusicXML element positions, MIDI ticks, or engraving-system object addresses as canonical score identity.

External formats are inputs and outputs. They are not the internal editing model.

### 5.2 Use a small orthogonal tool set

Each core tool has one main responsibility. Tools SHOULD combine cleanly rather than duplicate each other's behavior.

### 5.3 Keep creative decisions in the agent

Core tools MUST NOT make unrequested creative decisions.

For example, `score.transform` can transpose a passage by an explicit interval. It MUST NOT decide that a passage would sound better transposed and choose an interval on its own.

If an operation is musically underspecified, the tool MUST reject the request or apply a documented deterministic default. It MUST NOT silently invent musical intent.

### 5.4 Preserve stable identity

Every persistent score entity MUST have an opaque stable ID.

Edits that change an entity without replacing its musical identity SHOULD retain that ID. Engraving reflow MUST NOT change semantic IDs.

### 5.5 Use exact musical time

ScoreTex MUST represent canonical musical time with exact rational numbers. It MUST NOT use floating-point values for canonical onset or duration.

### 5.6 Make mutations atomic

Each mutation tool call MUST either apply completely or make no score change.

### 5.7 Separate structural validity from musical advice

ScoreTex MUST distinguish malformed score state from advisory musical warnings.

An out-of-range note can be a warning. A note that references a nonexistent voice is a structural error.

### 5.8 Make inspection selective

Agents MUST be able to inspect small parts of large scores without retrieving the whole score.

### 5.9 Provide a general escape hatch

`score.patch` MUST be able to express every legal state mutation that the higher-level mutation tools can express.

The higher-level tools exist for clarity, safety, and convenience. They do not define a separate editing model.

## 6. System model

A ScoreTex service manages one or more scores.

Each score has:

- a `score_id`;
- a current `revision_id`;
- immutable prior revisions;
- a graph of semantic entities;
- ordered and typed relationships between those entities.

A normal agent loop is:

```text
inspect -> select -> mutate -> validate -> render -> inspect
```

An agent can omit steps when they are not needed.

## 7. Identifiers and revisions

### 7.1 Score IDs

`score_id` is an opaque string assigned by ScoreTex.

Clients MUST NOT infer meaning from its value.

### 7.2 Entity IDs

Every persistent entity has an `id` field.

Entity IDs MUST be unique within a score for the full retained history of that score. An implementation MUST NOT reuse the ID of a deleted entity for a different entity.

### 7.3 Revision IDs

Every committed mutation creates a new immutable revision.

`revision_id` is an opaque string.

All mutation requests MUST include `base_revision_id`, except the initial `score.create` call.

The `base_revision_id` MUST equal the current score revision. If it does not, the mutation MUST fail with `REVISION_CONFLICT` and MUST NOT change the score.

v1alpha1 defines a linear revision history. Branch creation and automatic merge are outside scope.

### 7.4 Idempotency

Every mutation request SHOULD include an `idempotency_key`.

When an implementation receives the same key with the same normalized request, it MUST return the original result instead of applying the mutation again while the key remains in its idempotency store.

If the same key is reused with a different request, the implementation MUST return `IDEMPOTENCY_CONFLICT`.

The service documentation MUST state how long idempotency records are retained.

## 8. Common data types

### 8.1 Rational

Exact musical quantities use this shape:

```json
{
  "n": 1,
  "d": 4
}
```

Rules:

- `n` is a signed integer.
- `d` is a positive integer.
- zero MUST be encoded as `{ "n": 0, "d": 1 }`.
- implementations MUST normalize fractions to lowest terms at API boundaries.

For canonical score time, one whole note equals `1/1`. Therefore:

- half note = `1/2`;
- quarter note = `1/4`;
- eighth note = `1/8`.

### 8.2 Musical position

The canonical position is absolute score time from the score origin:

```json
{
  "time": { "n": 13, "d": 4 }
}
```

A client MAY address a position through a measure:

```json
{
  "measure_id": "m_12",
  "offset": { "n": 1, "d": 4 }
}
```

The engine MUST resolve measure-relative positions to exact score time before mutation.

Beat numbers are presentation values. They are not canonical time because beat meaning depends on meter.

### 8.3 Time range

Time ranges are half-open unless a tool states otherwise:

```text
[start, end)
```

Example:

```json
{
  "start": { "n": 2, "d": 1 },
  "end": { "n": 3, "d": 1 }
}
```

An event at `end` is outside the range.

### 8.4 Pitch

A written pitched note uses spelling, not only a MIDI number:

```json
{
  "step": "F",
  "octave": 4,
  "alter": { "n": 1, "d": 1 }
}
```

`step` MUST be one of `C`, `D`, `E`, `F`, `G`, `A`, or `B`.

`alter` is measured in semitones. It can be fractional, so microtonal notation is representable.

Examples:

```text
F4       alter = 0/1
F#4      alter = 1/1
Fb4      alter = -1/1
F half-sharp  alter = 1/2
```

Written pitch is canonical for notation. Sounding pitch is derived from written pitch and the active instrument transposition unless an entity explicitly represents concert pitch.

An unpitched event MUST use an unpitched pitch descriptor rather than a fake pitched spelling.

### 8.5 Duration

A temporal event has an exact semantic `duration`.

Notation-specific duration data, including dots and tuplet display, is separate from semantic duration.

Example:

```json
{
  "duration": { "n": 1, "d": 4 },
  "notation": {
    "base": { "n": 1, "d": 4 },
    "dots": 0
  }
}
```

A triplet eighth can have:

```json
{
  "duration": { "n": 1, "d": 12 },
  "notation": {
    "base": { "n": 1, "d": 8 },
    "time_modification": {
      "actual": 3,
      "normal": 2
    }
  }
}
```

The semantic duration governs time. Notation fields govern representation.

### 8.6 Grace events

Grace events do not consume canonical score time unless an explicit performance model assigns them time.

They MUST have:

- an anchor to a timed event or position;
- a stable order among grace events at the same anchor;
- notation properties that state their written form.

## 9. Canonical score model

### 9.1 Overview

The score is a typed graph with temporal information.

The main entity kinds are:

```text
score
part
instrument
staff
voice
measure
note
chord
tone
rest
directive
annotation
spanner
group
layout
```

An implementation MAY add entity kinds through the extension mechanism in section 21.

### 9.2 Score

The score root contains document-level metadata and references to top-level musical structure.

Minimum shape:

```json
{
  "id": "score_root",
  "kind": "score",
  "metadata": {
    "title": "Example",
    "composer": "Example Composer"
  },
  "part_ids": ["part_1"]
}
```

Metadata MAY include title, subtitle, movement, composer, lyricist, arranger, copyright, source, and other named values.

### 9.3 Part

A `part` represents one logical performance part.

It can contain one or more instruments and staves.

```json
{
  "id": "part_1",
  "kind": "part",
  "name": "Violin",
  "short_name": "Vln.",
  "instrument_ids": ["inst_1"],
  "staff_ids": ["staff_1"]
}
```

### 9.4 Instrument

An `instrument` describes sounding behavior and notation defaults.

It SHOULD include:

- name;
- transposition;
- optional written and sounding ranges;
- optional instrument identifier from a known taxonomy;
- optional playback mapping.

An instrument change during a part is represented by a directive that activates another instrument entity at a score time.

### 9.5 Staff

A `staff` belongs to a part.

```json
{
  "id": "staff_1",
  "kind": "staff",
  "part_id": "part_1",
  "index": 0,
  "lines": 5,
  "voice_ids": ["voice_1"]
}
```

A staff can change clef, key, meter, line count, and other properties over time through directives.

### 9.6 Voice

A `voice` is an ordered temporal stream within a staff.

```json
{
  "id": "voice_1",
  "kind": "voice",
  "staff_id": "staff_1",
  "index": 0
}
```

A voice MAY contain simultaneous events only when the entity semantics allow them. A normal pitched simultaneity SHOULD use a `chord` rather than overlapping independent notes in the same voice.

### 9.7 Measure

A `measure` defines a notational interval and its scope.

```json
{
  "id": "measure_12",
  "kind": "measure",
  "scope_ids": ["staff_1"],
  "start": { "n": 11, "d": 1 },
  "duration": { "n": 1, "d": 1 },
  "number": "12"
}
```

Measures are not required to form one global partition. This allows polymeter, local barlines, cadenzas, and other notation in which staves do not share identical measure boundaries.

Two measure entities MAY overlap when their scopes differ.

### 9.8 Note

A `note` is a single pitched timed event.

```json
{
  "id": "note_1",
  "kind": "note",
  "voice_id": "voice_1",
  "onset": { "n": 0, "d": 1 },
  "duration": { "n": 1, "d": 4 },
  "pitch": {
    "step": "C",
    "octave": 4,
    "alter": { "n": 0, "d": 1 }
  }
}
```

### 9.9 Chord and tone

A `chord` is one timed event with two or more child `tone` entities.

The chord owns onset and duration. Each tone owns a pitch and tone-specific notation such as fingering or notehead style.

```json
{
  "id": "chord_1",
  "kind": "chord",
  "voice_id": "voice_1",
  "onset": { "n": 1, "d": 1 },
  "duration": { "n": 1, "d": 4 },
  "tone_ids": ["tone_1", "tone_2"]
}
```

### 9.10 Rest

A `rest` is a timed event in a voice.

It has onset and duration but no pitch.

A multimeasure rest MAY be represented through grouping and notation metadata while retaining exact covered time.

### 9.11 Directive

A `directive` changes score interpretation or notation at a position without consuming time.

Directive types include:

- clef;
- key signature;
- time signature;
- tempo;
- barline;
- instrument change;
- staff configuration;
- transposition state;
- rehearsal mark;
- navigation mark.

A directive contains:

- an ID;
- a scope;
- an onset;
- a directive type;
- a typed value.

### 9.12 Annotation

An `annotation` adds information to an event, position, region, or structural object.

Annotation types include:

- dynamic;
- articulation;
- ornament;
- lyric;
- fingering;
- text;
- expression;
- chord symbol;
- figured bass;
- playing technique;
- analysis mark.

An annotation MUST state its anchor explicitly.

### 9.13 Spanner

A `spanner` connects two anchors or covers a range.

Examples include:

- tie;
- slur;
- crescendo and diminuendo hairpins;
- ottava;
- pedal;
- trill extension;
- glissando;
- volta;
- phrase marks.

A spanner MUST have a typed start anchor and end anchor. A tie MUST connect compatible pitched note or tone entities.

### 9.14 Group

A `group` represents a semantic grouping whose members remain first-class entities.

Examples include:

- tuplet;
- beam group;
- tremolo group;
- staff group;
- phrase or analytical group;
- repeat structure.

A group MUST state its `group_type` and member IDs.

### 9.15 Layout

A `layout` entity holds presentation constraints that are not part of core musical meaning.

Examples include:

- system break;
- page break;
- forced stem direction;
- manual placement;
- spacing hint;
- visibility override.

Semantic editing MUST preserve unrelated layout entities when possible.

A renderer MAY ignore unsupported layout hints, but it SHOULD report ignored constraints when the caller requests render diagnostics.

## 10. Relationships and ownership

Each entity relation is either:

- **owning**: deleting the parent normally deletes the owned child;
- **referential**: the referenced entity can exist independently.

Examples:

- score owns parts;
- part owns staves and instrument definitions;
- staff owns voices;
- chord owns tones;
- a spanner refers to its anchors;
- an annotation refers to its anchor unless its type is modeled as an owned child.

The engine MUST maintain referential integrity.

A mutation that would leave an invalid reference MUST either:

1. update or remove the dependent relation as explicitly requested; or
2. fail before commit.

The engine MUST NOT leave dangling references in a committed revision.

## 11. Ordering

Where order has musical meaning, it is part of the score state.

Examples include:

- part order;
- staff order;
- voice display order;
- grace-note order;
- ordered members of some groups.

Temporal order is derived first from exact onset and then from an explicit stable order key when two same-class objects share the same position.

Clients MUST NOT use entity ID lexical order as musical order.

## 12. Selectors

### 12.1 Purpose

Selectors identify score objects without depending on renderer coordinates or serialized file locations.

Every tool that accepts a `selector` MUST accept the structured selector form defined here.

An adapter MAY also provide a textual selector language. A textual selector is a convenience syntax only. It MUST compile to the structured semantics in this section.

### 12.2 ID selector

```json
{
  "ids": ["note_1", "note_2"]
}
```

### 12.3 Kind selector

```json
{
  "kind": "note"
}
```

### 12.4 Structural scope

```json
{
  "within": {
    "ids": ["part_violin"]
  },
  "match": {
    "kind": "note"
  }
}
```

`within` selects descendants of the structural or grouping scope.

### 12.5 Time selector

```json
{
  "time": {
    "start": { "n": 4, "d": 1 },
    "end": { "n": 8, "d": 1 },
    "relation": "overlap"
  }
}
```

`relation` MUST be one of:

- `onset_in`;
- `contained`;
- `overlap`;
- `cover`.

### 12.6 Field predicate

```json
{
  "where": {
    "field": "pitch.step",
    "op": "eq",
    "value": "C"
  }
}
```

Required predicate operators are:

```text
eq
ne
lt
lte
gt
gte
in
exists
```

String matching and regular expressions MAY be implemented as extensions.

### 12.7 Boolean selectors

```json
{
  "all": [
    { "kind": "note" },
    { "where": { "field": "pitch.octave", "op": "gte", "value": 5 } }
  ]
}
```

Supported forms are:

- `all` for logical AND;
- `any` for logical OR;
- `not` for logical NOT.

### 12.8 Relationship selectors

Implementations MUST support selection by direct structural ancestry and ownership.

Implementations SHOULD also support typed relation traversal, for example selecting all annotations anchored to a selected note set.

### 12.9 Selector result order

Unless the caller requests another supported order, selector results MUST use semantic score order:

1. part order;
2. staff order;
3. voice order;
4. onset;
5. entity-type-specific stable order.

### 12.10 Empty and ambiguous selections

Read operations MAY return an empty result.

A mutation that requires at least one target MUST return `SELECTION_EMPTY` when the selector matches nothing.

If an operation requires exactly one target and the selector matches more than one, it MUST return `SELECTION_AMBIGUOUS`.

## 13. Common request fields

Mutation tools use these common fields where applicable:

```json
{
  "score_id": "score_123",
  "base_revision_id": "rev_17",
  "idempotency_key": "client-request-abc",
  "validation_mode": "structural"
}
```

`validation_mode` MUST be one of:

- `structural`: enforce model integrity only;
- `standard`: also reject notational errors that prevent normal interpretation;
- `strict`: reject all diagnostics classified as errors by the selected validator profile.

The default is `structural`.

Structural integrity is mandatory and cannot be disabled.

## 14. Common mutation response

Successful mutations SHOULD return:

```json
{
  "score_id": "score_123",
  "previous_revision_id": "rev_17",
  "revision_id": "rev_18",
  "changed_ids": ["note_9"],
  "created_ids": [],
  "deleted_ids": [],
  "diagnostics": [],
  "summary": "Changed note_9 pitch from C4 to D4."
}
```

`summary` is for the agent and user. It MUST be derived from the committed mutation and MUST NOT be the authoritative representation of the change.

## 15. Tool specification

### 15.1 `score.create`

#### Purpose

Create a score and its first revision.

#### Request

```json
{
  "metadata": {
    "title": "String Sketch",
    "composer": "Example Composer"
  },
  "defaults": {
    "time_signature": { "numerator": 4, "denominator": 4 },
    "tempo": { "bpm": 96, "beat_unit": { "n": 1, "d": 4 } }
  },
  "parts": [
    {
      "name": "Violin",
      "staves": 1,
      "voices_per_staff": 1
    }
  ],
  "idempotency_key": "create-001"
}
```

#### Behavior

The tool MUST:

1. create a new score ID;
2. create the minimum required structural entities;
3. assign stable entity IDs;
4. create the initial immutable revision;
5. return the IDs required for subsequent editing.

If no parts are supplied, an empty score root is valid.

Convenience fields in `defaults` and `parts` are initialization instructions. The resulting score state MUST be representable entirely in the canonical entity model.

#### Response

```json
{
  "score_id": "score_123",
  "revision_id": "rev_1",
  "root_id": "score_root",
  "created_ids": [
    "part_1",
    "inst_1",
    "staff_1",
    "voice_1"
  ],
  "diagnostics": []
}
```

#### External import

External score import is not a separate v1alpha1 core tool.

An implementation MAY extend `score.create` with a `source` argument for formats such as MusicXML or MEI. Imported content MUST be converted to canonical ScoreTex entities before the call returns.

Import extensions MUST report information that could not be represented or was approximated.

---

### 15.2 `score.inspect`

#### Purpose

Read selected score state with controlled detail and context.

#### Request

```json
{
  "score_id": "score_123",
  "revision_id": "rev_18",
  "selector": {
    "within": { "ids": ["part_1"] },
    "match": { "kind": "note" }
  },
  "projection": {
    "fields": ["id", "kind", "onset", "duration", "pitch", "voice_id"],
    "include_relations": ["annotations", "spanners"]
  },
  "context": {
    "before": { "n": 1, "d": 1 },
    "after": { "n": 1, "d": 1 }
  },
  "limit": 200,
  "cursor": null
}
```

#### Behavior

`revision_id` is optional. When omitted, the current revision is read.

The tool MUST NOT modify the score.

The projection MUST let callers limit returned fields and related objects.

Large results MUST support pagination.

The tool SHOULD return enough structural references to make returned entities unambiguous.

#### Response

```json
{
  "score_id": "score_123",
  "revision_id": "rev_18",
  "items": [],
  "context_items": [],
  "next_cursor": null
}
```

The engine MUST distinguish selected `items` from additional `context_items`.

---

### 15.3 `score.insert`

#### Purpose

Insert new semantic score content at an explicit structural and temporal location.

#### Request

```json
{
  "score_id": "score_123",
  "base_revision_id": "rev_18",
  "at": {
    "voice_id": "voice_1",
    "time": { "n": 4, "d": 1 }
  },
  "content": [
    {
      "kind": "note",
      "duration": { "n": 1, "d": 4 },
      "pitch": {
        "step": "D",
        "octave": 5,
        "alter": { "n": 0, "d": 1 }
      }
    }
  ],
  "collision_policy": "reject",
  "validation_mode": "standard"
}
```

#### Location

`at` MUST identify enough context to place the content without guessing. It can identify:

- a voice and time;
- a staff and time for staff-level directives;
- a parent structural object and an ordered insertion point;
- an anchor entity for annotations or spanners.

#### Collision policy

Required policies are:

- `reject`: fail if inserted timed content conflicts with existing content in the same voice;
- `overlay`: allow legal simultaneous content;
- `shift_forward`: shift subsequent timed content in the insertion scope by the inserted duration.

`shift_forward` MUST require an explicit scope when the scope cannot be derived from `at`.

The tool MUST NOT silently overwrite existing content.

#### Content

`content` can contain any legal entity or nested group accepted by the canonical model.

The engine assigns IDs to new entities unless the caller supplies valid client-generated IDs and the implementation explicitly supports them.

#### Response

The response follows the common mutation response and MUST include `created_ids`.

---

### 15.4 `score.replace`

#### Purpose

Replace selected objects or a selected musical region while leaving unrelated score state unchanged.

#### Request

```json
{
  "score_id": "score_123",
  "base_revision_id": "rev_18",
  "selector": {
    "ids": ["note_10", "note_11"]
  },
  "content": [
    {
      "kind": "note",
      "duration": { "n": 1, "d": 4 },
      "pitch": {
        "step": "E",
        "octave": 5,
        "alter": { "n": 0, "d": 1 }
      }
    },
    {
      "kind": "note",
      "duration": { "n": 1, "d": 4 },
      "pitch": {
        "step": "F",
        "octave": 5,
        "alter": { "n": 1, "d": 1 }
      }
    }
  ],
  "mode": "objects",
  "preserve": ["onset", "duration"],
  "relation_policy": "reject"
}
```

#### Modes

Required modes are:

- `objects`: replace each selected entity in semantic order;
- `region`: replace the selected temporal region as one unit.

In `objects` mode, one-to-one replacement is required when `preserve` contains entity-level fields. If the selected count and replacement count differ, the tool MUST return `CARDINALITY_MISMATCH`.

In `region` mode, the replacement content MUST fit the region unless the request explicitly allows time expansion or contraction.

#### Preserve

`preserve` lists properties copied from replaced objects to their replacements.

Required preservable properties are:

- `onset`;
- `duration`;
- `voice`;
- `annotations`;
- `layout`.

The engine MUST reject a preservation request that has no unambiguous mapping.

#### Relations

`relation_policy` MUST be one of:

- `reject`: fail when replacement would break external references;
- `reattach`: reattach compatible relations when a one-to-one mapping is available;
- `cascade`: remove dependent relations that cannot survive replacement.

The default is `reject`.

---

### 15.5 `score.delete`

#### Purpose

Delete selected score content with explicit temporal behavior.

#### Request

```json
{
  "score_id": "score_123",
  "base_revision_id": "rev_18",
  "selector": { "ids": ["note_20"] },
  "mode": "replace_with_rest",
  "relation_policy": "reject"
}
```

#### Modes

Required modes are:

#### `remove`

Remove the selected entities without closing time or filling gaps.

This can leave an underfilled voice or measure. Structural validity must still hold.

#### `replace_with_rest`

Replace deleted timed material with rests covering the same voice-time intervals.

#### `preserve_measure_duration`

Delete selected sounding events and fill the affected measure scope as needed so that the measure remains rhythmically complete.

The engine MUST NOT rewrite unaffected voices.

#### `close_time`

Remove the selected temporal span and shift subsequent events earlier.

This mode MUST include or derive an explicit temporal scope. It MUST fail if moving content would create an unresolved collision or invalid cross-boundary relation.

#### Relationship handling

`relation_policy` follows the same rules as `score.replace`.

Owned children of a deleted entity are deleted with their owner.

---

### 15.6 `score.transform`

#### Purpose

Apply a deterministic musical transformation to a selection.

#### Request

```json
{
  "score_id": "score_123",
  "base_revision_id": "rev_18",
  "selector": {
    "time": {
      "start": { "n": 8, "d": 1 },
      "end": { "n": 12, "d": 1 },
      "relation": "onset_in"
    }
  },
  "operation": "transpose",
  "parameters": {
    "chromatic_semitones": 2,
    "diatonic_steps": 1,
    "spelling_policy": "preserve_interval_spelling"
  }
}
```

#### General rule

A transform MUST be deterministic from:

- the selected score state;
- the operation;
- the parameters;
- the documented renderer or transform version when relevant.

A transform MUST preserve unrelated properties unless the operation definition states otherwise.

#### Required transform operations

#### `transpose`

Move pitched notes and chord tones by an explicit interval.

The request MUST state enough information to determine both sounding displacement and spelling behavior.

Supported parameters MUST include chromatic semitone displacement. Implementations SHOULD also support diatonic displacement for exact interval spelling.

#### `invert`

Invert pitches around an explicit pitch axis.

The request MUST state whether inversion is chromatic, diatonic, or another supported pitch-space rule.

#### `retrograde`

Reverse selected temporal material inside an explicit bounding range.

The implementation MUST document whether the operation reverses:

- event order only;
- event order and durations;
- attached annotations;
- spanner direction.

v1alpha1 default behavior is to reverse event placement and preserve each event's duration unless parameters request a different supported mode.

#### `augment_duration`

Multiply selected durations by an exact rational factor greater than `1`.

#### `diminish_duration`

Multiply selected durations by an exact rational factor between `0` and `1`.

#### `shift_time`

Move selected temporal entities by an exact signed rational offset.

#### `quantize`

Move eligible onsets and, when requested, durations to an explicit rational grid.

The request MUST state the grid and rounding rule. The core operation MUST NOT infer a groove.

#### `respell`

Change written pitch spelling while preserving sounding pitch unless parameters explicitly state otherwise.

The request MUST state a deterministic spelling policy.

#### `revoice`

Move selected events between explicit voices according to a supplied mapping or deterministic partition rule.

The core operation MUST NOT invent a voice-leading solution from a vague instruction such as "make the voicing better."

#### `change_instrument`

Change the active instrument for an explicit part or region.

The request MUST state whether existing written pitches remain written or are rewritten to preserve sounding pitch.

#### Failure

A transform MUST fail rather than guess when its parameters do not define one result.

---

### 15.7 `score.patch`

#### Purpose

Apply exact low-level mutations as one atomic transaction.

This is the general mutation primitive and the escape hatch for operations that do not have a higher-level tool.

#### Request

```json
{
  "score_id": "score_123",
  "base_revision_id": "rev_18",
  "operations": [
    {
      "op": "test",
      "id": "note_9",
      "path": "/pitch/step",
      "value": "C"
    },
    {
      "op": "set",
      "id": "note_9",
      "path": "/pitch/step",
      "value": "D"
    }
  ]
}
```

#### Required patch operations

#### `test`

Assert that a field or relationship has an expected value.

A failed test aborts the whole patch with `PATCH_TEST_FAILED`.

#### `create`

Create an entity and attach it to an explicit owner or structural position.

#### `delete`

Delete an entity with an explicit relationship policy.

#### `set`

Set or replace one field value.

#### `unset`

Remove an optional field.

Required fields cannot be unset.

#### `move`

Change structural ownership, semantic order, voice, onset, or another supported location property.

The request MUST state the destination explicitly.

#### `link`

Create a typed referential relationship.

#### `unlink`

Remove a typed referential relationship.

#### Paths

Field paths SHOULD use JSON Pointer syntax.

A patch path addresses semantic entity fields, not positions in an exported file.

#### Atomicity

Patch operations execute in listed order against one transaction-local state.

Validation occurs before commit.

If any operation or validation step fails, no operation is committed.

#### Completeness requirement

Any legal mutation made by `score.insert`, `score.replace`, `score.delete`, or `score.transform` MUST have an equivalent `score.patch` representation, even if an implementation does not expose the compiled patch to clients.

---

### 15.8 `score.validate`

#### Purpose

Check a full score or selection and return structured diagnostics.

#### Request

```json
{
  "score_id": "score_123",
  "revision_id": "rev_18",
  "selector": { "ids": ["part_1"] },
  "levels": ["structural", "notation", "performance"],
  "profile": "default"
}
```

#### Validation levels

#### `structural`

Checks canonical-model invariants.

Examples:

- missing referenced entity;
- invalid ownership;
- non-normalized rational;
- illegal negative duration;
- invalid entity field;
- duplicate ID;
- invalid chord membership.

Structural errors MUST never exist in a committed revision.

#### `notation`

Checks notation consistency.

Examples:

- underfilled or overfilled measure;
- invalid tuplet membership;
- incompatible tie;
- impossible measure-relative offset;
- conflicting clef or meter directives in one scope;
- illegal repeat structure.

#### `performance`

Checks likely execution or playback problems.

Examples:

- note outside declared instrument range;
- unsupported microtonal playback mapping;
- impossible or suspicious instrument change;
- renderer-specific playback limitation.

These checks MAY depend on instrument metadata and renderer capabilities.

#### Diagnostic shape

```json
{
  "rule_id": "notation.measure.overfilled",
  "severity": "error",
  "message": "Voice voice_1 exceeds the measure duration by 1/8 note.",
  "entity_ids": ["voice_1", "measure_12"],
  "time": { "n": 47, "d": 4 },
  "details": {
    "excess": { "n": 1, "d": 8 }
  }
}
```

Severity MUST be one of:

- `error`;
- `warning`;
- `info`.

`rule_id` SHOULD remain stable across compatible implementation releases.

#### Advisory checks

An implementation MAY add advisory musical checks. It MUST clearly identify them as advisory and MUST NOT present aesthetic opinion as structural invalidity.

---

### 15.9 `score.render`

#### Purpose

Produce a human-visible or interchange representation of a score or selection.

#### Request

```json
{
  "score_id": "score_123",
  "revision_id": "rev_18",
  "selector": null,
  "format": "svg",
  "options": {
    "concert_pitch": false,
    "page_size": "A4"
  },
  "include_diagnostics": true
}
```

#### Required format

Every conforming implementation MUST support:

- `scoretex-json`.

#### Recommended formats

A full ScoreTex implementation SHOULD support:

- `musicxml`;
- `midi`;
- `svg`.

It MAY support:

- `png`;
- `pdf`;
- `mei`;
- audio formats such as WAV or FLAC;
- implementation-specific preview formats.

#### Output

Text formats MAY be returned inline when small.

Binary or large outputs SHOULD be returned as an artifact descriptor:

```json
{
  "artifact": {
    "id": "artifact_55",
    "media_type": "image/svg+xml",
    "size_bytes": 48120,
    "uri": "scoretex-artifact:artifact_55"
  }
}
```

Artifact URIs MUST be opaque. The core API MUST NOT require an agent to construct local filesystem paths.

#### ScoreTex JSON

`scoretex-json` MUST serialize canonical semantic state, including stable IDs and all core entity fields.

Object-key order is not significant.

Arrays whose order has musical meaning MUST preserve that order.

An implementation SHOULD provide deterministic serialization for the same revision and render options.

#### Loss and approximation

When an output format cannot represent source semantics exactly, the renderer MUST either:

1. fail with a clear diagnostic; or
2. render an approximation and report the loss when the chosen format and options allow approximation.

It MUST NOT silently discard musically significant information.

---

### 15.10 `score.diff`

#### Purpose

Describe semantic differences between two revisions of the same score.

#### Request

```json
{
  "score_id": "score_123",
  "revision_a": "rev_17",
  "revision_b": "rev_18",
  "selector": null,
  "granularity": "musical"
}
```

#### Granularity

Required values are:

- `entity`: report created, deleted, and changed entities and fields;
- `musical`: summarize changes in musical terms while retaining machine-readable entity references.

Implementations MAY add other levels.

#### Response

```json
{
  "score_id": "score_123",
  "revision_a": "rev_17",
  "revision_b": "rev_18",
  "changes": [
    {
      "type": "pitch_change",
      "entity_id": "note_9",
      "before": {
        "step": "C",
        "octave": 4,
        "alter": { "n": 0, "d": 1 }
      },
      "after": {
        "step": "D",
        "octave": 4,
        "alter": { "n": 0, "d": 1 }
      }
    }
  ],
  "summary": "Changed one note from C4 to D4."
}
```

A musical diff MUST be based on semantic entities and relationships, not textual differences between serialized exports.

## 16. Error model

Tool failures MUST return a structured error.

Minimum shape:

```json
{
  "error": {
    "code": "REVISION_CONFLICT",
    "message": "base_revision_id rev_17 is not the current revision.",
    "retryable": true,
    "path": "/base_revision_id",
    "details": {
      "current_revision_id": "rev_18"
    },
    "diagnostics": []
  }
}
```

Required core error codes are:

```text
SCORE_NOT_FOUND
REVISION_NOT_FOUND
REVISION_CONFLICT
IDEMPOTENCY_CONFLICT
ENTITY_NOT_FOUND
INVALID_ARGUMENT
INVALID_SELECTOR
SELECTION_EMPTY
SELECTION_AMBIGUOUS
CARDINALITY_MISMATCH
COLLISION
STRUCTURAL_VIOLATION
VALIDATION_FAILED
PATCH_TEST_FAILED
UNSUPPORTED_OPERATION
UNSUPPORTED_FORMAT
LIMIT_EXCEEDED
INTERNAL_ERROR
```

`message` MUST explain the immediate problem in terms useful to an agent or developer.

`path` SHOULD identify the failing request field when applicable.

`retryable` MUST mean that retrying can plausibly succeed without changing the requested musical intent. It MUST NOT be used merely to indicate a server error category.

## 17. Validation during mutation

Before committing a mutation, the engine MUST:

1. apply the requested changes to transaction-local state;
2. check all structural invariants;
3. apply the requested `validation_mode`;
4. abort if the selected mode contains a rejecting error;
5. commit one new revision if validation succeeds.

Warnings and informational diagnostics MAY be returned on successful mutations.

The engine MUST NOT commit a structurally invalid score, even in `structural` mode.

## 18. Determinism

Core semantic operations SHOULD be deterministic.

For the same revision and normalized arguments:

- selectors MUST select the same semantic entities;
- patch operations MUST produce the same semantic state;
- deterministic transforms MUST produce the same semantic state;
- validation rules MUST produce the same result for the same validator version and profile.

New IDs created by separate independent requests do not need to be byte-for-byte identical.

A retried idempotent request MUST return the IDs from the first committed result.

Renderers that use nondeterministic layout or synthesis SHOULD expose a deterministic seed or state that deterministic output is not guaranteed.

## 19. Large-score behavior

### 19.1 Pagination

`score.inspect` and other potentially large read operations MUST support bounded results.

### 19.2 Limits

Implementations MAY impose limits on:

- selected entity count;
- patch operation count;
- render size;
- artifact size;
- score size;
- validation scope;
- request payload size.

When a limit is exceeded, the tool MUST return `LIMIT_EXCEEDED` and SHOULD state the relevant limit.

### 19.3 No silent truncation

A read or diff operation MUST NOT silently truncate results.

It MUST return a cursor, explicit truncation indicator, or error.

## 20. MCP and direct-agent integration

### 20.1 One semantic API

Direct and MCP integrations MUST preserve the semantics in this specification.

Transport adapters MAY change naming syntax. For example, an MCP server that does not permit dots in tool names MAY expose:

```text
score_create
score_inspect
score_insert
score_replace
score_delete
score_transform
score_patch
score_validate
score_render
score_diff
```

The adapter MUST document the mapping.

### 20.2 Stateless calls

A tool call SHOULD carry all state references needed for the operation, especially `score_id` and `revision_id` or `base_revision_id`.

An agent SHOULD NOT need hidden conversation state to identify the score revision it is editing.

### 20.3 Structured results

MCP and direct interfaces MUST expose machine-readable results. Human-readable summaries are supplementary.

### 20.4 Artifacts

Large render results SHOULD use transport-native artifact or resource mechanisms when available.

An adapter MAY map `scoretex-artifact:` URIs to MCP resources, local sandbox files, object-store URLs, or another protected artifact channel.

This mapping is transport-specific and outside the semantic core.

## 21. Extension model

ScoreTex v1alpha1 must allow experimentation without allowing silent semantic collisions.

### 21.1 Namespaced fields

Extension fields MUST use a namespaced key beginning with `x-`.

Example:

```json
{
  "x-acme-bowing-model": {
    "bow_region": "sul_ponticello"
  }
}
```

### 21.2 Unknown fields

For mutation requests:

- unknown unnamespaced fields MUST be rejected;
- unknown namespaced extension fields MAY be accepted and preserved.

For responses:

- clients MUST ignore unknown unnamespaced response fields unless they conflict with known semantics;
- clients SHOULD preserve unknown namespaced fields when round-tripping entities.

### 21.3 Extension preservation

Unrelated edits MUST preserve extension data attached to unchanged entities.

An operation that replaces or deletes an entity MAY remove its extension data as part of replacing or deleting that entity.

### 21.4 Extension entity kinds

An implementation MAY define namespaced entity kinds such as:

```text
x-acme-spectral-note
```

Core tools that do not understand the semantics MUST still preserve the entity through unrelated operations when referential integrity can be maintained.

## 22. Interchange rules

### 22.1 MusicXML and MEI

Importers and exporters SHOULD retain source identifiers when safe, but external IDs MUST NOT replace ScoreTex stable IDs.

Unsupported source constructs MUST be reported.

### 22.2 MIDI

MIDI is a performance representation, not a complete score representation.

A MIDI renderer MAY omit notation-only information. It MUST report performance limitations that materially alter pitches, timing, or channel behavior when diagnostics are requested.

### 22.3 Round trips

ScoreTex does not require exact byte-level round trips through external formats.

A claimed semantic round trip SHOULD preserve all source semantics that the target format can represent and SHOULD report unsupported information.

## 23. Security and isolation

A ScoreTex service MUST treat score content as untrusted input.

Implementations MUST validate structured inputs before use.

Renderers and importers SHOULD run with limits appropriate to complex or malformed files.

Core tool arguments MUST NOT provide unrestricted filesystem paths, shell commands, or arbitrary code execution.

Artifact references SHOULD be opaque and access-controlled by the host environment.

## 24. Conformance

### 24.1 Core-conforming implementation

A core-conforming ScoreTex v1alpha1 implementation MUST:

- implement all ten core tools;
- implement stable score, revision, and entity IDs;
- implement exact rational musical time;
- implement the core entity model needed by its supported notation subset;
- implement structured selectors;
- make mutations atomic;
- enforce structural integrity;
- implement the required error model;
- implement `scoretex-json` rendering;
- make every high-level mutation expressible through `score.patch` semantics;
- preserve unrelated semantic state during edits.

### 24.2 Full-notation claims

An implementation MUST NOT claim general score support when it knowingly drops unsupported semantics without diagnostics.

An implementation can conform to the core while supporting only a defined notation subset, but it MUST document that subset.

### 24.3 Capability discovery

A dedicated capability-discovery tool is not part of v1alpha1.

Until one is added, implementations MUST document supported:

- entity extensions;
- transformations;
- validation profiles;
- import formats;
- render formats;
- notation subsets;
- resource limits.

A future ScoreTex version SHOULD add machine-readable capability discovery if implementation diversity makes it necessary.

## 25. Required invariants

Every committed revision MUST satisfy these invariants:

1. Every entity ID is unique within the score history.
2. Every required reference resolves to an existing compatible entity.
3. Every rational value is normalized and has a positive denominator.
4. Timed event duration is non-negative, and ordinary timed events have positive duration.
5. Grace events follow the grace-event rules instead of using fake negative or floating duration.
6. Every note belongs to one valid temporal scope, normally a voice.
7. Every chord owns at least two tones unless an import compatibility extension explicitly preserves a degenerate source construct.
8. Every spanner has valid anchors compatible with its type.
9. Structural ownership has no cycles.
10. Semantic order is explicit where equal-time ordering matters.
11. Unknown unnamespaced mutation fields are not stored silently.
12. No mutation commits partially.

Notation validators can impose stronger rules without changing these structural invariants.

## 26. Recommended agent behavior

This section is informative.

An agent should normally:

1. inspect the smallest relevant score region;
2. use IDs from inspection when precision matters;
3. prefer `insert`, `replace`, `delete`, or `transform` for clear musical edits;
4. use `patch` for exact field-level or relationship changes;
5. validate after meaningful mutations;
6. render when visual notation matters;
7. inspect or diff the result before continuing a multi-step edit;
8. use the latest returned `revision_id` as the next `base_revision_id`.

The agent should not regenerate a large passage when a small local edit can express the same intent.

## 27. Example workflow

The following sequence changes one note and checks the result.

### Step 1: inspect

```json
{
  "tool": "score.inspect",
  "arguments": {
    "score_id": "score_123",
    "selector": {
      "all": [
        { "kind": "note" },
        {
          "time": {
            "start": { "n": 8, "d": 1 },
            "end": { "n": 9, "d": 1 },
            "relation": "onset_in"
          }
        }
      ]
    },
    "projection": {
      "fields": ["id", "onset", "duration", "pitch", "voice_id"]
    }
  }
}
```

Assume the response identifies `note_42` and current revision `rev_20`.

### Step 2: patch

```json
{
  "tool": "score.patch",
  "arguments": {
    "score_id": "score_123",
    "base_revision_id": "rev_20",
    "operations": [
      {
        "op": "set",
        "id": "note_42",
        "path": "/pitch",
        "value": {
          "step": "G",
          "octave": 5,
          "alter": { "n": 1, "d": 1 }
        }
      }
    ]
  }
}
```

Assume the response returns `rev_21`.

### Step 3: validate

```json
{
  "tool": "score.validate",
  "arguments": {
    "score_id": "score_123",
    "revision_id": "rev_21",
    "selector": { "ids": ["note_42"] },
    "levels": ["structural", "notation", "performance"]
  }
}
```

### Step 4: render the surrounding passage

```json
{
  "tool": "score.render",
  "arguments": {
    "score_id": "score_123",
    "revision_id": "rev_21",
    "selector": {
      "time": {
        "start": { "n": 7, "d": 1 },
        "end": { "n": 10, "d": 1 },
        "relation": "overlap"
      }
    },
    "format": "svg"
  }
}
```

This workflow uses one semantic score throughout. No step edits renderer coordinates or external notation text.

## 28. Deferred design questions

The following issues are intentionally deferred from the v1alpha1 core and should be resolved through implementation experience:

- machine-readable capability discovery;
- branching, merge, and collaborative conflict resolution;
- persistent named selections;
- explicit transaction APIs beyond atomic `score.patch`;
- standardized import as a first-class tool;
- standardized undo and redo commands;
- score-level semantic query language beyond structured selectors;
- canonical performance interpretation;
- generalized tuning systems beyond semitone-rational alteration;
- standardized engraving constraints and renderer capability negotiation;
- standardized provenance for agent-generated material;
- cross-score copy and references;
- large-score streaming protocols.

These features SHOULD be added only when they cannot be expressed cleanly through the core or when repeated workflows show that a common abstraction is missing.

## 29. Architectural summary

ScoreTex v1alpha1 has one canonical score graph, exact musical time, stable IDs, immutable revisions, structured selectors, and atomic edits.

The ten core tools divide responsibilities as follows:

| Tool | Responsibility |
|---|---|
| `score.create` | Create canonical score state |
| `score.inspect` | Read selected semantic state |
| `score.insert` | Add score entities |
| `score.replace` | Replace selected entities or regions |
| `score.delete` | Remove score entities with explicit time behavior |
| `score.transform` | Apply deterministic musical transformations |
| `score.patch` | Perform exact general mutations |
| `score.validate` | Detect structural, notational, and performance problems |
| `score.render` | Produce notation, interchange, or playback artifacts |
| `score.diff` | Compare revisions semantically |

The core design goal is simple:

> An agent can express any score edit precisely, inspect its effect, verify the result, and continue from a stable revision without depending on a specific notation file format or engraving engine.

## 30. Reference

This document uses the plain-language principles of ISO 24495-1:2023, *Plain language — Part 1: Governing principles and guidelines*, as a writing standard for relevance, findability, understandability, and usability.

ISO reference page: https://www.iso.org/standard/78907.html
