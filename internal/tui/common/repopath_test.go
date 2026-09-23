package common_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
)

var configPaths = map[string]string{
	"user/repo": "/path/to/user/repo",
	"user_2/*":  "/path/to/user_2/*",
}

var configPathsWithOwnerRepoTemplateIgnoringOwner = map[string]string{
	"user/repo":    "/path/to/user/repo",
	"user_2/*":     "/path/to/user_2/*",
	":owner/:repo": "/path/to/user/dev/:repo",
}

var configPathsWithOwnerRepoTemplate = map[string]string{
	"user/repo":    "/path/to/the_repo",
	"org/*":        "/path/to/the_org/*",
	":owner/:repo": "/path/to/github.com/:owner/:repo",
}

func TestGetRepoLocalPath(t *testing.T) {
	testCases := map[string]struct {
		repo        string
		want        string
		found       bool
		configPaths map[string]string
	}{
		"exact match": {
			repo:        "user/repo",
			want:        "/path/to/user/repo",
			found:       true,
			configPaths: configPaths,
		},
		"exact no match": {
			repo:        "user/other_repo",
			want:        "",
			found:       false,
			configPaths: configPaths,
		},
		"wildcard match": {
			repo:        "user_2/repo123",
			want:        "/path/to/user_2/repo123",
			found:       true,
			configPaths: configPaths,
		},
		"bad path": {
			repo:        "invalid-lookup",
			want:        "",
			found:       false,
			configPaths: configPaths,
		},
		"with :owner/:repo template: ignoring owner substitution": {
			repo:        "user3/repo",
			want:        "/path/to/user/dev/repo",
			found:       true,
			configPaths: configPathsWithOwnerRepoTemplateIgnoringOwner,
		},
		"with :owner/:repo template: exact match": {
			repo:        "user/repo",
			want:        "/path/to/the_repo",
			found:       true,
			configPaths: configPathsWithOwnerRepoTemplate,
		},
		"with :owner/:repo template: no match for this sibling repo": {
			repo:        "user/another_repo",
			want:        "/path/to/github.com/user/another_repo",
			found:       true,
			configPaths: configPathsWithOwnerRepoTemplate,
		},
		"with :owner/:repo template: wildcard repo match": {
			repo:        "org/some_repo",
			want:        "/path/to/the_org/some_repo",
			found:       true,
			configPaths: configPathsWithOwnerRepoTemplate,
		},
		"with :owner/:repo template: general fallback": {
			repo:        "any-owner/any-repo",
			want:        "/path/to/github.com/any-owner/any-repo",
			found:       true,
			configPaths: configPathsWithOwnerRepoTemplate,
		},
		"with :owner/:repo template: repeated :repo substitution": {
			repo:        "any-owner/any-repo",
			want:        "src/github.com/any-owner/any-repo/any-repo",
			found:       true,
			configPaths: map[string]string{":owner/:repo": "src/github.com/:owner/:repo/:repo"},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got, found := common.GetRepoLocalPath(tc.repo, tc.configPaths)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.found, found)
		})
	}
}

func TestResolveRepoLocalPath(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	testCases := map[string]struct {
		repo        string
		cfgPaths    map[string]string
		currentName string
		currentPath string
		want        string
		wantErr     bool
	}{
		"config match wins over current repo": {
			repo:        "user/repo",
			cfgPaths:    configPaths,
			currentName: "user/repo",
			currentPath: "/cwd/repo",
			want:        "/path/to/user/repo",
		},
		"falls back to current repo when it matches": {
			repo:        "User/Repo",
			currentName: "user/repo",
			currentPath: "/cwd/repo",
			want:        "/cwd/repo",
		},
		"does not fall back to an unrelated current repo": {
			repo:        "user/other",
			currentName: "user/repo",
			currentPath: "/cwd/repo",
			wantErr:     true,
		},
		"errors outside any repo": {
			repo:    "user/other",
			wantErr: true,
		},
		"expands tilde": {
			repo:     "user/repo",
			cfgPaths: map[string]string{"user/repo": "~/code/repo"},
			want:     home + "/code/repo",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got, err := common.ResolveRepoLocalPath(
				tc.repo, tc.cfgPaths, tc.currentName, tc.currentPath)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
