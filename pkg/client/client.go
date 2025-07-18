// Package client provides a GitHub API client with transparent caching for repository metadata.
// It allows efficient retrieval of repository information such as archived status and last push date,
// reducing redundant API calls by using an in-memory cache.
package client

import (
	"fmt"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/patrickmn/go-cache"
)

// CachedGitHubClient wraps the GitHub API client and transparently caches repo
// results.
// restClient defines the minimal interface needed for CachedGitHubClient.
type restClient interface {
	Get(path string, resp any) error
}

// Client provides methods to interact with the GitHub API and transparently cache repository metadata.
// It is safe for concurrent use by multiple goroutines.
type Client struct {
	client restClient
	cache  *cache.Cache
}

// RepoResult contains metadata about a GitHub repository, including its
// archived status and last push date.
type RepoResult struct {
	Archived bool   `json:"archived"`
	PushedAt string `json:"pushed_at"`
}

// New creates a new CachedGitHubClient with a default REST
// client and an in-memory cache. The cache is used to store repository metadata
// and reduce redundant API calls. Returns an error if the GitHub API client
// cannot be created.
// New creates a new CachedGitHubClient with a default REST client and an in-memory cache.
func New() (*Client, error) {
	client, err := api.DefaultRESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub API client: %w", err)
	}

	c := cache.New(1*time.Hour, 2*time.Hour)

	return &Client{client: client, cache: c}, nil
}

// NewWithClient allows injecting a custom REST client (for testing).
func NewWithClient(client restClient) *Client {
	c := cache.New(1*time.Hour, 2*time.Hour)

	return &Client{client: client, cache: c}
}

// GetRepoResult returns the archived status and last push date for a GitHub
// repository. It transparently caches results to avoid redundant API calls. The
// repo argument should be in the form "owner/repo".
func (c *Client) GetRepoResult(repo string) (RepoResult, error) {
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
