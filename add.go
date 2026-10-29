package main

import (
	"bufio"
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

func addItem(path string, input io.Reader, output io.Writer) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(input)
	ask := func(prompt string) (string, error) {
		if _, err := fmt.Fprint(output, prompt); err != nil {
			return "", err
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("input interrupted: %w", err)
		}
		return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
	}
	var entry item
	for _, field := range []struct {
		prompt string
		value  *string
	}{
		{"repo — URL репозитория: ", &entry.Repo},
		{"target — директория проекта (относительно конфига): ", &entry.Target},
		{"run — команда запуска: ", &entry.Run},
	} {
		*field.value, err = ask(field.prompt)
		if err != nil {
			return err
		}
	}
	retry, err := ask("retry — дополнительные попытки [2], 0 — без повторов: ")
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
	_, err = fmt.Fprintf(output, "Проект добавлен в %s\n", path)
	return err
}
