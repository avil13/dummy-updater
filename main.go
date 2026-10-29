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
	Repo    string `json:"repo"`
	Target  string `json:"target"`
	Run     string `json:"run"`
	Retry   *int   `json:"retry"`
	Enabled *bool  `json:"enabled,omitempty"`
}

type telegramConfig struct {
	Enabled    bool   `json:"enabled"`
	BotToken   string `json:"bot_token"`
	ChatID     string `json:"chat_id"`
	OnlyErrors bool   `json:"only_errors"`
}

type config struct {
	Items []item          `json:"items"`
	TG    *telegramConfig `json:"tg,omitempty"`
}

func (c config) enabledItems() []item {
	var items []item
	for _, entry := range c.Items {
		if entry.Enabled == nil || *entry.Enabled {
			items = append(items, entry)
		}
	}
	return items
}

func readConfig(path string) (config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, err
	}
	return validateConfig(data, path)
}

func validateConfig(data []byte, path string) (config, error) {
	var c config
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
	if c.TG != nil && c.TG.Enabled && (strings.TrimSpace(c.TG.BotToken) == "" || strings.TrimSpace(c.TG.ChatID) == "") {
		return c, errors.New("tg: bot_token and chat_id are required when enabled")
	}
	seen := map[string]bool{}
	for i := range c.Items {
		entry := &c.Items[i]
		if strings.TrimSpace(entry.Repo) == "" || strings.TrimSpace(entry.Target) == "" || strings.TrimSpace(entry.Run) == "" {
			return c, fmt.Errorf("items[%d]: repo, target and run are required", i)
		}
		if entry.Retry != nil && *entry.Retry < 0 {
			return c, fmt.Errorf("items[%d].retry: must be non-negative", i)
		}
		if entry.Repo != strings.TrimSpace(entry.Repo) {
			return c, fmt.Errorf("items[%d].repo: leading or trailing whitespace is not allowed", i)
		}
		if !filepath.IsAbs(entry.Target) {
			entry.Target = filepath.Join(filepath.Dir(path), entry.Target)
		}
		target, err := filepath.Abs(entry.Target)
		if err != nil {
			return c, err
		}
		entry.Target = target
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

func update(ctx context.Context, entry item, output io.Writer, logger *log.Logger) (result string, err error) {
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
	remote, err := command(ctx, entry.Target, nil, "git", "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	if remote != entry.Repo {
		return "", errors.New("origin does not match configured repo")
	}
	status, err := command(ctx, entry.Target, nil, "git", "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if status != "" {
		logger.Print("WARNING: local changes; skipping")
		return "⏭ локальные изменения", nil
	}
	before, err := command(ctx, entry.Target, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if !cloned {
		if _, err := command(ctx, entry.Target, output, "git", "pull", "--ff-only"); err != nil {
			return "", fmt.Errorf("pull: %w", err)
		}
	}
	after, err := command(ctx, entry.Target, nil, "git", "rev-parse", "HEAD")
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

func notify(ctx context.Context, tg telegramConfig, message string, logger *log.Logger) {
	if !tg.Enabled {
		return
	}
	token, chat := tg.BotToken, tg.ChatID
	if token == "" || chat == "" {
		logger.Print("WARNING: Telegram credentials missing")
		return
	}
	body := url.Values{"chat_id": {chat}, "text": {message}, "disable_web_page_preview": {"true"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", strings.NewReader(body.Encode()))
	if err != nil {
		logger.Print("WARNING: invalid Telegram configuration")
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		logger.Print("WARNING: Telegram request failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.Printf("WARNING: Telegram HTTP %d", resp.StatusCode)
		return
	}
	var response struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&response); err != nil || !response.OK {
		logger.Print("WARNING: Telegram rejected report")
	}
}

func run(ctx context.Context, path string) (runErr error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	lock, err := acquireConfigLock(path)
	if err != nil {
		return err
	}
	defer lock.Close()
	c, err := readConfig(path)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(filepath.Dir(path), "error.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	logger := log.New(os.Stdout, "", log.LstdFlags)
	errorLogger := log.New(io.MultiWriter(file, os.Stderr), "", log.LstdFlags)
	tg := c.TG
	if tg == nil {
		tg = &telegramConfig{Enabled: os.Getenv("TELEGRAM_ENABLED") == "true", BotToken: os.Getenv("TELEGRAM_BOT_TOKEN"), ChatID: os.Getenv("TELEGRAM_CHAT_ID")}
	}
	statuses := make([]string, len(c.Items))
	projectErrors := make([]bool, len(c.Items))
	failed := false
	for i, entry := range c.Items {
		statuses[i] = "⏭ не выполнен"
		if entry.Enabled != nil && !*entry.Enabled {
			statuses[i] = "⏭ отключён"
		}
	}
	defer func() {
		if !tg.Enabled {
			return
		}
		logger.SetPrefix("")
		host, _ := os.Hostname()
		message := "Отчёт updater — " + host
		hasReport := false
		if runErr != nil && !failed {
			message += "\n❌ запуск не завершён (подробности в stderr)"
			hasReport = true
		}
		for i, entry := range c.Items {
			if tg.OnlyErrors && !projectErrors[i] {
				continue
			}
			hasReport = true
			line := "\n" + entry.Target + ": " + statuses[i]
			// Keep each message below Telegram’s 4096-character limit.
			if len([]rune(line)) > 3500 {
				line = string([]rune(line)[:3500]) + "…"
			}
			if len([]rune(message+line)) > 4000 {
				notify(context.WithoutCancel(ctx), *tg, message, logger)
				message = "Отчёт updater (продолжение)"
			}
			message += line
		}
		if hasReport {
			notify(context.WithoutCancel(ctx), *tg, message, logger)
		}
	}()
	items := c.enabledItems()
	if len(items) == 0 {
		logger.Print("No enabled projects")
		return nil
	}
	for _, name := range []string{"git", "docker"} {
		if _, err := exec.LookPath(name); err != nil {
			return err
		}
	}
	for i, entry := range c.Items {
		if entry.Enabled != nil && !*entry.Enabled {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		logger.SetPrefix("[" + entry.Target + "] ")
		logger.Print("Checking project")
		status, err := update(ctx, entry, os.Stdout, logger)
		statuses[i] = status
		if err != nil {
			projectErrors[i] = true
			statuses[i] = "❌ ошибка (подробности в error.log)"
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
	path := flag.String("config", filepath.Join(filepath.Dir(executable), "config.json"), "path to config.json")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), `Usage: updater [-config path] [add [directory]|stop]

Commands:
  (no command)  Update all repositories and execute their run commands.
  add [directory]  Add a project; prefill target and origin from a local repo.
  stop          Select a project and stop its Docker Compose containers.

Options:
  -h, --help    Show this help and exit.
  -config path  Config file (default: config.json beside the binary).

Examples:
  ./update.sh
  ./update.sh add
  ./update.sh add ../path/to/repo
  ./update.sh stop
  ./update.sh stop -config /absolute/path/config.json`)
	}
	args := os.Args[1:]
	action := ""
	if len(args) > 0 && (args[0] == "add" || args[0] == "stop") {
		action, args = args[0], args[1:]
	}
	directory := ""
	if action == "add" && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		directory, args = args[0], args[1:]
	}
	if err := flag.CommandLine.Parse(args); err != nil {
		log.Fatal(err)
	}
	rest := flag.Args()
	if action == "" && len(rest) > 0 && (rest[0] == "add" || rest[0] == "stop") {
		action, rest = rest[0], rest[1:]
	}
	if action == "add" && directory == "" && len(rest) == 1 {
		directory, rest = rest[0], rest[1:]
	}
	if len(rest) != 0 {
		log.Fatal("usage: updater [-config path] [add [directory]|stop]")
	}
	if action == "add" {
		err = addItem(*path, directory, os.Stdin, os.Stdout)
	} else {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if action == "stop" {
			err = stopItem(ctx, *path, os.Stdin, os.Stdout)
		} else {
			err = run(ctx, *path)
		}
	}
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
