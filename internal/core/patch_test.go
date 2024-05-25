package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestPatchApplyTreePatch(t *testing.T) {
	host := makeRepo(t)
	withRepo(t, host, func(ctx context.Context, app *App) {
		gopt := GlobalOptions{}

		// Helper to make a tree and return its hash.
		mkTree := func(files map[string]string) string {
			var lines []string
			for name, content := range files {
				r, _ := app.git(ctx, gopt, []string{"hash-object", "-w", "--stdin"}, modeSingleLine, gitOpts{input: []byte(content)})
				lines = append(lines, fmt.Sprintf("100644 blob %s\t%s", r.line, name))
			}
			var input strings.Builder
			for _, l := range lines {
				input.WriteString(l)
				input.WriteByte(0)
			}
			r, _ := app.git(ctx, gopt, []string{"mktree", "-z"}, modeSingleLine, gitOpts{input: []byte(input.String())})
			return r.line
		}

		base := mkTree(map[string]string{"a.txt": "base\n", "b.txt": "base\n"})
		old := mkTree(map[string]string{"a.txt": "base\n", "b.txt": "base\n"})
		new := mkTree(map[string]string{"a.txt": "base\n", "b.txt": "edit\n"})

		merged, conflicts, err := app.applyTreePatch(ctx, gopt, base, old, new, "vendor/x")
		if err != nil {
			t.Fatal(err)
		}
		if conflicts {
			t.Errorf("unexpected conflicts")
		}

		// Verify merged tree content.
		r, _ := app.git(ctx, gopt, []string{"ls-tree", merged}, modeNewlineTerminatedLines, gitOpts{})
		found := false
		for _, line := range r.lines {
			if strings.HasSuffix(line, "\tb.txt") {
				sp := strings.Fields(line)
				obj := sp[2]
				blob, _ := app.git(ctx, gopt, []string{"cat-file", "blob", obj}, modeRawBytes, gitOpts{})
				if string(blob.bytes) != "edit\n" {
					t.Errorf("merged content wrong: %q", blob.bytes)
				}
				found = true
			}
		}
		if !found {
			t.Errorf("b.txt not found in merged tree; got lines: %v", r.lines)
		}
	})
}
