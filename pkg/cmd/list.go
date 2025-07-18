package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/patrickmn/go-cache"
	"golang.org/x/mod/modfile"
)

func findGoModFiles() ([]string, error) {
	// Find all go.mod files recursively
	var goModFiles []string

	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("error accessing path %s: %w", path, err)
		}

		if !d.IsDir() && d.Name() == "go.mod" {
			goModFiles = append(goModFiles, path)
		}

		return nil
	})
	if err != nil {
		return goModFiles, fmt.Errorf("error walking directories: %w", err)
	}

	return goModFiles, nil
}

// archivedPrinter encapsulates printing and counting archived repos.
type archivedPrinter struct {
	count int64
	mu    sync.Mutex
}

func (ap *archivedPrinter) Print(goModPath, repo, pushedAt string, indirect bool) {
	if indirect {
		fmt.Printf("%s: https://github.com/%s (last push: %s) // indirect\n", goModPath, repo, pushedAt)
	} else {
		fmt.Printf("%s: https://github.com/%s (last push: %s)\n", goModPath, repo, pushedAt)
	}
	ap.mu.Lock()
	ap.count++
	ap.mu.Unlock()
}

func (ap *archivedPrinter) Count() int {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	return int(ap.count)
}

// ListArchivedGoModules lists archived Go modules, optionally including indirect ones. Returns the count of archived repos found.
func ListArchivedGoModules(ctx context.Context, checkIndirect bool) (int, error) {
	goModFileNames, err := findGoModFiles()
	if err != nil {
		return 0, fmt.Errorf("failed to find go.mod files: %w", err)
	}

	// repoKey: github.com/owner/repo, value: slice of info structs
	type repoInfo struct {
		indirect  bool
		goModPath string
	}

	repos := map[string][]repoInfo{}

	for _, name := range goModFileNames {
		data, err := os.ReadFile(name)
		if err != nil {
			slog.DebugContext(ctx, fmt.Sprintf("could not open %s: %v", name, err))

			continue
		}

		mf, err := modfile.Parse(name, data, nil)
		if err != nil {
			slog.DebugContext(ctx, fmt.Sprintf("failed to parse %s: %v", name, err))

			continue
		}

		for _, req := range mf.Require {
			if strings.HasPrefix(req.Mod.Path, "github.com/") {
				parts := strings.Split(req.Mod.Path, "/")
				if len(parts) >= 3 {
					repo := fmt.Sprintf("%s/%s", parts[1], parts[2])

					repos[repo] = append(repos[repo], repoInfo{req.Indirect, name})
				}
			}
		}

		for _, rep := range mf.Replace {
			if strings.HasPrefix(rep.New.Path, "github.com/") {
				parts := strings.Split(rep.New.Path, "/")
				if len(parts) >= 3 {
					repo := fmt.Sprintf("%s/%s", parts[1], parts[2])
					// If replaced repo is already in repos for this go.mod, skip
					found := false

					for _, info := range repos[repo] {
						if info.goModPath == name {
							found = true

							break
						}
					}

					if !found {
						repos[repo] = append(repos[repo], repoInfo{false, name})
					}
				}
			}
		}
	}

	if len(repos) == 0 {
		slog.DebugContext(ctx, "no github.com modules found in any go.mod file")

		return 0, nil
	}

	client, err := api.DefaultRESTClient()
	if err != nil {
		return 0, fmt.Errorf("failed to create github api client: %w", err)
	}

	// Set up cache with default expiration 1 hour, cleanup interval 2 hours
	c := cache.New(1*time.Hour, 2*time.Hour)

	var wg sync.WaitGroup

	type repoResult struct {
		Archived bool   `json:"archived"`
		PushedAt string `json:"pushed_at"`
	}

	ap := &archivedPrinter{}

	for repo, infos := range repos {
		// If checkIndirect is false and all infos are indirect, skip this repo entirely
		if !checkIndirect {
			onlyIndirect := true
			for _, info := range infos {
				if !info.indirect {
					onlyIndirect = false
					break
				}
			}
			if onlyIndirect {
				continue
			}
		}

		wg.Add(1)

		go func(repo string, infos []repoInfo) {
			defer wg.Done()

			// Check cache first
			if cached, found := c.Get(repo); found {
				result := cached.(repoResult)
				if result.Archived {
					for _, info := range infos {
						if !checkIndirect && info.indirect {
							continue
						}
						ap.Print(info.goModPath, repo, result.PushedAt, info.indirect)
					}
				}

				return
			}

			ownerRepo := strings.Split(repo, "/")
			if len(ownerRepo) != 2 {
				return
			}

			var result repoResult

			path := fmt.Sprintf("repos/%s/%s", ownerRepo[0], ownerRepo[1])

			err := client.Get(path, &result)
			if err != nil {
				slog.DebugContext(ctx, fmt.Sprintf("error fetching repo %s: %v", repo, err))

				return
			}

			// Store in cache
			c.Set(repo, result, cache.DefaultExpiration)

			if result.Archived {
				for _, info := range infos {
					if !checkIndirect && info.indirect {
						continue
					}
					ap.Print(info.goModPath, repo, result.PushedAt, info.indirect)
				}
			}
		}(repo, infos)
	}

	wg.Wait()

	return ap.Count(), nil
}
