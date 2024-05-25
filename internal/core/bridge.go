package core

// Bridge between the Go internals and a JSON-speaking caller. The C ABI
// shims live in cmd/git-third-party-lib; this file is plain Go so the test
// suite (and any non-cgo caller) can exercise it directly.
//
// Each Dispatch* helper takes a JSON request, runs exactly one do*() under
// captured stdout/stderr and a temporarily chdir'd working directory,
// and returns a JSON response of the form:
//
//	{exit_code, error, stdout, stderr, results}

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
)

type bridgeRequest struct {
	RepoPath  string          `json:"repo_path"`
	DryRun    bool            `json:"dry_run"`
	JSONOut   bool            `json:"json_out"`
	CommitMsg string          `json:"commit_msg"`
	LogLevel  string          `json:"log_level"`
	LogFormat string          `json:"log_format"`
	Color     string          `json:"color"`
	Args      json.RawMessage `json:"args"`
}

type bridgeResponse struct {
	ExitCode int             `json:"exit_code"`
	Error    string          `json:"error,omitempty"`
	Stdout   string          `json:"stdout,omitempty"`
	Stderr   string          `json:"stderr,omitempty"`
	Results  json.RawMessage `json:"results,omitempty"`
}

// DispatchFunc routes a JSON arg blob to one of the *App entry points.
type DispatchFunc func(context.Context, *App, GlobalOptions, json.RawMessage) error

// RunBridgeJSON is the Go-string-only core of the bridge. The cgo entry
// points wrap this; the package's tests call it directly.
func RunBridgeJSON(reqJSON string, dispatch DispatchFunc) string {
	var req bridgeRequest
	if err := json.Unmarshal([]byte(reqJSON), &req); err != nil {
		return marshalResponse(bridgeResponse{ExitCode: ExitUserError, Error: "bad request: " + err.Error()})
	}

	prevDefaultLogger := slog.Default()
	prevWD, _ := os.Getwd()
	defer func() {
		slog.SetDefault(prevDefaultLogger)
		if prevWD != "" {
			_ = os.Chdir(prevWD)
		}
	}()

	var outBuf, errBuf bytes.Buffer
	app := NewApp(&outBuf, &errBuf)

	gopt := GlobalOptions{
		DryRun:    req.DryRun,
		JSONOut:   req.JSONOut,
		CommitMsg: req.CommitMsg,
	}

	s := defaultSettings()
	if req.LogLevel != "" {
		s.LogLevel = req.LogLevel
	}
	if req.LogFormat != "" {
		s.LogFormat = req.LogFormat
	}
	if req.Color != "" {
		s.Color = req.Color
	}
	if err := app.configureLogging(s); err != nil {
		return marshalResponse(bridgeResponse{ExitCode: ExitUserError, Error: err.Error(), Stderr: errBuf.String()})
	}

	if req.RepoPath != "" {
		if err := os.Chdir(req.RepoPath); err != nil {
			return marshalResponse(bridgeResponse{ExitCode: ExitUserError, Error: "chdir: " + err.Error(), Stderr: errBuf.String()})
		}
	}

	ctx := context.Background()
	dispatchErr := safeDispatch(ctx, app, gopt, dispatch, req.Args)

	resp := bridgeResponse{
		Stdout: outBuf.String(),
		Stderr: errBuf.String(),
	}
	switch e := dispatchErr.(type) {
	case nil:
		resp.ExitCode = ExitOK
	case *checkErr:
		resp.ExitCode = ExitCheckDirty
		resp.Error = e.Error()
	case *UserError:
		resp.ExitCode = e.ExitCode
		if resp.ExitCode == 0 {
			resp.ExitCode = ExitUserError
		}
		resp.Error = e.Msg
	default:
		resp.ExitCode = ExitUserError
		resp.Error = dispatchErr.Error()
	}

	if req.JSONOut && len(resp.Stdout) > 0 && json.Valid([]byte(resp.Stdout)) {
		resp.Results = json.RawMessage(resp.Stdout)
		resp.Stdout = ""
	}
	return marshalResponse(resp)
}

func safeDispatch(ctx context.Context, app *App, gopt GlobalOptions, dispatch DispatchFunc, args json.RawMessage) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in bridge dispatch: %v\n%s", r, debug.Stack())
		}
	}()
	return dispatch(ctx, app, gopt, args)
}

func marshalResponse(r bridgeResponse) string {
	b, err := json.Marshal(r)
	if err != nil {
		return `{"exit_code":1,"error":"bridge: marshal response failed"}`
	}
	return string(b)
}

// --- Per-command dispatchers --------------------------------------------------

