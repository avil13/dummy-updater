package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func stopItem(ctx context.Context, path string, input io.Reader, output io.Writer) error {
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
	items := c.enabledItems()
	if len(items) == 0 {
		_, err := fmt.Fprintln(output, "No enabled projects")
		return err
	}
	for i, entry := range items {
		if _, err := fmt.Fprintf(output, "%d) %s\n", i+1, entry.Target); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(output, "Select project to stop (number, Enter to cancel): "); err != nil {
		return err
	}
	// Allow cancellation while the terminal is waiting for input.
	type answer struct {
		line string
		err  error
	}
	answers := make(chan answer, 1)
	go func() {
		line, err := bufio.NewReader(input).ReadString('\n')
		answers <- answer{line, err}
	}()
	var line string
	select {
	case <-ctx.Done():
		return ctx.Err()
	case a := <-answers:
		if a.err != nil {
			return fmt.Errorf("input interrupted: %w", a.err)
		}
		line = strings.TrimSpace(a.line)
	}
	if line == "" {
		return nil
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(items) {
		return fmt.Errorf("select a project number from 1 to %d", len(items))
	}
	entry := items[n-1]
	file, err := os.OpenFile(filepath.Join(filepath.Dir(path), "error.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := command(ctx, entry.Target, output, "docker", "compose", "stop"); err != nil {
		logger := log.New(io.MultiWriter(file, os.Stderr), "["+entry.Target+"] ", log.LstdFlags)
		logger.Printf("ERROR: stop: %v", err)
		return fmt.Errorf("stop %s: %w", entry.Target, err)
	}
	_, err = fmt.Fprintf(output, "Stopped: %s\n", entry.Target)
	return err
}
