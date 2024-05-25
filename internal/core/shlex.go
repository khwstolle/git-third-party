package core

import (
	"fmt"
	"strings"
)

// shellJoin quotes each argument with shlexQuote and joins them with spaces.
func shellJoin(args []string) string {
	var sb strings.Builder
	for i, a := range args {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(shlexQuote(a))
	}
	return sb.String()
}

// shlexQuote returns a shell-safe single-quoted string, or s unchanged if it
// consists solely of safe characters.
func shlexQuote(s string) string {
	if s == "" {
		return "''"
	}
	if isShellSafe(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// Safe characters: the set matched by [^\w@%+=:,./-] in ASCII.
func isShellSafe(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '@' || c == '%' || c == '+' || c == '=' ||
			c == ':' || c == ',' || c == '.' || c == '/' || c == '-':
		default:
			return false
		}
	}
	return true
}

// shlexSplit splits s using POSIX shlex rules: whitespace-delimited, single/double
// quoting, backslash escapes inside and outside of quotes.
func shlexSplit(s string) ([]string, error) {
	out := []string{}
	var cur strings.Builder
	inSingle := false
	inDouble := false
	hasToken := false

	i := 0
	for i < len(s) {
		c := s[i]
		if !inSingle && !inDouble {
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v' {
				if hasToken {
					out = append(out, cur.String())
					cur.Reset()
					hasToken = false
				}
				i++
				continue
			}
			if c == '\\' {
				if i+1 >= len(s) {
					return nil, fmt.Errorf("no escaped character")
				}
				cur.WriteByte(s[i+1])
				hasToken = true
				i += 2
				continue
			}
			if c == '\'' {
				inSingle = true
				hasToken = true
				i++
				continue
			}
			if c == '"' {
				inDouble = true
				hasToken = true
				i++
				continue
			}
			cur.WriteByte(c)
			hasToken = true
			i++
			continue
		}
		if inSingle {
			if c == '\'' {
				inSingle = false
				i++
				continue
			}
			cur.WriteByte(c)
			i++
			continue
		}
		// inDouble
		if c == '"' {
			inDouble = false
			i++
			continue
		}
		if c == '\\' {
			if i+1 >= len(s) {
				return nil, fmt.Errorf("no escaped character")
			}
			next := s[i+1]
			// Only $, `, ", \, newline are special inside double quotes (POSIX shlex).
			if next == '$' || next == '`' || next == '"' || next == '\\' || next == '\n' {
				cur.WriteByte(next)
			} else {
				cur.WriteByte(c)
				cur.WriteByte(next)
			}
			i += 2
			continue
		}
		cur.WriteByte(c)
		i++
	}
	if inSingle || inDouble {
		return nil, fmt.Errorf("no closing quotation")
	}
	if hasToken {
		out = append(out, cur.String())
	}
	return out, nil
}
