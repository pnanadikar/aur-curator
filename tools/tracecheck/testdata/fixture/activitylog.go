package fixture

import (
	"context"

	aurlog "fake/import/path/internal/activitylog"
)

type fakeActivitylogCtx context.Context

func activitylogCalls(ctx fakeActivitylogCtx, path string, kv []string) {
	aurlog.Log(ctx, path, "import", "pkg", "status", "success")             // well-formed
	aurlog.Log(ctx, path, "import", "pkg", "status", "fail", "ACTIVITYLOG") // odd: bare label
	aurlog.Log(ctx, path, "import", "pkg", kv...)                           // spread: skipped
}
