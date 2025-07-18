// Package gomod provides commands for scanning Go module dependencies and
// reporting archived GitHub repositories.
package gomod

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/wayneashleyberry/gh-arc/pkg/client"
	"github.com/wayneashleyberry/gh-arc/pkg/util"
	"golang.org/x/mod/modfile"
)

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

// ListArchived lists archived Go modules, optionally including
// indirect ones. Returns the count of archived repos found.
func ListArchived(ctx context.Context, checkIndirect bool) (int, error) {
	goModFileNames, err := util.FindFiles(ctx, "go.mod")
	if err != nil {
		return 0, fmt.Errorf("failed to find go.mod files: %w", err)
	}

	type repoInfo struct {
		indirect  bool
		goModPath string
	}

	repos := map[string][]repoInfo{}

	for _, name := range goModFileNames {
		data, err := os.ReadFile(name) // #nosec G304
		if err != nil {
			slog.DebugContext(ctx, fmt.Sprintf("could not open %s: %v", name, err))

			continue
		}

		mf, err := modfile.Parse(name, data, nil)
		if err != nil {
			slog.DebugContext(ctx, fmt.Sprintf("failed to parse %s: %v", name, err))

			continue
		}

		addDep := func(modPath string, indirect bool) {
			if !strings.HasPrefix(modPath, "github.com/") {
				return
			}

			parts := strings.Split(modPath, "/")
			if len(parts) < 3 {
				return
			}

			repo := fmt.Sprintf("%s/%s", parts[1], parts[2])
			repos[repo] = append(repos[repo], repoInfo{indirect, name})
		}

		for _, req := range mf.Require {
			addDep(req.Mod.Path, req.Indirect)
		}

		for _, rep := range mf.Replace {
			if !strings.HasPrefix(rep.New.Path, "github.com/") {
				continue
			}

			parts := strings.Split(rep.New.Path, "/")
			if len(parts) < 3 {
				continue
			}

			repo := fmt.Sprintf("%s/%s", parts[1], parts[2])

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

	if len(repos) == 0 {
		slog.DebugContext(ctx, "no github.com modules found in any go.mod file")

		return 0, nil
	}

	client, err := client.New()
	if err != nil {
		return 0, fmt.Errorf("failed to create github api client: %w", err)
	}

	var wg sync.WaitGroup

	ap := &archivedPrinter{}

	for repo, infos := range repos {
		// Skip this repository if the user does not want to include indirect
		// dependencies and all references to this repository are indirect. This
		// ensures that only directly required repositories are processed unless
		// indirects are explicitly requested.
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

			result, err := client.GetRepoResult(repo)
			if err != nil {
				slog.DebugContext(ctx, fmt.Sprintf("error fetching repo %s: %v", repo, err))

				return
			}

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
