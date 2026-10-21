package git

import "context"

// Forge abstracts remote repository hosting.
type Forge interface {
	CreateRepository(ctx context.Context, name string) (cloneURL string, err error)
	DeleteRepository(ctx context.Context, name string) error
}
