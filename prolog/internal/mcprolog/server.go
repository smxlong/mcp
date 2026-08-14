// Package mcprolog implements the mcprolog MCP server: a persistent Prolog
// knowledge base, organised into units, that an agent can query, prove and
// explain goals against.
//
// Reasoning is delegated to SWI-Prolog, which runs as a short-lived subprocess
// per request. Durable state lives here, in Go, not in the Prolog process.
package mcprolog

import (
	"cmp"
	"context"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Config configures a server. A zero StatePath keeps the knowledge base in
// memory only; the other zero values select the package defaults.
type Config struct {
	StatePath       string        // knowledge base file
	Swipl           string        // swipl executable to run
	StackLimit      string        // SWI-Prolog stack limit per query process
	Timeout         time.Duration // default time limit for a single goal
	MaxSolutions    int           // hard cap on solutions per query
	Sandbox         bool          // reject goals library(sandbox) considers unsafe
	AllowDirectives bool          // permit arbitrary ':- Goal.' in unit source
	Serialize       bool          // run tool calls one at a time
	KeepAlive       time.Duration // ping interval; leave zero on stdio
	Version         string        // reported to clients during initialization
}

// New builds a server from cfg. It fails if the knowledge base cannot be read
// or SWI-Prolog cannot be found.
func New(cfg Config) (*mcp.Server, error) {
	kb, err := NewKB(cfg.StatePath)
	if err != nil {
		return nil, err
	}
	runner, err := NewRunner(
		cmp.Or(cfg.Swipl, DefaultSwipl),
		cmp.Or(cfg.StackLimit, DefaultStackLimit),
		cfg.Sandbox,
		orDefault(cfg.Timeout, DefaultTimeout),
	)
	if err != nil {
		return nil, err
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "prolog",
		Title:   "SWI-Prolog logic tools",
		Version: cmp.Or(cfg.Version, "dev"),
	}, &mcp.ServerOptions{
		// Instructions are shown to the model once, at initialization. They are
		// the right place for cross-cutting guidance that would otherwise have to
		// be repeated in every tool description.
		Instructions: instructions,
		KeepAlive:    cfg.KeepAlive,
	})

	svc := &service{
		kb:              kb,
		runner:          runner,
		maxSolutions:    orDefault(cfg.MaxSolutions, DefaultMaxSolutions),
		allowDirectives: cfg.AllowDirectives,
	}
	svc.register(server)
	if cfg.Serialize {
		server.AddReceivingMiddleware(serializeToolCalls())
	}
	return server, nil
}

// serializeToolCalls returns receiving middleware that lets at most one
// tools/call run at a time.
//
// The SDK handles every request except initialize asynchronously, so tool calls
// that a client issues as a batch execute concurrently by default. That is fine
// for a stateless server and wrong for this one: two calls that both read the
// knowledge base, act on it and write it back can interleave, and a read issued
// alongside a mutation can observe the state from before it.
//
// Only tools/call is serialised. tools/list, ping and the rest stay concurrent,
// so a long-running query does not make the server look hung.
//
// This guarantees mutual exclusion, not arrival order: the SDK releases each
// request for concurrent execution before any middleware runs, so by the time
// this lock is contended the original ordering is already lost. A client that
// needs B to observe A's effect must wait for A's response before sending B --
// no server-side setting can substitute for that.
func serializeToolCalls() mcp.Middleware {
	var mu sync.Mutex
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			mu.Lock()
			defer mu.Unlock()
			// Do not start work the caller has already given up on.
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return next(ctx, method, req)
		}
	}
}

const instructions = `This server gives you a persistent Prolog knowledge base and the ability to
reason over it.

Facts and rules live in units: named groups of clauses. Only units that are in
scope are visible to queries, so you can reason under different sets of
assumptions by changing what is loaded.

A normal working pattern is:

  1. prolog_list_units to see what already exists.
  2. prolog_create_unit, then prolog_assert to add facts and rules.
  3. prolog_load_unit (or prolog_set_scope) to choose what is in scope.
  4. prolog_query / prolog_prove to draw conclusions, and prolog_explain when
     you need to justify or debug one.

Goals are written without a trailing '.'. Variables start with a capital letter
or an underscore; those starting with '_' are not reported in results.

State persists across calls, so treat the knowledge base as long-lived: check
what is there before adding to it, and prefer editing a unit over creating a
near-duplicate.`
