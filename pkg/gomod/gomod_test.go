package gomod

import (
	"bytes"
	"io"
	"os"
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
