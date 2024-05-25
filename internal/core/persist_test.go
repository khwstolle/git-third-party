package core

import (
	"context"
	"strings"
	"testing"
)

func TestPersistRoundTrip(t *testing.T) {
	host := makeRepo(t)
	items := []*ConfigItem{
		{
			Dir:    "vendor/a",
			URL:    "https://example.com/a",
			Follow: "main",
		},
		{
			Dir:    "vendor/b",
			URL:    "https://example.com/b",
			Pin:    "v1.2.3",
			Subdir: "src",
		},
	}

	withRepo(t, host, func(ctx context.Context, a *App) {
		gopt := GlobalOptions{}
		if err := a.writeConfigAndLock(ctx, gopt, items); err != nil {
			t.Fatal(err)
		}
		got, err := a.readConfig(ctx, gopt)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d items; want 2", len(got))
		}
		if got[0].Dir != "vendor/a" || got[1].Dir != "vendor/b" {
			t.Errorf("round-trip failed: %+v", got)
		}
	})
}

func TestPersistRejectsEmptyDir(t *testing.T) {
	app := NewApp(nil, nil)
	ctx := context.Background()
	gopt := GlobalOptions{}
	body := []byte(`[[third_party]]
url = "https://x"
follow = "y"
`)
	_, err := app.readNewConfig(ctx, gopt, body, "test.toml")
	if err == nil || !strings.Contains(err.Error(), "missing required dir") {
		t.Errorf("expected missing-dir error; got %v", err)
	}
}

func TestPersistRejectsDuplicateDir(t *testing.T) {
	app := NewApp(nil, nil)
	ctx := context.Background()
	gopt := GlobalOptions{}
	body := []byte(`[[third_party]]
dir = "x"
url = "https://u1"
follow = "f1"

[[third_party]]
dir = "x"
url = "https://u2"
follow = "f2"
`)
	_, err := app.readNewConfig(ctx, gopt, body, "test.toml")
	if err == nil || !strings.Contains(err.Error(), "multiple sections with the same dir") {
		t.Errorf("expected duplicate-dir error; got %v", err)
	}
}
