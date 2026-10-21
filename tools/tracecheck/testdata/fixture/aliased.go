package fixture

import (
	"context"

	tr "github.com/pramod/aur-curator/internal/trace"
)

func aliased(ctx context.Context) {
	tr.Log(ctx, "event", "LABEL_ALIASED", "pkg", "x") // malformed, via alias
}
