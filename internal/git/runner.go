package git

import "context"

// Runner executes git commands.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (string, error)
}
