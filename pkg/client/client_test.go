package client

import (
	"errors"
	"testing"

	"github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

// mockRESTClient implements the minimal interface needed for testing
// Only Get is used in CachedGitHubClient

type mockRESTClient struct {
	getFunc func(string, any) error
}

func (m *mockRESTClient) Get(path string, v any) error {
	return m.getFunc(path, v)
}

func TestNew(t *testing.T) {
	c, err := New()
	require.NoError(t, err)
	require.NotNil(t, c)
	require.NotNil(t, c.cache)
}

func TestGetRepoResult_CacheHit(t *testing.T) {
	c := NewWithClient(&mockRESTClient{})
	repo := "owner/repo"
	want := RepoResult{Archived: true, PushedAt: "2024-01-01T00:00:00Z"}
	c.cache.Set(repo, want, cache.DefaultExpiration)

	got, err := c.GetRepoResult(repo)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestGetRepoResult_InvalidRepo(t *testing.T) {
	c := NewWithClient(&mockRESTClient{})

	_, err := c.GetRepoResult("invalidrepo")
	require.Error(t, err)
}

func TestGetRepoResult_APIFailure(t *testing.T) {
	c := NewWithClient(&mockRESTClient{
		getFunc: func(path string, v any) error {
			return errors.New("api error")
		},
	})

	_, err := c.GetRepoResult("owner/repo")
	require.Error(t, err)
	require.Equal(t, "failed to fetch repo owner/repo: api error", err.Error())
}

func TestGetRepoResult_APISuccess(t *testing.T) {
	c := NewWithClient(&mockRESTClient{
		getFunc: func(path string, v any) error {
			r, ok := v.(*RepoResult)
			if !ok {
				return errors.New("wrong type")
			}
			r.Archived = false
			r.PushedAt = "2025-07-18T12:00:00Z"

			return nil
		},
	})
	repo := "owner/repo"

	got, err := c.GetRepoResult(repo)
	require.NoError(t, err)

	require.Equal(t, false, got.Archived)
	require.Equal(t, "2025-07-18T12:00:00Z", got.PushedAt)

	// Should be cached now
	cached, found := c.cache.Get(repo)
	require.True(t, found)
	require.Equal(t, cached, got)
}
