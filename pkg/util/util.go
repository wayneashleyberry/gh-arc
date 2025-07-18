// Package util provides utility functions for file discovery and related
// helpers used across the project.
package util

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// FindFiles searches recursively from the current directory for files with the
// given name. It returns a slice of matching file paths or an error if
// directory traversal fails. Logging is performed for each found file using
// slog with the provided context.
func FindFiles(ctx context.Context, name string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("error accessing path %s: %w", path, err)
		}

		if !d.IsDir() && d.Name() == name {
			files = append(files, path)

			slog.DebugContext(ctx, "found "+name+" file", slog.String("path", path))
		}

		return nil
	})
	if err != nil {
		return files, fmt.Errorf("error walking directories: %w", err)
	}

	return files, nil
}
