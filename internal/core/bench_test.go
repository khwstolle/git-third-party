package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func BenchmarkFilterTree(b *testing.B) {
	// Simple init since makeRepo calls t.Fatalf.
	runInSimple := func(dir, name string, args ...string) {
		c := exec.Command(name, args...)
		c.Dir = dir
		_ = c.Run()
	}
	tmp := b.TempDir()
	runInSimple(tmp, "git", "init", "-q", "-b", "main")
	prev, _ := os.Getwd()
	_ = os.Chdir(tmp)
	defer func() { _ = os.Chdir(prev) }()

	app := NewApp(nil, nil)
	ctx := context.Background()
	gopt := GlobalOptions{}

	mkBlob := func(content string) string {
		r, _ := app.git(ctx, gopt, []string{"hash-object", "-w", "--stdin"}, modeSingleLine, gitOpts{input: []byte(content)})
		return r.line
	}

	b1 := mkBlob("content\n")
	var input strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&input, "100644 blob %s\tfile%d.txt\x00", b1, i)
	}
	root, _ := app.git(ctx, gopt, []string{"mktree", "-z"}, modeSingleLine, gitOpts{input: []byte(input.String())})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.filterTree(ctx, gopt, root.line, "", nil, nil, 0)
	}
}
