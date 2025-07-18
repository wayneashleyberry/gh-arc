package gomod

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func captureStdout(t *testing.T, f func()) string {
	t.Helper()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	_ = w.Close()

	os.Stdout = old

	var buf bytes.Buffer

	_, _ = io.Copy(&buf, r)

	return buf.String()
}

func TestArchivedPrinter_Print_Direct(t *testing.T) {
	t.Parallel()

	ap := &archivedPrinter{}
	out := captureStdout(t, func() {
		ap.Print("foo/go.mod", "owner/repo", "2025-07-18T12:00:00Z", false)
	})

	expected := "foo/go.mod: https://github.com/owner/repo (last push: 2025-07-18T12:00:00Z)\n"
	require.Equal(t, expected, out)
	require.Equal(t, 1, ap.Count())
}

func TestArchivedPrinter_Print_Indirect(t *testing.T) {
	t.Parallel()

	ap := &archivedPrinter{}
	out := captureStdout(t, func() {
		ap.Print("bar/go.mod", "owner/repo", "2025-07-18T12:00:00Z", true)
	})

	expected := "bar/go.mod: https://github.com/owner/repo (last push: 2025-07-18T12:00:00Z) // indirect\n"
	require.Equal(t, expected, out)
	require.Equal(t, 1, ap.Count())
}

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)

	err := os.WriteFile(path, []byte(content), 0o644) //nolint: gosec
	require.NoError(t, err, "failed to write temp file")

	return path
}

func TestDiscoverGitHubDependencies(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()

	// Create a go.mod file with direct and indirect github.com dependencies
	goModContent := `module example.com/foo

require (
	github.com/wayneashleyberry/gh-arc v1.2.3
	github.com/other/repo v0.1.0 // indirect
	golang.org/x/tools v0.1.0
)
replace github.com/wayneashleyberry/gh-arc => github.com/wayneashleyberry/gh-arc v1.2.4
`
	goModPath := writeTempFile(t, dir, "go.mod", goModContent)

	// Create another go.mod file with a different github.com dependency
	goModContent2 := `module example.com/bar

require (
	github.com/foo/bar v0.2.0
)
`
	goModPath2 := writeTempFile(t, dir, "go2.mod", goModContent2)

	files := []string{goModPath, goModPath2}
	repos := DiscoverGitHubDependencies(ctx, files)

	// Should find wayneashleyberry/gh-arc and other/repo and foo/bar
	require.Len(t, repos, 3, "expected 3 repos")

	// Check for wayneashleyberry/gh-arc
	infos, ok := repos["wayneashleyberry/gh-arc"]
	require.True(t, ok, "expected wayneashleyberry/gh-arc in repos")

	foundDirect := false

	for _, info := range infos {
		if info.goModPath == goModPath && !info.indirect {
			foundDirect = true
		}
	}

	require.True(t, foundDirect, "expected direct dependency for wayneashleyberry/gh-arc")

	// Check for other/repo (indirect)
	infos, ok = repos["other/repo"]
	require.True(t, ok, "expected other/repo in repos")

	foundIndirect := false

	for _, info := range infos {
		if info.goModPath == goModPath && info.indirect {
			foundIndirect = true
		}
	}

	require.True(t, foundIndirect, "expected indirect dependency for other/repo")

	// Check for foo/bar in second file
	infos, ok = repos["foo/bar"]
	require.True(t, ok, "expected foo/bar in repos")

	found := false

	for _, info := range infos {
		if info.goModPath == goModPath2 && !info.indirect {
			found = true
		}
	}

	require.True(t, found, "expected direct dependency for foo/bar in go2.mod")

	// Should not include non-github.com modules
	for repo := range repos {
		require.Contains(t, repo, "/", "unexpected repo key: %s", repo)
	}
}
