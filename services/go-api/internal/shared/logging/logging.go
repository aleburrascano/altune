package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Setup installs the default slog logger and returns the in-memory ring that
// tees every record for the Mission Control logs panel. Callers that don't need
// the ring (e.g. CLI subcommands) may ignore the return value. Takes the two
// config values it needs rather than *config.Config so logging has no
// intra-shared dependency.
func Setup(logLevel string, development bool) *RingBuffer {
	level := parseLevel(logLevel)

	var base slog.Handler
	if development {
		base = &prettyHandler{
			level:     level,
			w:         os.Stdout,
			addSource: true,
		}
	} else {
		base = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level:     level,
			AddSource: true,
		})
	}

	ring := NewRingBuffer(logRingCapacity)
	slog.SetDefault(slog.New(newRingHandler(base, ring)))
	return ring
}

func parseLevel(s string) slog.Level {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorGray    = "\033[90m"
	colorWhite   = "\033[97m"
)

type prettyHandler struct {
	level     slog.Level
	w         io.Writer
	mu        sync.Mutex
	attrs     []slog.Attr
	group     string
	addSource bool
}

func (h *prettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *prettyHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	timestamp := r.Time.Format("15:04:05.000")
	level, levelColor := formatLevel(r.Level)

	var b strings.Builder

	// timestamp + level + message
	fmt.Fprintf(&b, "%s%s%s %s%-5s%s %s%s%s",
		colorGray, timestamp, colorReset,
		levelColor, level, colorReset,
		colorWhite, r.Message, colorReset,
	)

	// source location (function name + file:line)
	if h.addSource {
		fs := r.PC
		if fs != 0 {
			frames := runtime.CallersFrames([]uintptr{fs})
			f, _ := frames.Next()
			if f.Function != "" {
				funcName := shortFuncName(f.Function)
				file := shortFilePath(f.File)
				fmt.Fprintf(&b, " %s@%s%s %s%s:%d%s",
					colorMagenta, funcName, colorReset,
					colorGray, file, f.Line, colorReset,
				)
			}
		}
	}

	// pre-set attrs (already group-qualified at WithAttrs time)
	for _, a := range h.attrs {
		writeAttr(&b, a, "")
	}
	// record attrs, qualified with the handler's open group
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(&b, a, h.group)
		return true
	})

	b.WriteByte('\n')
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Qualify now so the open group at attach time sticks to these attrs.
	qualified := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		qualified = append(qualified, slog.Attr{Key: h.group + a.Key, Value: a.Value})
	}
	return &prettyHandler{
		level:     h.level,
		w:         h.w,
		attrs:     append(append([]slog.Attr{}, h.attrs...), qualified...),
		group:     h.group,
		addSource: h.addSource,
	}
}

func (h *prettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &prettyHandler{
		level:     h.level,
		w:         h.w,
		attrs:     h.attrs,
		group:     h.group + name + ".",
		addSource: h.addSource,
	}
}

func formatLevel(level slog.Level) (string, string) {
	switch {
	case level >= slog.LevelError:
		return "ERROR", colorRed
	case level >= slog.LevelWarn:
		return "WARN ", colorYellow
	case level >= slog.LevelInfo:
		return "INFO ", colorGreen
	default:
		return "DEBUG", colorBlue
	}
}

// writeAttr renders one attr, prefixing its key with the handler's open group
// (dot-joined). Group-valued attrs flatten to parent.child keys.
func writeAttr(b *strings.Builder, a slog.Attr, prefix string) {
	if a.Equal(slog.Attr{}) {
		return
	}

	key := prefix + a.Key
	val := a.Value.Resolve()
	if val.Kind() == slog.KindGroup {
		for _, ga := range val.Group() {
			writeAttr(b, ga, key+".")
		}
		return
	}

	switch val.Kind() {
	case slog.KindTime:
		fmt.Fprintf(b, " %s%s%s=%s%s%s",
			colorCyan, key, colorReset,
			colorGray, val.Time().Format(time.RFC3339), colorReset)
	case slog.KindDuration:
		fmt.Fprintf(b, " %s%s%s=%s%s%s",
			colorCyan, key, colorReset,
			colorGray, val.Duration(), colorReset)
	default:
		fmt.Fprintf(b, " %s%s%s=%s%s%s",
			colorCyan, key, colorReset,
			colorGray, val.String(), colorReset)
	}
}

func shortFuncName(full string) string {
	// "altune/go-api/internal/catalog/service.(*AddTrackService).Execute"
	// → "AddTrackService.Execute"
	if idx := strings.LastIndex(full, "/"); idx >= 0 {
		full = full[idx+1:]
	}
	if idx := strings.Index(full, "."); idx >= 0 {
		full = full[idx+1:]
	}
	full = strings.TrimPrefix(full, "(*")
	full = strings.TrimSuffix(full, ")")
	full = strings.Replace(full, ").", ".", 1)
	return full
}

func shortFilePath(full string) string {
	parts := strings.Split(filepath.ToSlash(full), "/")
	if len(parts) <= 2 {
		return full
	}
	return strings.Join(parts[len(parts)-2:], "/")
}
