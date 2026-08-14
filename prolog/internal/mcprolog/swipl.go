package mcprolog

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

//go:embed driver.pl
var driverSource []byte

// Modes understood by driver.pl.
const (
	modeCheck   = "check"
	modeQuery   = "query"
	modeProve   = "prove"
	modeExplain = "explain"
	modeMatch   = "match"
)

// Defaults applied when a request or configuration leaves a value unset.
const (
	DefaultStatePath    = "prolog-kb.json"
	DefaultSwipl        = "swipl"
	DefaultStackLimit   = "512m"
	DefaultTimeout      = 10 * time.Second
	DefaultMaxSolutions = 200

	defaultLimit = 25
	defaultDepth = 12
	// graceMargin is how much longer the process is allowed to live than the
	// goal itself, so that the in-Prolog time limit reports the timeout first.
	graceMargin = 5 * time.Second
)

// Diagnostic is a warning or error raised by SWI-Prolog while loading a program
// or running a goal.
type Diagnostic struct {
	Kind    string `json:"kind" jsonschema:"error or warning"`
	Message string `json:"message" jsonschema:"the diagnostic text as SWI-Prolog would print it"`
}

// Predicate is a predicate defined by a program.
type Predicate struct {
	Name    string `json:"name" jsonschema:"predicate name"`
	Arity   int    `json:"arity" jsonschema:"predicate arity"`
	Dynamic bool   `json:"dynamic" jsonschema:"whether the predicate is declared dynamic"`
}

// Match is a clause whose head unified with a search pattern.
type Match struct {
	Index int    `json:"index" jsonschema:"1-based position of the clause within its unit"`
	Text  string `json:"text" jsonschema:"the clause as SWI-Prolog prints it"`
}

// Result is the union of every field driver.pl can emit. Only the fields
// relevant to the requested mode are populated.
type Result struct {
	OK          bool         `json:"ok"`
	Error       string       `json:"error"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Output      string       `json:"output"`

	// query
	Succeeded bool                `json:"succeeded"`
	Count     int                 `json:"count"`
	Truncated bool                `json:"truncated"`
	Solutions []map[string]string `json:"solutions"`

	// prove
	Proved   bool              `json:"proved"`
	Bindings map[string]string `json:"bindings"`

	// explain
	Proof string `json:"proof"`

	// check
	Predicates []Predicate `json:"predicates"`

	// match
	Total   int     `json:"total"`
	Matches []Match `json:"matches"`
}

// Runner executes driver.pl under SWI-Prolog.
type Runner struct {
	swipl      string
	stackLimit string
	sandbox    bool
	timeout    time.Duration
}

// NewRunner locates the SWI-Prolog executable and fixes the limits every query
// runs under.
func NewRunner(swipl, stackLimit string, sandbox bool, timeout time.Duration) (*Runner, error) {
	path, err := exec.LookPath(swipl)
	if err != nil {
		return nil, fmt.Errorf("swipl not found (%q): %w; this server requires a full SWI-Prolog installation", swipl, err)
	}
	return &Runner{swipl: path, stackLimit: stackLimit, sandbox: sandbox, timeout: timeout}, nil
}

// request is one invocation of driver.pl.
type request struct {
	mode    string
	program string // Prolog source; empty means "no program"
	goal    string // goal text or match pattern; empty means "none"
	limit   int
	timeout time.Duration
	depth   int
}

// Run executes one request in a fresh process and working directory. A process
// per request is slower than a persistent toplevel, but it makes each request
// hermetic: a goal that corrupts the database, exhausts the stack or wedges the
// engine cannot affect the next one.
func (r *Runner) Run(ctx context.Context, req request) (*Result, error) {
	dir, err := os.MkdirTemp("", "mcprolog-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	driver, err := writeFile(dir, "driver.pl", driverSource)
	if err != nil {
		return nil, err
	}
	programArg, err := optionalFile(dir, "program.pl", req.program)
	if err != nil {
		return nil, err
	}
	goalArg, err := optionalFile(dir, "goal.txt", req.goal)
	if err != nil {
		return nil, err
	}

	limit := orDefault(req.limit, defaultLimit)
	depth := orDefault(req.depth, defaultDepth)
	goalTimeout := orDefault(req.timeout, r.timeout)

	// The in-Prolog time limit is the first line of defence. The process-level
	// deadline is the second: a goal can wedge the engine in a way that
	// call_with_time_limit/2 cannot interrupt (deep foreign calls, runaway
	// memory), and only killing the process recovers from that.
	hardCtx, cancel := context.WithTimeout(ctx, goalTimeout+graceMargin)
	defer cancel()

	cmd := exec.CommandContext(hardCtx, r.swipl,
		"-q",
		"--stack-limit="+r.stackLimit,
		"--no-tty",
		driver,
		req.mode,
		programArg,
		goalArg,
		strconv.Itoa(limit),
		strconv.FormatInt(goalTimeout.Milliseconds(), 10),
		strconv.FormatBool(r.sandbox),
		strconv.Itoa(depth),
	)
	cmd.Dir = dir
	// Do not inherit the parent environment: the child runs agent-supplied
	// Prolog and has no business seeing the server's credentials.
	cmd.Env = []string{"LANG=C.UTF-8", "HOME=" + dir}
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	// driver.pl writes its JSON result as the last line of stdout. Anything a
	// goal wrote directly to user_output (bypassing with_output_to/2) appears
	// before it, so parse the last non-empty line rather than the whole buffer.
	line := lastNonEmptyLine(stdout.String())
	if line == "" {
		msg := strings.TrimSpace(stderr.String())
		switch {
		case errors.Is(hardCtx.Err(), context.DeadlineExceeded):
			return nil, fmt.Errorf("swipl exceeded the hard %s deadline and was killed", goalTimeout+graceMargin)
		case runErr != nil:
			return nil, fmt.Errorf("swipl failed: %w: %s", runErr, msg)
		default:
			return nil, fmt.Errorf("swipl produced no result: %s", msg)
		}
	}

	var res Result
	if err := json.Unmarshal([]byte(line), &res); err != nil {
		return nil, fmt.Errorf("could not parse driver output %q: %w", truncate(line, 200), err)
	}
	if s := strings.TrimSpace(stderr.String()); s != "" {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{Kind: "stderr", Message: s})
	}
	return &res, nil
}

// optionalFile writes content to dir, returning the driver's "absent" marker
// when there is nothing to write. Inputs reach swipl as files rather than as
// command-line text, so agent-supplied Prolog cannot be misread as an argument.
func optionalFile(dir, name, content string) (string, error) {
	if content == "" {
		return "-", nil
	}
	return writeFile(dir, name, []byte(content))
}

func writeFile(dir, name string, content []byte) (string, error) {
	path := filepath.Join(dir, name)
	return path, os.WriteFile(path, content, 0o600)
}

func orDefault[T int | time.Duration](v, fallback T) T {
	if v <= 0 {
		return fallback
	}
	return v
}

func lastNonEmptyLine(s string) string {
	for _, line := range slices.Backward(strings.Split(s, "\n")) {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
