package updater

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func projectLogPrefix(target string) string {
	return fmt.Sprintf("project-output-%x-", sha256.Sum256([]byte(target)))
}

// The filename identifies the project even when the transcript's header is truncated.
func projectLogs(path, target string) (string, []string, error) {
	dir := filepath.Join(filepath.Dir(path), "logs")
	data, err := os.ReadFile(filepath.Join(dir, "error.log"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", nil, err
	}
	var lines []string
	legacy := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "["+target+"] ") {
			continue
		}
		lines = append(lines, line)
		if _, name, ok := strings.Cut(line, "; output: "); ok && filepath.Dir(name) == dir {
			legacy[filepath.Base(name)] = true
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", nil, err
	}
	var files []os.FileInfo
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "project-output-") || !strings.HasSuffix(name, ".log") ||
			(!strings.HasPrefix(name, projectLogPrefix(target)) && !legacy[name]) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", nil, err
		}
		if info.Mode().IsRegular() {
			files = append(files, info)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime().After(files[j].ModTime()) })
	names := make([]string, len(files))
	for i, info := range files {
		names[i] = info.Name()
	}
	return strings.Join(lines, "\n"), names, nil
}

func (m *menuUI) logs(path, target string) error {
	errorsText, files, err := projectLogs(path, target)
	if err != nil {
		return err
	}
	if errorsText == "" {
		errorsText = "No saved errors. Output from successful runs is not saved."
	}
	labels := append([]string{"Project errors (error.log)"}, files...)
	selected := 0
	for {
		selected, err = m.choose("Logs: "+target, labels, selected)
		if err != nil || selected < 0 {
			return err
		}
		content := errorsText
		if selected > 0 {
			name := filepath.Join(filepath.Dir(path), "logs", files[selected-1])
			// Recheck after selection: never follow a symlink from the log menu.
			info, err := os.Lstat(name)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("not a regular log file: %s", name)
			}
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			content = string(data)
		}
		if err := m.view(labels[selected], content); err != nil {
			return err
		}
	}
}
