package scoretex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Config struct {
	StatePath string
	Version   string
	KeepAlive time.Duration
}

var descriptions = map[string]string{
	"create":    "Create a score with stable structural IDs. Optional parts contain name, staves and voices_per_staff; defaults contain time_signature and tempo.",
	"inspect":   "Inspect a revision using IDs, kind, within/match, time, where, all/any/not selectors. Use projection and pagination to keep results small.",
	"insert":    "Insert explicit entities at voice/time, measure/offset, owner, or anchor. Choose reject, overlay or shift_forward collision behavior.",
	"replace":   "Replace selected objects one-for-one, or an exact single-voice region. Preserve onset, duration, voice, annotations or layout explicitly.",
	"delete":    "Delete selected entities with remove, replace_with_rest, preserve_measure_duration or close_time; relation policy is reject, cascade or reattach.",
	"transform": "Apply an explicit deterministic musical transform. Supported: transpose, invert, retrograde, augment_duration, diminish_duration, shift_time, quantize, respell, revoice, change_instrument.",
	"patch":     "Apply ordered test/create/delete/set/unset/move/link/unlink operations atomically. Paths are semantic JSON Pointers; ID arrays are replaced as whole values.",
	"validate":  "Return structural, notation and performance diagnostics for a revision or selection. Supported profile: default.",
	"render":    "Serialize canonical entities deterministically. Supported format: scoretex-json. Selection renders are explicitly marked partial.",
	"diff":      "Compare two immutable revisions semantically, with optional selector. Granularity: entity or musical; results never silently truncate.",
}

func New(cfg Config) (*mcp.Server, error) {
	engine, er := NewEngine(cfg.StatePath)
	if er != nil {
		return nil, er
	}
	version := cfg.Version
	if version == "" {
		version = "devel"
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "scoretex", Version: version}, &mcp.ServerOptions{KeepAlive: cfg.KeepAlive, Instructions: `ScoreTex v1alpha1: inspect -> mutate -> validate -> render. Exact time uses {n: integer, d: positive integer}, one whole note = 1/1. Every edit requires score_id and current base_revision_id; await dependent calls. Reuse an idempotency_key for retries. Revisions and keys persist for the lifetime of the configured state file, or process in memory mode. The ten score.* tools use the specification's names. Structural validation is mandatory; standard/strict also reject notation errors. See README for notation subset and limits.`})
	for _, name := range []string{"create", "inspect", "insert", "replace", "delete", "transform", "patch", "validate", "render", "diff"} {
		props := map[string]any{}
		for _, key := range strings.Fields(requestFields[name] + " score_id base_revision_id idempotency_key validation_mode") {
			props[key] = parameterSchema(key)
		}
		schema := Object{"type": "object", "properties": props, "additionalProperties": true}
		readOnly := !mutation(name)
		destructive := name == "delete" || name == "replace" || name == "patch" || name == "transform"
		closed := false
		mcp.AddTool(s, &mcp.Tool{Name: "score." + name, Description: descriptions[name], InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly, IdempotentHint: readOnly, DestructiveHint: &destructive, OpenWorldHint: &closed}}, func(ctx context.Context, req *mcp.CallToolRequest, in map[string]any) (*mcp.CallToolResult, map[string]any, error) {
			// Decode the original bytes: map decoding through float64 loses exact integers.
			decoder := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
			decoder.UseNumber()
			var out Object
			er := decoder.Decode(&in)
			if er == nil {
				out, er = engine.Call(ctx, name, in)
			}
			result := &mcp.CallToolResult{}
			if er != nil {
				var fault *Fault
				if !errors.As(er, &fault) {
					fault = &Fault{Code: "INTERNAL_ERROR", Message: er.Error(), Details: Object{}, Diagnostics: []Object{}}
				}
				out = Object{"error": fault}
				result.IsError = true
			}
			b, _ := json.Marshal(out)
			result.Content = []mcp.Content{&mcp.TextContent{Text: string(b)}}
			return result, out, nil
		})
	}
	s.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			if r, ok := result.(*mcp.CallToolResult); ok && r.IsError && r.StructuredContent == nil {
				message := "invalid tool arguments"
				for _, c := range r.Content {
					if tc, ok := c.(*mcp.TextContent); ok {
						message = tc.Text
						break
					}
				}
				body := Object{"error": &Fault{Code: "INVALID_ARGUMENT", Message: message, Details: Object{}, Diagnostics: []Object{}}}
				r.StructuredContent = body
				b, _ := json.Marshal(body)
				r.Content = []mcp.Content{&mcp.TextContent{Text: string(b)}}
			}
			return result, err
		}
	})
	return s, nil
}
func parameterDescription(k string) string {
	switch k {
	case "selector":
		return "Structured selector: ids, kind, within/match, time {start,end,relation}, where {field,op,value}, all, any, not."
	case "base_revision_id":
		return "Required for edits: current revision returned by the preceding successful call."
	case "revision_id":
		return "Immutable revision to read; defaults to current. Pin this while paging."
	case "content":
		return "Array of canonical entities; IDs assigned unless a never-used client ID is supplied."
	case "operations":
		return "Ordered patch objects with op, id, path/value, entity/owner_id/relation, to, or target_id as applicable."
	case "parameters":
		return "Explicit operation arguments; see documented transform parameter table."
	case "idempotency_key":
		return "Unique retry key retained with score history; different requests may not reuse it."
	default:
		return strings.ReplaceAll(k, "_", " ") + " as defined by ScoreTex v1alpha1."
	}
}

func parameterSchema(k string) Object {
	s := Object{"description": parameterDescription(k)}
	switch k {
	case "parts", "content", "operations", "preserve", "levels":
		s["type"] = "array"
		s["items"] = Object{}
	case "metadata", "defaults", "source", "at", "projection", "context", "parameters", "options":
		s["type"] = "object"
		s["additionalProperties"] = true
	case "selector":
		s["type"] = []string{"object", "null"}
		s["additionalProperties"] = true
	case "limit":
		s["type"] = "integer"
	case "include_diagnostics":
		s["type"] = "boolean"
	case "cursor":
		s["type"] = []string{"string", "null"}
	default:
		s["type"] = "string"
	}
	return s
}
