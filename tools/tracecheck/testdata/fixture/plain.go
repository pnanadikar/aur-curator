package fixture

import (
	"context"

	"github.com/pramod/aur-curator/internal/trace"
)

type other struct{}

func (other) Log(args ...any) {}

func plain(ctx context.Context, kv []any) {
	trace.Log(ctx, "event", "pkg", "yay")              // well-formed
	trace.Log(ctx, "event", "LABEL_PLAIN", "pkg", "x") // malformed: bare label
	trace.Log(ctx, "event", kv...)                     // spread: skipped

	var o other
	o.Log(ctx, "event", "odd") // not trace.Log: ignored
}
