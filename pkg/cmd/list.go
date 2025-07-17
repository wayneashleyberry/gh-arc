package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cli/go-gh/v2/pkg/api"
	"golang.org/x/mod/modfile"
)

func ListArchivedGoModules() error {
	goModPath := filepath.Join(".", "go.mod")

	data, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("could not open go.mod: %w", err)
	}

	mf, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return fmt.Errorf("failed to parse go.mod: %w", err)
	}

	repos := map[string]string{} // repo -> "direct" or "indirect"

	for _, req := range mf.Require {
		if strings.HasPrefix(req.Mod.Path, "github.com/") {
			parts := strings.Split(req.Mod.Path, "/")
			if len(parts) >= 3 {
				repo := fmt.Sprintf("%s/%s", parts[1], parts[2])
				depType := "direct"
				if req.Indirect {
					depType = "indirect"
				}
				repos[repo] = depType
			}
		}
	}

	for _, rep := range mf.Replace {
		if strings.HasPrefix(rep.New.Path, "github.com/") {
			parts := strings.Split(rep.New.Path, "/")
			if len(parts) >= 3 {
				repo := fmt.Sprintf("%s/%s", parts[1], parts[2])
				// If replaced repo is already in repos, keep its type (direct/indirect)
				if _, ok := repos[repo]; !ok {
					repos[repo] = "direct" // default to direct for replace
				}
			}
		}
	}

	if len(repos) == 0 {
		fmt.Println("No github.com modules found in go.mod")

		return nil
	}

	client, err := api.DefaultRESTClient()
	if err != nil {
		return fmt.Errorf("failed to create GitHub API client: %w", err)
	}

	var wg sync.WaitGroup

	for repo, depType := range repos {
		wg.Add(1)

		go func(repo, depType string) {
			defer wg.Done()

			ownerRepo := strings.Split(repo, "/")
			if len(ownerRepo) != 2 {
				return
			}

			var result struct {
				Archived bool   `json:"archived"`
				PushedAt string `json:"pushed_at"`
			}

			path := fmt.Sprintf("repos/%s/%s", ownerRepo[0], ownerRepo[1])

			err := client.Get(path, &result)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error fetching repo %s: %v\n", repo, err)

				return
			}

			if result.Archived {
				fmt.Printf("github.com/%s (last push: %s) [%s]\n", repo, result.PushedAt, depType)
			}
		}(repo, depType)
	}

	wg.Wait()

	return nil
}
