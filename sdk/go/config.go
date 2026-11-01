package atlas

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"atlas.local/sdk/go/protocol/grammar"
)

// maxConfig bounds config.toml.
const maxConfig = 1 << 20

// config is what the client takes from <ATLAS_HOME>/config.toml (§9.7.2):
// the default environment, and each environment's gateway URL.
//
//	env = "dv"
//
//	[environments.dv]
//	url = "https://atlas-dv.example.com"
type config struct {
	Env          string
	Environments map[string]string
}

// parseConfig reads the subset of TOML (v1.0) that config.toml needs, one
// line at a time: comments, tables, and keys (bare, quoted or dotted)
// whose values are basic or literal strings. A key the client does not use
// may hold any one-line value, which it skips; a multi-line value is
// refused, as is anything that is not TOML, with its line.
func parseConfig(b []byte) (config, error) {
	cfg := config{Environments: map[string]string{}}
	if !utf8.Valid(b) {
		return cfg, fmt.Errorf("config.toml is not UTF-8")
	}
	var table []string
	for n, line := range strings.Split(string(b), "\n") {
		fail := func(why string) (config, error) { return cfg, fmt.Errorf("line %d: %s", n+1, why) }
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			if strings.HasPrefix(line, "[[") {
				return fail("arrays of tables are not used by atlas")
			}
			end := strings.IndexByte(line, ']')
			if end < 0 {
				return fail("a table header without its ]")
			}
			if rest := strings.TrimSpace(line[end+1:]); rest != "" && rest[0] != '#' {
				return fail("text after the table header")
			}
			keys, rest, err := parseKey(line[1:end])
			if err != nil || strings.TrimSpace(rest) != "" {
				return fail("a table header is a key")
			}
			table = keys
			continue
		}
		keys, rest, err := parseKey(line)
		if err != nil {
			return fail(err.Error())
		}
		rest = strings.TrimSpace(rest)
		if !strings.HasPrefix(rest, "=") {
			return fail("a key is followed by =")
		}
		rest = strings.TrimSpace(rest[1:])
		path := append(append([]string{}, table...), keys...)
		used := len(path) == 1 && path[0] == "env" || len(path) == 3 && path[0] == "environments" && path[2] == "url"
		if !used {
			if strings.HasPrefix(rest, `"""`) || strings.HasPrefix(rest, "'''") || rest == "" || rest == "[" || rest == "{" {
				return fail("multi-line values are not read by atlas")
			}
			continue
		}
		value, after, err := parseString(rest)
		if err != nil {
			return fail(err.Error())
		}
		if after = strings.TrimSpace(after); after != "" && after[0] != '#' {
			return fail("text after the value")
		}
		if path[0] == "env" {
			if err := grammar.Segment(value); err != nil {
				return fail(fmt.Sprintf("env %q is not an environment name", value))
			}
			cfg.Env = value
			continue
		}
		if err := grammar.Segment(path[1]); err != nil {
			return fail(fmt.Sprintf("%q is not an environment name", path[1]))
		}
		cfg.Environments[path[1]] = value
	}
	return cfg, nil
}

// parseKey reads a key, bare ("a-b_1"), quoted ("a b") or dotted (a."b".c).
func parseKey(s string) ([]string, string, error) {
	var keys []string
	for {
		s = strings.TrimLeft(s, " \t")
		if s == "" {
			return nil, "", fmt.Errorf("a key is missing")
		}
		var k string
		switch s[0] {
		case '"', '\'':
			v, rest, err := parseString(s)
			if err != nil {
				return nil, "", err
			}
			k, s = v, rest
		default:
			i := 0
			for i < len(s) && (s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' || s[i] >= '0' && s[i] <= '9' || s[i] == '-' || s[i] == '_') {
				i++
			}
			if i == 0 {
				return nil, "", fmt.Errorf("%q does not begin with a key", s)
			}
			k, s = s[:i], s[i:]
		}
		keys = append(keys, k)
		s = strings.TrimLeft(s, " \t")
		if !strings.HasPrefix(s, ".") {
			return keys, s, nil
		}
		s = s[1:]
	}
}

// parseString reads a one-line basic ("…", with TOML's escapes) or literal
// ('…') string at the start of s, and returns what follows it.
func parseString(s string) (string, string, error) {
	if s == "" || s[0] != '"' && s[0] != '\'' {
		return "", "", fmt.Errorf("the value is not a string")
	}
	if strings.HasPrefix(s, `"""`) || strings.HasPrefix(s, "'''") {
		return "", "", fmt.Errorf("multi-line strings are not read by atlas")
	}
	if s[0] == '\'' {
		end := strings.IndexByte(s[1:], '\'')
		if end < 0 {
			return "", "", fmt.Errorf("a string without its closing quote")
		}
		return s[1 : 1+end], s[2+end:], nil
	}
	var out strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			return out.String(), s[i+1:], nil
		case c == '\\':
			if i+1 >= len(s) {
				return "", "", fmt.Errorf("an escape at the end of the line")
			}
			i++
			switch s[i] {
			case 'b':
				out.WriteByte('\b')
			case 't':
				out.WriteByte('\t')
			case 'n':
				out.WriteByte('\n')
			case 'f':
				out.WriteByte('\f')
			case 'r':
				out.WriteByte('\r')
			case '"':
				out.WriteByte('"')
			case '\\':
				out.WriteByte('\\')
			case 'u', 'U':
				n := 4
				if s[i] == 'U' {
					n = 8
				}
				if i+n >= len(s) {
					return "", "", fmt.Errorf("a short unicode escape")
				}
				r, err := strconv.ParseUint(s[i+1:i+1+n], 16, 32)
				if err != nil || !utf8.ValidRune(rune(r)) {
					return "", "", fmt.Errorf("a unicode escape that is not a scalar value")
				}
				out.WriteRune(rune(r))
				i += n
			default:
				return "", "", fmt.Errorf("an unknown escape")
			}
		case c < 0x20 && c != '\t' || c == 0x7f:
			return "", "", fmt.Errorf("a control character in a string")
		default:
			out.WriteByte(c)
		}
	}
	return "", "", fmt.Errorf("a string without its closing quote")
}
