package mcprolog

import (
	"fmt"
	"strings"
	"unicode"
)

// SplitClauses splits Prolog source text into individual clauses, each
// including its terminating '.'. Comments are preserved and attach to the
// clause that follows them.
//
// This is a lexer, not a parser: it only needs to know where clauses end. The
// cases that matter are the ones where a '.' or a quote does not mean what it
// appears to mean:
//
//   - inside 'quoted atoms', "strings" and `back-quoted strings`, where quotes
//     may be escaped with a backslash or by doubling;
//   - inside % line comments and /* block comments */;
//   - in 0'c character-code literals, where the character after the quote is
//     data (0'. and 0” are both legal).
func SplitClauses(src string) ([]string, error) {
	var (
		out  []string
		cur  strings.Builder
		rs   = []rune(src)
		n    = len(rs)
		i    int
		prev rune // previous significant rune, for disambiguating 0'c
	)
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, t)
		}
		cur.Reset()
	}
	for i < n {
		c := rs[i]
		switch {
		case c == '%':
			for i < n && rs[i] != '\n' {
				cur.WriteRune(rs[i])
				i++
			}

		case c == '/' && i+1 < n && rs[i+1] == '*':
			j := i + 2
			for j+1 < n && (rs[j] != '*' || rs[j+1] != '/') {
				j++
			}
			if j+1 >= n {
				return nil, fmt.Errorf("unterminated block comment")
			}
			cur.WriteString(string(rs[i : j+2]))
			i = j + 2

		case c == '0' && i+1 < n && rs[i+1] == '\'' && !isAlnum(prev):
			cur.WriteString("0'")
			i += 2
			switch {
			case i >= n:
			case rs[i] == '\\' && i+1 < n: // escape sequence
				cur.WriteRune(rs[i])
				cur.WriteRune(rs[i+1])
				i += 2
			case rs[i] == '\'' && i+1 < n && rs[i+1] == '\'': // 0'' is a quote
				cur.WriteString("''")
				i += 2
			default:
				cur.WriteRune(rs[i])
				i++
			}
			prev = 'x'

		case c == '\'' || c == '"' || c == '`':
			q := c
			cur.WriteRune(c)
			i++
			for {
				if i >= n {
					return nil, fmt.Errorf("unterminated quoted token (%c)", q)
				}
				if rs[i] == '\\' && i+1 < n {
					cur.WriteRune(rs[i])
					cur.WriteRune(rs[i+1])
					i += 2
					continue
				}
				if rs[i] == q {
					if i+1 < n && rs[i+1] == q { // doubled quote escapes itself
						cur.WriteRune(q)
						cur.WriteRune(q)
						i += 2
						continue
					}
					cur.WriteRune(q)
					i++
					break
				}
				cur.WriteRune(rs[i])
				i++
			}
			prev = 'x'

		case c == '.' && (i+1 >= n || isLayout(rs[i+1]) || rs[i+1] == '%'):
			cur.WriteRune('.')
			i++
			flush()
			prev = 0

		default:
			cur.WriteRune(c)
			if !isLayout(c) {
				prev = c
			}
			i++
		}
	}
	if t := strings.TrimSpace(cur.String()); t != "" && !isAllComment(t) {
		return nil, fmt.Errorf("clause is not terminated by '.': %s", truncate(t, 120))
	}
	return out, nil
}

func isLayout(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v'
}

func isAlnum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// isAllComment reports whether s consists only of comment lines, which is the
// one thing that may legitimately follow the last clause of a unit.
func isAllComment(s string) bool {
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "%") {
			return false
		}
	}
	return true
}

// stripComments reduces a clause to its code, on one line, for cheap
// inspection of its leading token.
func stripComments(s string) string {
	var out []string
	for line := range strings.SplitSeq(s, "\n") {
		if i := strings.Index(line, "%"); i >= 0 {
			line = line[:i]
		}
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return strings.TrimSpace(strings.Join(out, " "))
}

// truncate shortens s to at most limit runes, so that a diagnostic quoting
// agent-supplied source stays readable and valid UTF-8.
func truncate(s string, limit int) string {
	rs := []rune(s)
	if len(rs) <= limit {
		return s
	}
	return string(rs[:limit]) + "..."
}
