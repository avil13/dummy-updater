package updater

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func command(ctx context.Context, dir string, output io.Writer, name string, args ...string) (string, error) {
	return commandStreams(ctx, dir, output, output, name, args...)
}

func commandStreams(ctx context.Context, dir string, output, stderr io.Writer, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if output != nil {
		cmd.Stdout = output
	}
	cmd.Stderr = os.Stderr
	if stderr != nil {
		cmd.Stderr = stderr
	}
	err := cmd.Run()
	return strings.TrimSpace(buf.String()), err
}

func update(ctx context.Context, entry item, output io.Writer, logger *log.Logger) (result string, err error) {
	inspect := func(args ...string) (string, error) {
		return commandStreams(ctx, entry.Target, nil, output, "git", args...)
	}
	cloned := false
	if _, err := os.Stat(entry.Target); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(entry.Target), 0755); err != nil {
			return "", err
		}
		if _, err := command(ctx, "", output, "git", "clone", "--", entry.Repo, entry.Target); err != nil {
			return "", fmt.Errorf("clone: %w", err)
		}
		cloned = true
	} else if err != nil {
		return "", err
	}
	// Require the target itself to be a repository, not a directory inside one.
	if _, err := os.Stat(filepath.Join(entry.Target, ".git")); err != nil {
		return "", fmt.Errorf("target is not a Git repository: %w", err)
	}
	remote, err := inspect("remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	if remote != entry.Repo {
		return "", errors.New("origin does not match configured repo")
	}
	status, err := inspect("status", "--porcelain")
	if err != nil {
		return "", err
	}
	if status != "" {
		logger.Print("WARNING: local changes; skipping")
		return "⏭ локальные изменения", nil
	}
	before, err := inspect("rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if !cloned {
		if _, err := command(ctx, entry.Target, output, "git", "pull", "--ff-only"); err != nil {
			return "", fmt.Errorf("pull: %w", err)
		}
	}
	after, err := inspect("rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	// Run even without new commits so a failed deployment is retried next time.
	// The trusted config intentionally supplies a shell command.
	retry := 2
	if entry.Retry != nil {
		retry = *entry.Retry
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		_, err := command(ctx, entry.Target, output, "sh", "-c", entry.Run)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("run: %w", err)
		}
		_, _ = command(ctx, entry.Target, output, "docker", "compose", "ps")
		_, _ = command(ctx, entry.Target, output, "docker", "compose", "logs", "--tail=30")
		if attempt >= retry {
			return "", fmt.Errorf("run: %w", err)
		}
		logger.Printf("WARNING: run attempt %d failed: %v; retrying", attempt+1, err)
	}
	if _, err := command(ctx, entry.Target, output, "docker", "compose", "ps"); err != nil {
		return "", err
	}
	logger.Printf("SUCCESS: %s -> %s", before, after)
	if cloned {
		return "✅ клонирован, run выполнен", nil
	}
	if before != after {
		return "✅ обновлён, run выполнен", nil
	}
	return "✅ без новых коммитов, run выполнен", nil
}
