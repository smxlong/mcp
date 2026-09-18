#!/usr/bin/env python3
"""Repeatable stdio MCP demonstration. No third-party Python packages required."""
import argparse
import json
import subprocess
import tempfile
from pathlib import Path


def rational(n, d=1):
    return {"n": n, "d": d}


def pitch(step="C", octave=4, alter=0):
    return {"step": step, "octave": octave, "alter": rational(alter)}


class Client:
    def __init__(self, binary, state):
        self.proc = subprocess.Popen([binary, "-state", str(state)], stdin=subprocess.PIPE,
                                     stdout=subprocess.PIPE, text=True)
        self.seq = 0
        self.request("initialize", {"protocolVersion": "2025-11-25", "capabilities": {},
                                    "clientInfo": {"name": "scoretex-validation", "version": "1"}})
        self.send({"jsonrpc": "2.0", "method": "notifications/initialized"})

    def send(self, message):
        self.proc.stdin.write(json.dumps(message) + "\n")
        self.proc.stdin.flush()

    def request(self, method, params):
        self.seq += 1
        self.send({"jsonrpc": "2.0", "id": self.seq, "method": method, "params": params})
        while True:
            line = self.proc.stdout.readline()
            assert line, "server exited unexpectedly"
            response = json.loads(line)
            if response.get("id") == self.seq:
                assert "error" not in response, response
                return response["result"]

    def call(self, name, arguments, error=None):
        result = self.request("tools/call", {"name": "score." + name, "arguments": arguments})
        output = result.get("structuredContent")
        assert output is not None, result
        if error:
            assert result.get("isError") and output["error"]["code"] == error, output
        else:
            assert not result.get("isError"), output
        return output

    def close(self):
        self.proc.stdin.close()
        assert self.proc.wait(timeout=10) == 0
        self.proc.stdout.close()


