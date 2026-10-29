package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func acquireConfigLock(path string) (*os.File, error) {
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("cannot acquire config lock (another updater may be running): %w", err)
	}
	return lock, nil
}

func addItem(path, directory string, input io.Reader, output io.Writer) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	cyan, green, reset := "", "", ""
	if file, ok := output.(*os.File); ok && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" && os.Getenv("TERM") != "" {
		if info, err := file.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			cyan, green, reset = "\033[36m", "\033[32m", "\033[0m"
		}
	}
	reader := bufio.NewReader(input)
	ask := func(prompt string) (string, error) {
		if _, err := fmt.Fprint(output, cyan, prompt, reset); err != nil {
			return "", err
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("input interrupted: %w", err)
		}
		return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
	}
	const defaultRun = "docker compose up -d --build --remove-orphans"
	retryDefault := 3
	enabled := true
	entry := item{Run: defaultRun, Retry: &retryDefault, Enabled: &enabled}
	if directory != "" {
		entry.Target, err = filepath.Abs(directory)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(entry.Target, ".git")); err != nil {
			return fmt.Errorf("target is not a Git repository: %w", err)
		}
		entry.Repo, err = command(context.Background(), entry.Target, nil, "git", "remote", "get-url", "origin")
		if err != nil {
			return fmt.Errorf("read origin: %w", err)
		}
		if entry.Repo == "" {
			return errors.New("origin must not be empty")
		}
	}
	for _, field := range []struct {
		prompt string
		value  *string
	}{
		{"repo — repository URL", &entry.Repo},
		{"target — project directory (relative to config)", &entry.Target},
		{"run — command", &entry.Run},
	} {
		prompt := field.prompt
		if *field.value != "" {
			prompt += " [" + *field.value + "]"
		}
		value, err := ask(prompt + ": ")
		if err != nil {
			return err
		}
		if value != "" {
			*field.value = value
		}
	}
	retry, err := ask("retry — additional attempts [3], 0 — no retries: ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(retry) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(retry))
		if err != nil || n < 0 {
			return errors.New("retry must be a non-negative integer")
		}
		entry.Retry = &n
	}
	value, err := ask("enabled — enable project [true] (true/false): ")
	if err != nil {
		return err
	}
	switch strings.TrimSpace(value) {
	case "", "true":
		enabled = true
	case "false":
		enabled = false
	default:
		return errors.New("enabled must be true or false")
	}
	lock, err := acquireConfigLock(path)
	if err != nil {
		return err
	}
	defer lock.Close()
	var c config
	data, err := os.ReadFile(path)
	if err == nil {
		if _, err := validateConfig(data, path); err != nil {
			return err
		}
		// Keep relative targets as entered, rather than saving normalized paths.
		if err := json.Unmarshal(data, &c); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	c.Items = append(c.Items, entry)
	data, err = json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if _, err := validateConfig(data, path); err != nil {
		return err
	}
	// Replace atomically so an interrupted write cannot truncate the config.
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "%sProject added to %s%s\n", green, path, reset)
	return err
}
