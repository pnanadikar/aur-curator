package domain

import (
	"encoding/json"
	"time"
)

// PolicyConfig stores persisted policy override configuration.
type PolicyConfig struct {
	ID PolicyConfigID

	WorkspaceID WorkspaceID

	Overrides json.RawMessage

	Version string

	UpdatedAt time.Time
}
