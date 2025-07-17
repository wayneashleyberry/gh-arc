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

func ListArchivedGoModules() error {
	// Find all go.mod files recursively
	var goModFiles []string

	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && d.Name() == "go.mod" {
			goModFiles = append(goModFiles, path)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("error walking directories: %w", err)
	}

	if len(goModFiles) == 0 {
		fmt.Println("No go.mod files found.")

		return nil
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
						depType = "indirect"
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

		return nil
	}

	client, err := api.DefaultRESTClient()
	if err != nil {
		return fmt.Errorf("failed to create GitHub API client: %w", err)
	}

	// Set up cache with default expiration 1 hour, cleanup interval 2 hours
	c := cache.New(1*time.Hour, 2*time.Hour)

	var wg sync.WaitGroup

	type repoResult struct {
		Archived bool   `json:"archived"`
		PushedAt string `json:"pushed_at"`
	}

	printArchived := func(goModPath, repo, pushedAt, depType string) {
		if depType == "indirect" {
			fmt.Printf("%s: https://github.com/%s (last push: %s) [indirect]\n", goModPath, repo, pushedAt)
		} else {
			fmt.Printf("%s: https://github.com/%s (last push: %s)\n", goModPath, repo, pushedAt)
		}
	}

	for repo, infos := range repos {
		wg.Add(1)

		go func(repo string, infos []repoInfo) {
			defer wg.Done()

			// Check cache first
			if cached, found := c.Get(repo); found {
				result := cached.(repoResult)
				if result.Archived {
					for _, info := range infos {
						printArchived(info.goModPath, repo, result.PushedAt, info.depType)
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
					printArchived(info.goModPath, repo, result.PushedAt, info.depType)
				}
			}
		}(repo, infos)
	}

	wg.Wait()

	return nil
}
