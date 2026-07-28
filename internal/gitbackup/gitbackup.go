package gitbackup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Token string `json:"token"`
	User  string `json:"user"`
	Email string `json:"email"`
}

func ParseRepo(url string) (owner, repo string, err error) {
	if !strings.HasPrefix(url, "github:") {
		return "", "", fmt.Errorf("not a github: URL")
	}
	rest := strings.TrimPrefix(url, "github:")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid github URL %q — expected github:owner/repo", url)
	}
	parts[1] = strings.TrimSuffix(parts[1], ".git")
	return parts[0], parts[1], nil
}

func PushBackup(ctx context.Context, cfg Config, owner, repo, domain, sourceDir string) error {
	tmp, err := os.MkdirTemp("", "bombvault-github-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	cloneURL := fmt.Sprintf("https://%s@github.com/%s/%s.git", cfg.Token, owner, repo)
	gitDir := filepath.Join(tmp, "repo")

	if out, err := run(ctx, tmp, "git", "clone", "--depth=1", cloneURL, gitDir); err != nil {
		return fmt.Errorf("git clone: %w\n%s", err, out)
	}

	dateFolder := time.Now().Format("01-02-2006")
	targetDir := filepath.Join(gitDir, dateFolder, domain)
	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return fmt.Errorf("create target dir: %w", err)
	}

	if err := CopyDir(sourceDir, targetDir); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}

	git := func(args ...string) (string, error) {
		return run(ctx, gitDir, "git", args...)
	}

	_, _ = git("config", "user.name", cfg.User)
	_, _ = git("config", "user.email", cfg.Email)

	if out, err := git("add", "-A"); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, out)
	}
	msg := fmt.Sprintf("backup %s %s", domain, dateFolder)
	if out, err := git("commit", "-m", msg); err != nil {
		if strings.Contains(out, "nothing to commit") {
			return nil
		}
		return fmt.Errorf("git commit: %w\n%s", err, out)
	}
	if out, err := git("push", "origin", "HEAD"); err != nil {
		return fmt.Errorf("git push: %w\n%s", err, out)
	}
	return nil
}

func TestAccess(ctx context.Context, cfg Config, owner, repo string) error {
	cloneURL := fmt.Sprintf("https://%s@github.com/%s/%s.git", cfg.Token, owner, repo)
	out, err := exec.CommandContext(ctx, "git", "ls-remote", cloneURL, "HEAD").CombinedOutput() //nolint:gosec
	if err != nil {
		return fmt.Errorf("github access test failed: %w\n%s", err, string(out))
	}
	return nil
}

func run(ctx context.Context, dir, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...) //nolint:gosec
	c.Dir = dir
	out, err := c.CombinedOutput()
	return string(out), err
}

func CopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, fi.Mode())
		}
		data, err := os.ReadFile(path) //nolint:gosec
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, fi.Mode()) //nolint:gosec
	})
}
