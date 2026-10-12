package logger

import (
	"context"
	"log/slog"
	"strings"
	"sync"
)

// CaptureSlogHandler records slog records so tests can assert on log output.
// It is a test helper exported from this package following the
// db.PrepareTestDB pattern: any package's _test.go files can build a logger
// that writes into it and then inspect what was logged.
type CaptureSlogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *CaptureSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *CaptureSlogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r)
	h.mu.Unlock()
	return nil
}

func (h *CaptureSlogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *CaptureSlogHandler) WithGroup(string) slog.Handler      { return h }

// Records returns a copy of the captured records.
func (h *CaptureSlogHandler) Records() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]slog.Record(nil), h.records...)
}

// ContainsWarn reports whether any WARN record's message contains msgSubstr.
func (h *CaptureSlogHandler) ContainsWarn(msgSubstr string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, msgSubstr) {
			return true
		}
	}
	return false
}

// ContainsError reports whether any ERROR record's message contains msgSubstr.
func (h *CaptureSlogHandler) ContainsError(msgSubstr string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level == slog.LevelError && strings.Contains(r.Message, msgSubstr) {
			return true
		}
	}
	return false
}

// ContainsErrorAttr reports whether any ERROR record has an attribute whose
// key equals key and whose string value equals want.
func (h *CaptureSlogHandler) ContainsErrorAttr(key, want string) bool {
	return h.containsAttr(slog.LevelError, key, want)
}

// ContainsInfoAttr reports whether any INFO record has an attribute whose
// key equals key and whose string value equals want.
func (h *CaptureSlogHandler) ContainsInfoAttr(key, want string) bool {
	return h.containsAttr(slog.LevelInfo, key, want)
}

func (h *CaptureSlogHandler) containsAttr(level slog.Level, key, want string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level != level {
			continue
		}
		found := false
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == key && a.Value.String() == want {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}
