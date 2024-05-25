package core

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestBridgeJSONError(t *testing.T) {
	got := RunBridgeJSON(`{ "repo_path": "/bad/path" }`, nil)
	var resp bridgeResponse
	if err := json.Unmarshal([]byte(got), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ExitCode == 0 {
		t.Errorf("expected non-zero exit code for bad path")
	}
}

func TestBridgeRestoresCWDAndOpts(t *testing.T) {
	startWD, _ := os.Getwd()
	tmp := t.TempDir()

	// Dispatch that changes CWD.
	RunBridgeJSON(`{ "repo_path": "`+tmp+`" }`, func(ctx context.Context, a *App, gopt GlobalOptions, args json.RawMessage) error {
		return nil
	})

	endWD, _ := os.Getwd()
	if endWD != startWD {
		t.Errorf("CWD was not restored: %q -> %q", startWD, endWD)
	}
}

func TestBridgeCapturesOutput(t *testing.T) {
	got := RunBridgeJSON(`{}`, func(ctx context.Context, a *App, gopt GlobalOptions, args json.RawMessage) error {
		_, _ = a.Stdout.Write([]byte("out\n"))
		_, _ = a.Stderr.Write([]byte("err\n"))
		return nil
	})
	var resp bridgeResponse
	_ = json.Unmarshal([]byte(got), &resp)
	if resp.Stdout != "out\n" {
		t.Errorf("stdout wrong: %q", resp.Stdout)
	}
	if resp.Stderr != "err\n" {
		t.Errorf("stderr wrong: %q", resp.Stderr)
	}
}

func TestBridgeHandlesPanic(t *testing.T) {
	got := RunBridgeJSON(`{}`, func(ctx context.Context, a *App, gopt GlobalOptions, args json.RawMessage) error {
		panic("boom")
	})
	var resp bridgeResponse
	_ = json.Unmarshal([]byte(got), &resp)
	if resp.ExitCode == 0 {
		t.Errorf("expected non-zero exit for panic")
	}
	if !strings.Contains(resp.Error, "panic") {
		t.Errorf("expected 'panic' in error; got %q", resp.Error)
	}
}

func TestBridgeJSONOutFieldMapping(t *testing.T) {
	got := RunBridgeJSON(`{"json_out": true}`, func(ctx context.Context, a *App, gopt GlobalOptions, args json.RawMessage) error {
		// results are written to a.Stdout as JSON by the command bodies.
		_, _ = a.Stdout.Write([]byte(`[{"dir": "v/x", "action": "added"}]`))
		return nil
	})
	var resp bridgeResponse
	_ = json.Unmarshal([]byte(got), &resp)
	if resp.Stdout != "" {
		t.Errorf("stdout should be empty when json_out is true and results are populated; got %q", resp.Stdout)
	}
	// Note: RunBridgeJSON might re-marshal or normalize JSON.
	if !strings.Contains(string(resp.Results), `"dir":"v/x"`) {
		t.Errorf("results wrong: %s", resp.Results)
	}
}

func TestDispatchAdd(t *testing.T) {
	// Clear git environment variables to avoid interference when running
	// tests from inside a git hook (where GIT_DIR, etc. are set).
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "GIT_") {
			key := strings.SplitN(env, "=", 2)[0]
			old, ok := os.LookupEnv(key)
			_ = os.Unsetenv(key)
			if ok {
				t.Cleanup(func() { _ = os.Setenv(key, old) })
			}
		}
	}

	host := makeRepo(t)
	upstream, _ := makeUpstream(t, map[string]string{"a": "a"})

	args := addArgs{
		URL:    upstream,
		Follow: "main",
		Dir:    "vendor/x",
	}
	raw, _ := json.Marshal(args)

	req := bridgeRequest{
		RepoPath: host,
		Args:     raw,
	}
	reqJSON, _ := json.Marshal(req)

	got := RunBridgeJSON(string(reqJSON), DispatchAdd)
	var resp bridgeResponse
	_ = json.Unmarshal([]byte(got), &resp)

	if resp.ExitCode != 0 {
		t.Fatalf("DispatchAdd failed: %s\nStderr: %s", resp.Error, resp.Stderr)
	}
	if _, err := os.Stat(host + "/vendor/x/a"); err != nil {
		t.Errorf("file not added to host: %v", err)
	}
}
