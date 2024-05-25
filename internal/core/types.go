package core

// ConfigItem is one vendored-directory entry. The fields are the union of
// what the user authors in `third-party.toml` and what the tool resolves
// into `third-party.lock`. They share an in-memory representation so
// callers can mutate either intent or resolution and re-emit both files.
type ConfigItem struct {
	// Intent — written to third-party.toml.
	Dir     string
	URL     string
	Follow  string // branch to track; re-resolved on every update
	Pin     string // tag name or commit SHA; resolved once then frozen
	Subdir  string
	Include []string
	Exclude []string

	// Resolution — written to third-party.lock.
	Commit    string
	TreePatch string
}
