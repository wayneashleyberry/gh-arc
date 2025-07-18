// Package cmd provides commands for scanning Go module dependencies and
// reporting archived GitHub repositories.
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

func findFiles(ctx context.Context, name string) ([]string, error) {
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

// CachedGitHubClient wraps the GitHub API client and transparently caches repo
// results.
type CachedGitHubClient struct {
	client *api.RESTClient
	cache  *cache.Cache
}

// RepoResult contains metadata about a GitHub repository, including its
// archived status and last push date.
type RepoResult struct {
	Archived bool   `json:"archived"`
	PushedAt string `json:"pushed_at"`
}

// NewCachedGitHubClient creates a new CachedGitHubClient with a default REST
// client and an in-memory cache. The cache is used to store repository metadata
// and reduce redundant API calls. Returns an error if the GitHub API client
// cannot be created.
func NewCachedGitHubClient() (*CachedGitHubClient, error) {
	client, err := api.DefaultRESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub API client: %w", err)
	}

	c := cache.New(1*time.Hour, 2*time.Hour)

	return &CachedGitHubClient{client: client, cache: c}, nil
}

// GetRepoResult returns the archived status and last push date for a GitHub
// repository. It transparently caches results to avoid redundant API calls. The
// repo argument should be in the form "owner/repo".
func (c *CachedGitHubClient) GetRepoResult(repo string) (RepoResult, error) {
	if cached, found := c.cache.Get(repo); found {
		return cached.(RepoResult), nil
	}

	ownerRepo := strings.Split(repo, "/")
	if len(ownerRepo) != 2 {
		return RepoResult{}, fmt.Errorf("invalid repo: %s", repo)
	}

	var result RepoResult

	path := fmt.Sprintf("repos/%s/%s", ownerRepo[0], ownerRepo[1])

	err := c.client.Get(path, &result)
	if err != nil {
		return RepoResult{}, fmt.Errorf("failed to fetch repo %s: %w", repo, err)
	}

	c.cache.Set(repo, result, cache.DefaultExpiration)

	return result, nil
}

// ListArchivedGoModules lists archived Go modules, optionally including
// indirect ones. Returns the count of archived repos found.
func ListArchivedGoModules(ctx context.Context, checkIndirect bool) (int, error) {
	goModFileNames, err := findFiles(ctx, "go.mod")
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

	client, err := NewCachedGitHubClient()
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