type addArgs struct {
	URL            string   `json:"url"`
	Follow         string   `json:"follow"`
	Pin            string   `json:"pin"`
	Dir            string   `json:"dir"`
	Subdir         string   `json:"subdir"`
	Include        []string `json:"include"`
	Exclude        []string `json:"exclude"`
	AllowDirExists bool     `json:"allow_dir_exists"`
}

func DispatchAdd(ctx context.Context, a *App, gopt GlobalOptions, raw json.RawMessage) error {
	var args addArgs
	if err := unmarshalArgs(raw, &args); err != nil {
		return err
	}
	return a.add(ctx, gopt, args.URL, args.Follow, args.Pin, args.Dir, args.Subdir, args.Include, args.Exclude, args.AllowDirExists)
}

type setArgs struct {
	URL     string    `json:"url"`
	Follow  string    `json:"follow"`
	Pin     string    `json:"pin"`
	Dir     string    `json:"dir"`
	Subdir  *string   `json:"subdir"`
	Include *[]string `json:"include"`
	Exclude *[]string `json:"exclude"`
}

func DispatchSet(ctx context.Context, a *App, gopt GlobalOptions, raw json.RawMessage) error {
	var args setArgs
	if err := unmarshalArgs(raw, &args); err != nil {
		return err
	}
	return a.set(ctx, gopt, args.URL, args.Follow, args.Pin, args.Dir, args.Subdir, args.Include, args.Exclude)
}

type unsetArgs struct {
	Dir    string   `json:"dir"`
	Fields []string `json:"fields"`
}

func DispatchUnset(ctx context.Context, a *App, gopt GlobalOptions, raw json.RawMessage) error {
	var args unsetArgs
	if err := unmarshalArgs(raw, &args); err != nil {
		return err
	}
	return a.unset(ctx, gopt, args.Dir, args.Fields)
}

type updateArgs struct {
	Dir   string `json:"dir"`
	Check bool   `json:"check"`
}

func DispatchUpdate(ctx context.Context, a *App, gopt GlobalOptions, raw json.RawMessage) error {
	var args updateArgs
	if err := unmarshalArgs(raw, &args); err != nil {
		return err
	}
	return a.update(ctx, gopt, args.Dir, args.Check)
}

type listArgs struct {
	Dir string `json:"dir"`
}

func DispatchList(ctx context.Context, a *App, gopt GlobalOptions, raw json.RawMessage) error {
	var args listArgs
	if err := unmarshalArgs(raw, &args); err != nil {
		return err
	}
	return a.list(ctx, gopt, args.Dir, gopt.JSONOut)
}

type singleDirArgs struct {
	Dir string `json:"dir"`
}

func DispatchRemove(ctx context.Context, a *App, gopt GlobalOptions, raw json.RawMessage) error {
	var args singleDirArgs
	if err := unmarshalArgs(raw, &args); err != nil {
		return err
	}
	return a.remove(ctx, gopt, args.Dir)
}

type renameArgs struct {
	Dir            string `json:"dir"`
	NewDir         string `json:"new_dir"`
	AllowDirExists bool   `json:"allow_dir_exists"`
}

func DispatchRename(ctx context.Context, a *App, gopt GlobalOptions, raw json.RawMessage) error {
	var args renameArgs
	if err := unmarshalArgs(raw, &args); err != nil {
		return err
	}
	return a.rename(ctx, gopt, args.Dir, args.NewDir, args.AllowDirExists)
}

func DispatchSavePatch(ctx context.Context, a *App, gopt GlobalOptions, raw json.RawMessage) error {
	var args singleDirArgs
	if err := unmarshalArgs(raw, &args); err != nil {
		return err
	}
	return a.savePatch(ctx, gopt, args.Dir)
}

func DispatchDiffPatch(ctx context.Context, a *App, gopt GlobalOptions, raw json.RawMessage) error {
	var args singleDirArgs
	if err := unmarshalArgs(raw, &args); err != nil {
		return err
	}
	return a.diffPatch(ctx, gopt, args.Dir)
}

func DispatchInfo(ctx context.Context, a *App, gopt GlobalOptions, raw json.RawMessage) error {
	var args singleDirArgs
	if err := unmarshalArgs(raw, &args); err != nil {
		return err
	}
	return a.info(ctx, gopt, args.Dir)
}

func DispatchInit(ctx context.Context, a *App, gopt GlobalOptions, raw json.RawMessage) error {
	return a.init(ctx, gopt)
}

func unmarshalArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return userErrorf("bridge: invalid args: %s", err.Error())
	}
	return nil
}
