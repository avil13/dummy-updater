package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func listItems(ctx context.Context, path string) (err error) {
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	// Validate before changing terminal settings.
	if _, err := readConfig(path); err != nil {
		return err
	}
	enter, restore, err := terminalMode(os.Stdin)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, restore())
		_, _ = fmt.Fprint(os.Stdout, "\033[0m\n")
	}()
	if err := enter(); err != nil {
		return err
	}
	m := &menuUI{ctx: ctx, input: os.Stdin, output: os.Stdout, poll: true}
	err = browseItems(m, path, func(target string) error {
		if err := restore(); err != nil {
			return err
		}
		runErr := runSelectedWithConfirmation(ctx, path, target, func() (bool, error) {
			if err := enter(); err != nil {
				return false, err
			}
			_, _ = fmt.Fprintln(m.output)
			approved, err := m.confirmRun()
			return approved, errors.Join(err, restore())
		})
		return errors.Join(runErr, enter())
	})
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (m *menuUI) confirmRun() (bool, error) {
	for {
		answer, err := m.ask("No new Git commits. Execute run again? [Y/n]: ")
		if errors.Is(err, errEditCanceled) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
	}
}

func browseItems(m *menuUI, path string, runProject func(string) error) error {
	selected := 0
	for {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		c, err := validateConfig(data, path)
		if err != nil {
			return err
		}
		labels := make([]string, len(c.Items))
		for i, entry := range c.Items {
			labels[i] = entry.Target
			if entry.Enabled != nil && !*entry.Enabled {
				labels[i] += " [disabled]"
			}
		}
		selected, err = m.choose("Projects in config.items", labels, min(selected, len(labels)-1))
		if err != nil || selected < 0 {
			return err
		}
		entry := c.Items[selected]
		for {
			action, err := m.choose(entry.Target, []string{"Edit settings", "Run project", "View logs"}, 0)
			if err != nil {
				return err
			}
			if action < 0 {
				break
			}
			var actionErr error
			switch action {
			case 0:
				actionErr = editItem(path, data, selected, m.ask)
			case 1:
				actionErr = runProject(entry.Target)
			case 2:
				actionErr = m.logs(path, entry.Target)
			}
			if err := m.ctx.Err(); err != nil {
				return err
			}
			if errors.Is(actionErr, context.Canceled) || errors.Is(actionErr, io.EOF) {
				return actionErr
			}
			if actionErr != nil && !errors.Is(actionErr, errEditCanceled) {
				if err := m.view("Error", actionErr.Error()); err != nil {
					return err
				}
			} else if action != 2 && actionErr == nil {
				if err := m.view("Done", "Operation completed."); err != nil {
					return err
				}
			}
			// Reload after editing, including a changed target or a concurrent config edit.
			if action == 0 {
				break
			}
		}
	}
}

func editItem(path string, original []byte, index int, ask func(string) (string, error)) error {
	if _, err := validateConfig(original, path); err != nil {
		return err
	}
	var c config
	if err := json.Unmarshal(original, &c); err != nil {
		return err
	}
	entry := &c.Items[index]
	for _, field := range []struct {
		name  string
		value *string
	}{{"repo", &entry.Repo}, {"target", &entry.Target}, {"run", &entry.Run}} {
		value, err := ask(field.name + " [" + *field.value + "] (Enter — keep, Esc — cancel): ")
		if err != nil {
			return err
		}
		if value != "" {
			*field.value = value
		}
	}
	retry := 2
	if entry.Retry != nil {
		retry = *entry.Retry
	}
	value, err := ask(fmt.Sprintf("retry [%d]: ", retry))
	if err != nil {
		return err
	}
	if strings.TrimSpace(value) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 0 {
			return errors.New("retry must be a non-negative integer")
		}
		entry.Retry = &n
	}
	enabled := entry.Enabled == nil || *entry.Enabled
	value, err = ask(fmt.Sprintf("enabled [%t] (true/false): ", enabled))
	if err != nil {
		return err
	}
	switch strings.TrimSpace(value) {
	case "":
	case "true", "false":
		enabled = strings.TrimSpace(value) == "true"
		entry.Enabled = &enabled
	default:
		return errors.New("enabled must be true or false")
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if _, err := validateConfig(data, path); err != nil {
		return err
	}
	lock, err := acquireConfigLock(path)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) {
		return errors.New("config changed while editing; reopen the project and try again")
	}
	return writeConfig(path, data)
}