def demonstrate(binary, state):
    c = Client(binary, state)
    tools = c.request("tools/list", {})["tools"]
    assert len(tools) == 10 and all(t["description"] and t["inputSchema"] for t in tools)
    create = {"metadata": {"title": "Repeatable ScoreTex demonstration"},
              "parts": [{"name": "Piano", "staves": 1, "voices_per_staff": 2}],
              "defaults": {"time_signature": {"numerator": 4, "denominator": 4}},
              "idempotency_key": "demo-create"}
    score = c.call("create", create)
    assert score == c.call("create", create)
    sid, revision, root = score["score_id"], score["revision_id"], score["root_id"]
    initial = revision

    def read(name, **args):
        return c.call(name, {"score_id": sid, **args})

    def edit(name, **args):
        nonlocal revision
        result = c.call(name, {"score_id": sid, "base_revision_id": revision, **args})
        revision = result["revision_id"]
        return result

    all_items = read("inspect")["items"]
    voices = [e["id"] for e in all_items if e["kind"] == "voice"]
    staff = next(e["id"] for e in all_items if e["kind"] == "staff")
    part = next(e["id"] for e in all_items if e["kind"] == "part")
    instrument = next(e["id"] for e in all_items if e["kind"] == "instrument")
    voice, second_voice = voices
    edit("patch", operations=[{"op": "create", "entity": {"id": "measure", "kind": "measure",
         "scope_ids": [staff], "start": rational(0), "duration": rational(1), "number": "1"}}])
    content = [{"kind": "note", "duration": rational(2, 8), "pitch": pitch(step),
                "x-demo-color": "blue"} for step in "CDEF"]
    inserted = edit("insert", at={"voice_id": voice, "measure_id": "measure", "offset": rational(0)},
                    content=content, collision_policy="reject", idempotency_key="insert-notes")
    notes = read("inspect", selector={"kind": "note"})["items"]
    assert len(notes) == 4 and notes[0]["duration"] == rational(1, 4)
    note = notes[0]["id"]
    assert len(read("inspect", selector={"within": {"ids": [part]}, "match": {"kind": "note"}})["items"]) == 4
    for relation in ("onset_in", "contained", "overlap", "cover"):
        read("inspect", selector={"time": {"start": rational(0), "end": rational(1, 4), "relation": relation}})
    for op, value in [("eq", 4), ("ne", 8), ("lt", 5), ("lte", 4), ("gt", 3), ("gte", 4),
                      ("in", [3, 4]), ("exists", True)]:
        assert len(read("inspect", selector={"all": [{"kind": "note"},
                   {"where": {"field": "pitch.octave", "op": op, "value": value}}]})["items"]) == 4
    read("inspect", selector={"any": [{"kind": "note"}, {"not": {"kind": "note"}}]})
    page = read("inspect", revision_id=revision, limit=2)
    total = len(page["items"])
    while page["next_cursor"]:
        page = read("inspect", revision_id=revision, limit=2, cursor=page["next_cursor"])
        total += len(page["items"])
    assert total == len(read("inspect")["items"])
    edit("patch", operations=[
        {"op": "create", "entity": {"id": "dynamic", "kind": "annotation", "annotation_type": "dynamic",
         "anchor": {"entity_id": note}, "value": "mf"}},
        {"op": "create", "entity": {"id": "layout", "kind": "layout", "layout_type": "stem",
         "anchor": {"entity_id": note}, "value": "up"}},
        {"op": "test", "id": note, "path": "/pitch/step", "value": "C"},
        {"op": "set", "id": note, "path": "/pitch/step", "value": "G"},
        {"op": "set", "id": note, "path": "/x-demo-temp", "value": True},
        {"op": "unset", "id": note, "path": "/x-demo-temp"}])
    detail = read("inspect", selector={"ids": [note]}, projection={"fields": ["pitch"],
                  "include_relations": ["annotations", "layout"]}, context={"after": rational(1, 4)})
    assert len(detail["context_items"]) == 3
    before = revision
    c.call("patch", {"score_id": sid, "base_revision_id": revision, "operations": [
        {"op": "set", "id": note, "path": "/pitch/step", "value": "A"},
        {"op": "test", "id": note, "path": "/pitch/step", "value": "B"}]}, "PATCH_TEST_FAILED")
    assert read("inspect")["revision_id"] == before
    c.call("delete", {"score_id": sid, "base_revision_id": initial,
                      "selector": {"ids": [note]}}, "REVISION_CONFLICT")
    c.call("create", {"idempotency_key": "demo-create"}, "IDEMPOTENCY_CONFLICT")
    c.call("insert", {"score_id": sid, "base_revision_id": revision, "at": {"voice_id": voice, "time": rational(0)},
                      "content": content[:1]}, "COLLISION")
    edit("patch", operations=[{"op": "create", "entity": {"id": "phrase", "kind": "group", "group_type": "phrase", "member_ids": []}},
                              {"op": "link", "id": "phrase", "relation": "member_ids", "target_id": note},
                              {"op": "unlink", "id": "phrase", "relation": "member_ids", "target_id": note},
                              {"op": "move", "id": note, "to": {"voice_id": second_voice}},
                              {"op": "move", "id": note, "to": {"voice_id": voice}}])
    transforms = [
        ("transpose", {"chromatic_semitones": 2, "diatonic_steps": 1, "spelling_policy": "preserve_interval_spelling"}),
        ("invert", {"axis": pitch(), "space": "chromatic", "spelling_policy": "sharps"}),
        ("respell", {"spelling_policy": "flats"}),
        ("augment_duration", {"factor": rational(2)}),
        ("diminish_duration", {"factor": rational(1, 2)}),
        ("shift_time", {"offset": rational(1, 16)}),
        ("quantize", {"grid": rational(1, 4), "rounding": "nearest", "durations": True}),
        ("revoice", {"mapping": {voice: second_voice}}),
        ("revoice", {"mapping": {second_voice: voice}}),
    ]
    for operation, parameters in transforms:
        edit("transform", selector={"ids": [note]}, operation=operation, parameters=parameters)
    edit("transform", selector={"kind": "note"}, operation="retrograde", parameters={"start": rational(0), "end": rational(1)})
    edit("transform", selector={"ids": [part]}, operation="change_instrument",
         parameters={"instrument_id": instrument, "time": rational(0), "pitch_policy": "preserve_written"})
    assert read("inspect", selector={"ids": [note]})["items"][0]["x-demo-color"] == "blue"
    edit("replace", selector={"ids": [note]}, content=[{"kind": "note", "pitch": pitch("B")}],
         preserve=["onset", "duration", "voice", "annotations", "layout"], relation_policy="reattach")
    for mode in ("replace_with_rest", "preserve_measure_duration"):
        target = read("inspect", selector={"kind": "note"})["items"][0]["id"]
        edit("delete", selector={"ids": [target]}, mode=mode, relation_policy="cascade")
    # Separate voice demonstrates region replacement and both temporal collision policies.
    edit("insert", at={"voice_id": second_voice, "time": rational(2)}, content=content[:2])
    region = {"all": [{"kind": "note"}, {"where": {"field": "voice_id", "op": "eq", "value": second_voice}}]}
    edit("replace", selector=region, mode="region", content=[{"kind": "rest", "duration": rational(1, 2)}])
    edit("insert", at={"voice_id": second_voice, "time": rational(2)}, content=content[:1], collision_policy="shift_forward")
    edit("delete", selector=region, mode="close_time")
    overlay = edit("insert", at={"voice_id": second_voice, "time": rational(2)}, content=content[:1], collision_policy="overlay")
    edit("delete", selector={"ids": overlay["created_ids"]}, mode="remove")
    edit("patch", operations=[{"op": "create", "entity": {"id": "slur", "kind": "spanner",
         "spanner_type": "slur", "start_anchor": {"time": rational(0), "scope_id": staff},
         "end_anchor": {"time": rational(1), "scope_id": staff}}}])
    edit("transform", selector={"ids": [part]}, operation="change_instrument",
         parameters={"instrument_id": instrument, "time": rational(0), "pitch_policy": "preserve_sounding"})
    # Chord ownership and owned-child deletion.
    edit("patch", operations=[
        {"op": "create", "entity": {"id": "tone-a", "kind": "tone", "pitch": pitch("C")}},
        {"op": "create", "entity": {"id": "tone-b", "kind": "tone", "pitch": pitch("E")}},
        {"op": "create", "entity": {"id": "chord", "kind": "chord", "voice_id": second_voice,
         "onset": rational(4), "duration": rational(1, 4), "tone_ids": ["tone-a", "tone-b"]}}])
    edit("transform", selector={"ids": ["chord"]}, operation="transpose",
         parameters={"chromatic_semitones": 1, "spelling_policy": "sharps"})
    edit("patch", operations=[{"op": "delete", "id": "chord", "relation_policy": "cascade"}])
    assert not read("inspect", selector={"ids": ["tone-a", "tone-b"]})["items"]
    for mode in ("structural", "standard", "strict"):
        edit("patch", operations=[{"op": "set", "id": root, "path": "/metadata/title", "value": "Validated " + mode}], validation_mode=mode)
    read("validate", levels=["structural", "notation", "performance"], profile="default")
    rendered = read("render", revision_id=revision, format="scoretex-json", include_diagnostics=True)
    assert rendered == read("render", revision_id=revision, format="scoretex-json", include_diagnostics=True)
    assert json.loads(rendered["text"])["root_id"] == root
    for granularity in ("entity", "musical"):
        assert read("diff", revision_a=initial, revision_b=revision, granularity=granularity)["changes"]
    c.call("render", {"score_id": sid, "format": "svg"}, "UNSUPPORTED_FORMAT")
    c.call("inspect", {"score_id": sid, "limit": 1001}, "INVALID_ARGUMENT")
    c.call("patch", {"score_id": sid, "base_revision_id": revision,
                      "operations": [{"op": "create", "entity": {"id": "tone-a", "kind": "tone", "pitch": pitch()}}]}, "STRUCTURAL_VIOLATION")
    c.close()
    c = Client(binary, state)
    assert c.call("inspect", {"score_id": sid})["revision_id"] == revision
    assert c.call("create", create) == score
    assert c.call("inspect", {"score_id": sid, "revision_id": initial})["revision_id"] == initial
    c.close()
    print("PASS: all 10 tools, all 10 transforms, selectors, patches, relations, revisions, retries, rollback, rendering and restart persistence")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    root = Path(__file__).resolve().parents[1]
    default_binary = root / "bin" / "scoretex"
    if not default_binary.exists():
        default_binary = root / "scoretex"
    parser.add_argument("--binary", default=str(default_binary))
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="scoretex-validation-") as directory:
        demonstrate(str(Path(args.binary).resolve()), Path(directory) / "state.json")


if __name__ == "__main__":
    main()
