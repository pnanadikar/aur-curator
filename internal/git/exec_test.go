package git

import (
	"context"
	"testing"
)

func TestRunnerMissingDirectory(t *testing.T) {
	r := ExecRunner{}
	_, err := r.Run(context.Background(), "/does/not/exist", "status")
	if err == nil {
		t.Fatal("expected error")
	}
}
