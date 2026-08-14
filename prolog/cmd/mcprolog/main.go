// Command mcprolog is an MCP server that exposes SWI-Prolog as a set of logic
// tools: an agent can build up a knowledge base of facts and rules, organise it
// into units, choose which units are in scope, and then query, prove and
// explain goals against them.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/smxlong/mcp/prolog/internal/mcprolog"
)

// version is stamped in at release time; a build from source reports whatever
// the module cache knows instead.
var version = ""

// keepAlive is how often to ping a network peer to notice that it has gone.
const keepAlive = 30 * time.Second

func main() {
	log.SetFlags(0)
	log.SetPrefix("mcprolog: ")
	// Never write to stdout: on the stdio transport it carries the JSON-RPC
	// stream, and any stray byte corrupts the session.
	log.SetOutput(os.Stderr)

	var (
		cfg         mcprolog.Config
		httpAddr    = flag.String("http", "", "serve Streamable HTTP on this address instead of stdio (e.g. :8080)")
		showVersion = flag.Bool("version", false, "print the version and exit")
	)
	flag.StringVar(&cfg.StatePath, "state", envOr("MCPROLOG_STATE", mcprolog.DefaultStatePath), "path to the knowledge base file; empty means keep state in memory only")
	flag.StringVar(&cfg.Swipl, "swipl", envOr("MCPROLOG_SWIPL", mcprolog.DefaultSwipl), "path to the swipl executable")
	flag.DurationVar(&cfg.Timeout, "timeout", mcprolog.DefaultTimeout, "default time limit for a single goal")
	flag.StringVar(&cfg.StackLimit, "stack-limit", mcprolog.DefaultStackLimit, "SWI-Prolog stack limit for each query process")
	flag.IntVar(&cfg.MaxSolutions, "max-solutions", mcprolog.DefaultMaxSolutions, "hard cap on solutions returned by a single query")
	flag.BoolVar(&cfg.Sandbox, "sandbox", true, "reject goals that library(sandbox) considers unsafe")
	flag.BoolVar(&cfg.AllowDirectives, "allow-directives", false, "allow arbitrary ':- Goal.' directives in unit source (unsafe: directives run at load time, outside the sandbox)")
	flag.BoolVar(&cfg.Serialize, "serialize", true, "run tool calls one at a time; disable only if every tool is independent and side-effect free")
	flag.Parse()

	cfg.Version = buildVersion()
	if *showVersion {
		fmt.Println(cfg.Version)
		return
	}
	if *httpAddr != "" {
		// Keepalive pings detect peers that vanish without closing the
		// connection, which only happens on a network transport. On stdio a
		// closed stdin already signals disconnection, and an in-flight ping
		// racing with that EOF turns a clean shutdown into an error.
		cfg.KeepAlive = keepAlive
	}

	if err := run(cfg, *httpAddr); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(cfg mcprolog.Config, httpAddr string) error {
	server, err := mcprolog.New(cfg)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if httpAddr == "" {
		// One process, one client, framed over stdin/stdout.
		if err := server.Run(ctx, &mcp.StdioTransport{}); !isCleanShutdown(err) {
			return err
		}
		return nil
	}
	return serveHTTP(ctx, server, httpAddr)
}

// serveHTTP serves the Streamable HTTP transport: one process, many clients.
// The handler is called per request and returns the server to use for that
// session.
func serveHTTP(ctx context.Context, server *mcp.Server, addr string) error {
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	log.Printf("listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "devel"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// codeServerClosing is the JSON-RPC error code the SDK reports when a session
// ends because the peer went away. On stdio that is exactly what a client
// closing stdin looks like, so Server.Run always returns an error wrapping it
// at the end of a normal session. The SDK does not export the constant (it is
// jsonrpc2.ErrServerClosing internally), so it is repeated here.
const codeServerClosing = -32004

// isCleanShutdown reports whether an error from Server.Run represents an
// ordinary end of session rather than a failure. Without this, every stdio
// server exits non-zero every time its client disconnects.
func isCleanShutdown(err error) bool {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return true
	}
	var wireErr *jsonrpc.Error
	return errors.As(err, &wireErr) && wireErr.Code == codeServerClosing
}
