package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/patrickmn/go-cache"
	"golang.org/x/mod/modfile"
)

const indirectDepType = "indirect"

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

func (ap *archivedPrinter) Print(goModPath, repo, pushedAt, depType string) {
	if depType == indirectDepType {
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
func ListArchivedGoModules(checkIndirect bool) (int, error) {
	goModFiles, err := findGoModFiles()
	if err != nil {
		return 0, fmt.Errorf("failed to find go.mod files: %w", err)
	}

	// repoKey: github.com/owner/repo, value: slice of info structs
	type repoInfo struct {
		depType   string
		goModPath string
	}

	repos := map[string][]repoInfo{}

	for _, goModPath := range goModFiles {
		data, err := os.ReadFile(goModPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not open %s: %v\n", goModPath, err)

			continue
		}

		mf, err := modfile.Parse(goModPath, data, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to parse %s: %v\n", goModPath, err)

			continue
		}

		for _, req := range mf.Require {
			if strings.HasPrefix(req.Mod.Path, "github.com/") {
				parts := strings.Split(req.Mod.Path, "/")
				if len(parts) >= 3 {
					repo := fmt.Sprintf("%s/%s", parts[1], parts[2])

					depType := "direct"
					if req.Indirect {
						depType = indirectDepType
					}

					repos[repo] = append(repos[repo], repoInfo{depType, goModPath})
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
						if info.goModPath == goModPath {
							found = true

							break
						}
					}

					if !found {
						repos[repo] = append(repos[repo], repoInfo{"direct", goModPath})
					}
				}
			}
		}
	}

	if len(repos) == 0 {
		fmt.Println("No github.com modules found in any go.mod file.")

		return 0, nil
	}

	client, err := api.DefaultRESTClient()
	if err != nil {
		return 0, fmt.Errorf("failed to create GitHub API client: %w", err)
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
				if info.depType == "direct" {
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
						if !checkIndirect && info.depType == indirectDepType {
							continue
						}

						ap.Print(info.goModPath, repo, result.PushedAt, info.depType)
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
				fmt.Fprintf(os.Stderr, "error fetching repo %s: %v\n", repo, err)

				return
			}

			// Store in cache
			c.Set(repo, result, cache.DefaultExpiration)

			if result.Archived {
				for _, info := range infos {
					if !checkIndirect && info.depType == indirectDepType {
						continue
					}

					ap.Print(info.goModPath, repo, result.PushedAt, info.depType)
				}
			}
		}(repo, infos)
	}

	wg.Wait()

	return ap.Count(), nil
}
