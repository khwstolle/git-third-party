package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestV0_0_1_Compatibility(t *testing.T) {
	host := makeRepo(t)
	// V0.0.1 style config (manual dir creation, then add).
	_ = os.MkdirAll(filepath.Join(host, "vendor/old"), 0755)
	upstream, _ := makeUpstream(t, map[string]string{"a": "a"})

	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{}, upstream, "main", "", "vendor/old", "", nil, nil, true); err != nil {
			t.Fatal(err)
		}
	})

	if _, err := os.Stat(filepath.Join(host, "vendor/old/a")); err != nil {
		t.Errorf("v0.0.1 style add failed: %v", err)
	}
}
