package debug

import (
	"fmt"
	"io"
	"sync"
)

var logger = &debugLog{}

type debugLog struct {
	mu      sync.Mutex
	w       io.Writer
	enabled bool
}

// Set enables the logger with the given writer.
func Set(w io.Writer) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	logger.w = w
	logger.enabled = true
}

// Logf writes a namespaced log line. No-op when disabled.
func Logf(tag, format string, args ...any) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if !logger.enabled || logger.w == nil {
		return
	}
	s := fmt.Sprintf("[%s] "+format, append([]any{tag}, args...)...)
	_, _ = logger.w.Write([]byte(s + "\n"))
}
