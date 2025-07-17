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

	repos := map[string]struct{}{}

	for _, req := range mf.Require {
		if strings.HasPrefix(req.Mod.Path, "github.com/") {
			parts := strings.Split(req.Mod.Path, "/")
			if len(parts) >= 3 {
				repo := fmt.Sprintf("%s/%s", parts[1], parts[2])
				repos[repo] = struct{}{}
			}
		}
	}

	for _, rep := range mf.Replace {
		if strings.HasPrefix(rep.New.Path, "github.com/") {
			parts := strings.Split(rep.New.Path, "/")
			if len(parts) >= 3 {
				repo := fmt.Sprintf("%s/%s", parts[1], parts[2])
				repos[repo] = struct{}{}
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

	for repo := range repos {
		wg.Add(1)

		go func(repo string) {
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
				fmt.Printf("github.com/%s (last push: %s)\n", repo, result.PushedAt)
			}
		}(repo)
	}

	wg.Wait()

	return nil
}
