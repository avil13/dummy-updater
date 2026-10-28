package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type item struct {
	Repo   string `json:"repo"`
	Target string `json:"target"`
}

type config struct {
	Items []item `json:"items"`
}

func readConfig(path string) (config, error) {
	var c config
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return c, errors.New("expected one JSON object")
	}
	if len(c.Items) == 0 {
		return c, errors.New("items must not be empty")
	}
	seen := map[string]bool{}
	for i := range c.Items {
		entry := &c.Items[i]
		if strings.TrimSpace(entry.Repo) == "" || strings.TrimSpace(entry.Target) == "" {
			return c, fmt.Errorf("items[%d]: repo and target are required", i)
		}
		if !filepath.IsAbs(entry.Target) {
			entry.Target = filepath.Join(filepath.Dir(path), entry.Target)
		}
		entry.Target, err = filepath.Abs(entry.Target)
		if err != nil {
			return c, err
		}
		if seen[entry.Target] {
			return c, fmt.Errorf("duplicate target: %s", entry.Target)
		}
		seen[entry.Target] = true
	}
	return c, nil
}

func command(ctx context.Context, dir string, output io.Writer, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if output != nil {
		cmd.Stdout = io.MultiWriter(output, &buf)
	}
	cmd.Stderr = os.Stderr
	if output != nil {
		cmd.Stderr = output
	}
	err := cmd.Run()
	return strings.TrimSpace(buf.String()), err
}

func update(ctx context.Context, entry item, output io.Writer, logger *log.Logger) error {
	cloned := false
	if _, err := os.Stat(entry.Target); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(entry.Target), 0755); err != nil {
			return err
		}
		if _, err := command(ctx, "", output, "git", "clone", "--", entry.Repo, entry.Target); err != nil {
			return fmt.Errorf("clone: %w", err)
		}
		cloned = true
	} else if err != nil {
		return err
	}
	// Require the target itself to be a repository, not a directory inside one.
	if _, err := os.Stat(filepath.Join(entry.Target, ".git")); err != nil {
		return fmt.Errorf("target is not a Git repository: %w", err)
	}
	remote, err := command(ctx, entry.Target, nil, "git", "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	if remote != entry.Repo {
		return errors.New("origin does not match configured repo")
	}
	status, err := command(ctx, entry.Target, nil, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if status != "" {
		logger.Print("WARNING: local changes; skipping")
		return nil
	}
	before, err := command(ctx, entry.Target, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if !cloned {
		if _, err := command(ctx, entry.Target, output, "git", "pull", "--ff-only"); err != nil {
			return fmt.Errorf("pull: %w", err)
		}
	}
	after, err := command(ctx, entry.Target, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	// Run even without new commits so a failed deployment is retried next time.
	if _, err := command(ctx, entry.Target, output, "docker", "compose", "up", "-d", "--build", "--remove-orphans"); err != nil {
		if ctx.Err() == nil {
			_, _ = command(ctx, entry.Target, output, "docker", "compose", "ps")
			_, _ = command(ctx, entry.Target, output, "docker", "compose", "logs", "--tail=30")
		}
		return fmt.Errorf("compose up: %w", err)
	}
	if _, err := command(ctx, entry.Target, output, "docker", "compose", "ps"); err != nil {
		return err
	}
	logger.Printf("SUCCESS: %s -> %s", before, after)
	if cloned || before != after {
		details, err := command(ctx, entry.Target, nil, "git", "log", "-1", "--format=%an: %s")
		if err != nil {
			return err
		}
		host, _ := os.Hostname()
		notify(ctx, fmt.Sprintf("Updated %s\n%s -> %s\n%s\nHost: %s\nDocker Compose started", entry.Target, before, after, details, host), logger)
	}
	return nil
}

func notify(ctx context.Context, message string, logger *log.Logger) {
	if os.Getenv("TELEGRAM_ENABLED") != "true" {
		return
	}
	token, chat := os.Getenv("TELEGRAM_BOT_TOKEN"), os.Getenv("TELEGRAM_CHAT_ID")
	if token == "" || chat == "" {
		logger.Print("WARNING: Telegram credentials missing")
		return
	}
	body := url.Values{"chat_id": {chat}, "text": {message}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", strings.NewReader(body.Encode()))
	if err != nil {
		logger.Print("WARNING: invalid Telegram configuration")
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Print("WARNING: Telegram request failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.Printf("WARNING: Telegram HTTP %d", resp.StatusCode)
	}
}

func run(ctx context.Context, path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	c, err := readConfig(path)
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("cannot acquire update lock (another updater may be running): %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	file, err := os.OpenFile(filepath.Join(filepath.Dir(path), "error.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	logger := log.New(os.Stdout, "", log.LstdFlags)
	errorLogger := log.New(file, "", log.LstdFlags)
	for _, name := range []string{"git", "docker"} {
		if _, err := exec.LookPath(name); err != nil {
			return err
		}
	}
	failed := false
	for _, entry := range c.Items {
		if err := ctx.Err(); err != nil {
			return err
		}
		logger.SetPrefix("[" + entry.Target + "] ")
		logger.Print("Checking project")
		if err := update(ctx, entry, os.Stdout, logger); err != nil {
			errorLogger.SetPrefix("[" + entry.Target + "] ")
			errorLogger.Printf("ERROR: %v", err)
			failed = true
		}
	}
	if failed {
		return errors.New("one or more projects failed to update")
	}
	return nil
}

func main() {
	executable, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	path := flag.String("config", filepath.Join(filepath.Dir(executable), "update-items.json"), "path to update-items.json")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *path); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
