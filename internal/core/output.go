package core

import (
	"encoding/json"
	"io"
)

// entryResult is the structured form of a command's effect on a single
// vendored entry. Emitted as one JSON record per affected entry when
// --json is set; commands that affect zero-or-more entries emit an
// array of these.
//
// Action vocabulary:
//
//	added            — new entry created
//	updated          — existing entry's commit/lockfile/work tree changed
//	would-update     — dry-run; mutation skipped but pending
//	would-discard    — dry-run; would discard local modifications
//	up-to-date       — already in sync; no-op
//	removed          — entry unregistered and content rm'd
//	renamed          — entry moved to NewDir
//	saved            — patch saved (experimental)
//	saved-up-to-date — patch save was a no-op
//	patched          — update applied a recorded patch cleanly
//	conflicts        — update produced a `-conflicts` tree-patch
//	unchanged        — info/list-style observation, no mutation attempted
type entryResult struct {
	Dir        string `json:"dir"`
	Action     string `json:"action"`
	URL        string `json:"url,omitempty"`
	FromCommit string `json:"from_commit,omitempty"`
	ToCommit   string `json:"to_commit,omitempty"`
	NewDir     string `json:"new_dir,omitempty"`
	TreePatch  string `json:"tree_patch,omitempty"`
	Conflicts  bool   `json:"conflicts,omitempty"`
	DryRun     bool   `json:"dry_run,omitempty"`
	// Diff is populated by `patch diff --json` and is the textual
	// `git diff <old-tree> <new-tree>` output captured into the JSON
	// record. Empty in all other contexts.
	Diff string `json:"diff,omitempty"`
}

// changed answers whether the result represents an actual or planned
// mutation (used by `update --check` to decide its exit code).
func (r entryResult) changed() bool {
	switch r.Action {
	case "added", "updated", "would-update", "would-discard",
		"removed", "renamed", "saved", "patched", "conflicts":
		return true
	}
	return false
}

func emitJSONResults(w io.Writer, results []entryResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if results == nil {
		results = []entryResult{}
	}
	return enc.Encode(results)
}
