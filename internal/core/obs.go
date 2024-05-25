package core

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Custom slog levels extending the standard four; Trace is for per-subprocess shell scripts.
const (
	LevelTrace = slog.Level(-8)
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// parseLevel maps a level name to slog.Level; empty string returns Info.
func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return LevelInfo, nil
	case "trace":
		return LevelTrace, nil
	case "debug":
		return LevelDebug, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	}
	return 0, userErrorf("unknown log level %q (want one of: trace, debug, info, warn, error)", s)
}

// levelFromVerbosity maps -v repeat-count to a slog level. 0 = Info (the
// default), 1 = Debug, 2+ = Trace. quiet (== -q) is the inverse and bumps
// up to Warn.
func levelFromVerbosity(v int, quiet bool) slog.Level {
	if v > 0 {
		// Explicit -v always wins over -q.
		switch {
		case v >= 2:
			return LevelTrace
		default:
			return LevelDebug
		}
	}
	if quiet {
		return LevelWarn
	}
	return LevelInfo
}

// configureLogging builds the slog handler from resolved settings and
// installs it on the App. Always writes to stderr — stdout is reserved
// for tool output (list, change-staged messages, JSON). Returns a
// UserError if log-level/log-format are unrecognized (so callers can
// surface a clear message instead of silently defaulting to Info).
func (a *App) configureLogging(s *settings) error {
	var w = a.Stderr
	if w == nil {
		w = os.Stderr
	}
	level, err := parseLevel(s.LogLevel)
	if err != nil {
		return err
	}

	format := strings.ToLower(s.LogFormat)
	var handler slog.Handler
	switch format {
	case "", "text":
		handler = newPrettyHandler(w, level, s.colorEnabled(w))
	case "json":
		handler = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	default:
		return userErrorf("unknown log format %q (want one of: text, json)", s.LogFormat)
	}
	a.Logger = slog.New(handler)
	a.LogLevel = level
	slog.SetDefault(a.Logger)
	return nil
}

// prettyHandler is a minimal text handler for the default (terminal) case.
// One line per record: `<LEVEL> <message> [key=value …]`. Color is applied
// to the level tag when enabled.
type prettyHandler struct {
	w     io.Writer
	level slog.Level
	color bool
	attrs []slog.Attr
}

func newPrettyHandler(w io.Writer, level slog.Level, color bool) slog.Handler {
	return &prettyHandler{w: w, level: level, color: color}
}

func (h *prettyHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *prettyHandler) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	tag, col := levelTag(r.Level)
	if h.color && col != "" {
		sb.WriteString(col)
		sb.WriteString(tag)
		sb.WriteString("\x1b[0m")
	} else {
		sb.WriteString(tag)
	}
	sb.WriteByte(' ')
	sb.WriteString(r.Message)
	for _, a := range h.attrs {
		sb.WriteByte(' ')
		sb.WriteString(a.Key)
		sb.WriteByte('=')
		sb.WriteString(formatAttr(a.Value))
	}
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteByte(' ')
		sb.WriteString(a.Key)
		sb.WriteByte('=')
		sb.WriteString(formatAttr(a.Value))
		return true
	})
	sb.WriteByte('\n')
	_, err := io.WriteString(h.w, sb.String())
	return err
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &c
}

func (h *prettyHandler) WithGroup(_ string) slog.Handler { return h }

func levelTag(l slog.Level) (string, string) {
	switch {
	case l <= LevelTrace:
		return "TRACE", "\x1b[90m"
	case l <= LevelDebug:
		return "DEBUG", "\x1b[36m"
	case l <= LevelInfo:
		return "INFO ", ""
	case l <= LevelWarn:
		return "WARN ", "\x1b[33m"
	default:
		return "ERROR", "\x1b[31m"
	}
}

func formatAttr(v slog.Value) string {
	s := v.String()
	if strings.ContainsAny(s, " \t\"") {
		return shlexQuote(s)
	}
	return s
}
