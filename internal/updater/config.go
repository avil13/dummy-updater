package updater

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

type logConfig struct {
	MaxErrMB     *float64 `json:"max_err_mb,omitempty"`
	MaxProjectMB *float64 `json:"max_project_mb,omitempty"`
}

type config struct {
	Items []item          `json:"items"`
	TG    *telegramConfig `json:"tg,omitempty"`
	Log   *logConfig      `json:"log,omitempty"`
}

func (c config) logLimits() (errLimit, projectLimit int64) {
	errLimit, projectLimit = 1_000_000, 500_000
	if c.Log != nil {
		if c.Log.MaxErrMB != nil {
			errLimit = int64(*c.Log.MaxErrMB * 1_000_000)
		}
		if c.Log.MaxProjectMB != nil {
			projectLimit = int64(*c.Log.MaxProjectMB * 1_000_000)
		}
	}
	return
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
	if c.Log != nil {
		for name, value := range map[string]*float64{"max_err_mb": c.Log.MaxErrMB, "max_project_mb": c.Log.MaxProjectMB} {
			if value != nil && (*value < 0 || *value >= float64(1<<63)/1_000_000 || (*value > 0 && *value < 0.000001)) {
				return c, fmt.Errorf("log.%s: must be zero or at least one byte and fit in int64", name)
			}
		}
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
