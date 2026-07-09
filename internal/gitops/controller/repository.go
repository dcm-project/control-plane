package controller

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitOperations abstracts git operations for testability.
type GitOperations interface {
	// CloneOrFetch clones the repo if not present, or fetches and resets if it exists.
	// Returns the latest commit SHA on the specified branch.
	CloneOrFetch(ctx context.Context, url, branch, repoID string) (latestCommit string, err error)

	// WorkDir returns the path to the cloned repo for a given repoID.
	WorkDir(repoID string) string
}

// GitClient implements GitOperations using the git CLI.
type GitClient struct {
	baseDir string
}

// NewGitClient creates a GitClient that stores cloned repos under baseDir.
func NewGitClient(baseDir string) *GitClient {
	return &GitClient{baseDir: baseDir}
}

func (g *GitClient) WorkDir(repoID string) string {
	return filepath.Join(g.baseDir, repoID)
}

func (g *GitClient) CloneOrFetch(ctx context.Context, url, branch, repoID string) (string, error) {
	dir := g.WorkDir(repoID)

	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", fmt.Errorf("create parent dir: %w", err)
		}
		if err := g.run(ctx, g.baseDir, "git", "clone", "--branch", branch, "--single-branch", "--depth", "1", url, repoID); err != nil {
			return "", fmt.Errorf("git clone: %w", err)
		}
	} else {
		if err := g.run(ctx, dir, "git", "fetch", "origin", branch); err != nil {
			return "", fmt.Errorf("git fetch: %w", err)
		}
		if err := g.run(ctx, dir, "git", "reset", "--hard", "origin/"+branch); err != nil {
			return "", fmt.Errorf("git reset: %w", err)
		}
	}

	commit, err := g.output(ctx, dir, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(commit), nil
}

func (g *GitClient) run(ctx context.Context, dir string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr // log git output to stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (g *GitClient) output(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
