package updater

import (
	"context"
	"encoding/json"
	"io"
	"strings"
)

// Uncertain Compose state must not prevent a deployment attempt.
func composeHealthy(ctx context.Context, target string, output io.Writer) bool {
	services, err := commandStreams(ctx, target, nil, output, "docker", "compose", "config", "--services")
	if err != nil || strings.TrimSpace(services) == "" {
		return false
	}
	data, err := commandStreams(ctx, target, nil, output, "docker", "compose", "ps", "--all", "--format", "json")
	if err != nil {
		return false
	}
	type container struct {
		Service string
		State   string
		Health  string
	}
	var containers []container
	// Compose versions emit either a JSON array or one JSON object per line.
	if strings.HasPrefix(data, "[") {
		if err := json.Unmarshal([]byte(data), &containers); err != nil {
			return false
		}
	} else {
		decoder := json.NewDecoder(strings.NewReader(data))
		for {
			var c container
			if err := decoder.Decode(&c); err == io.EOF {
				break
			} else if err != nil {
				return false
			}
			containers = append(containers, c)
		}
	}
	running := make(map[string]bool)
	for _, c := range containers {
		if c.Service == "" || c.State != "running" || (c.Health != "" && c.Health != "healthy") {
			return false
		}
		running[c.Service] = true
	}
	for _, service := range strings.Fields(services) {
		if !running[service] {
			return false
		}
	}
	return true
}
