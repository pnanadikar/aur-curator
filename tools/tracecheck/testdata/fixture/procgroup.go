package fixture

import (
	"context"
	"os/exec"

	pg "fake/import/path/internal/procgroup"
)

type fakeProcgroupCtx context.Context
type fakeCmd struct{}

func fakeCmdFn() *fakeCmd { return new(fakeCmd) }

func procgroupCalls(ctx fakeProcgroupCtx) {
	_ = pg.Run(ctx, fakeCmdFn(), "key1", "val1", "key2", "val2") // well-formed
	_ = pg.Run(ctx, fakeCmdFn(), "key1", "val1", "PROCGROUP")    // odd: 3 kv args
}
