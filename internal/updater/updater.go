package updater

import (
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

func run(ctx context.Context, path string) (runErr error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	lock, err := acquireConfigLock(path)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	c, err := readConfig(path)
	if err != nil {
		return err
	}
	logDir, err := prepareLogDir(path, c)
	if err != nil {
		return err
	}
	errorLogLimit, outputLogLimit := c.logLimits()
	file, err := openErrorLog(filepath.Join(logDir, "error.log"), errorLogLimit)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	logger := log.New(os.Stdout, "", log.LstdFlags)
	green, reset := "", ""
	terminal := false
	if os.Getenv("TERM") != "dumb" && os.Getenv("TERM") != "" {
		if info, err := os.Stdout.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			terminal = true
			if os.Getenv("NO_COLOR") == "" {
				green, reset = "\033[32m", "\033[0m"
			}
		}
	}
	errorLogger := log.New(io.MultiWriter(file, os.Stderr), "", log.LstdFlags)
	tg := resolveTelegram(c.TG)
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
			line := "\n" + entry.Target + ": " + statuses[i] + "\n  " + diskSpaceSummary(entry.Target)
			// Keep each message below Telegram’s 4096-character limit.
			if len([]rune(line)) > 3500 {
				line = string([]rune(line)[:3500]) + "…"
			}
			if len([]rune(message+line)) > 4000 {
				notify(context.WithoutCancel(ctx), tg, message, logger)
				message = "Отчёт updater (продолжение)"
			}
			message += line
		}
		if hasReport {
			notify(context.WithoutCancel(ctx), tg, message, logger)
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
	if terminal {
		_, _ = fmt.Fprint(os.Stdout, "\n\n")
	}
	for i, entry := range c.Items {
		if entry.Enabled != nil && !*entry.Enabled {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		diskWarningSent := false
		checkDisk := func() {
			if c.Log != nil && c.Log.FreeSize != nil && !diskWarningSent {
				threshold, _ := parseFreeSize(*c.Log.FreeSize) // Already validated by readConfig.
				diskWarningSent = checkFreeSpace(ctx, entry.Target, threshold, tg, logger)
			}
		}
		checkDisk()
		var transcript *projectTranscript
		var transcriptOutput = io.Discard
		if outputLogLimit > 0 {
			file, err := os.CreateTemp(logDir, "project-output-*.log")
			if err != nil {
				return fmt.Errorf("create project output log: %w", err)
			}
			transcript = &projectTranscript{File: file, limit: outputLogLimit}
			transcriptOutput = transcript
		}
		var display io.Writer = os.Stdout
		var progress *projectOutput
		if terminal {
			_, _ = fmt.Fprint(os.Stdout, "\033[?7l")
			log.New(os.Stdout, "["+green+entry.Target+reset+"] ", log.LstdFlags).Print("Checking project")
			_, _ = fmt.Fprint(os.Stdout, "\033[?7h")
			progress = newProjectOutput(os.Stdout)
			display = progress
		} else {
			_, _ = fmt.Fprint(os.Stdout, "\n\n")
		}
		output := io.MultiWriter(transcriptOutput, display)
		logger.SetOutput(output)
		logger.SetPrefix("[" + entry.Target + "] ")
		if terminal {
			log.New(transcriptOutput, "["+entry.Target+"] ", log.LstdFlags).Print("Checking project")
		} else {
			logger.Print("Checking project")
		}
		status, updateErr := update(ctx, entry, output, logger)
		if transcript != nil {
			if closeErr := transcript.Close(); closeErr != nil {
				updateErr = errors.Join(updateErr, fmt.Errorf("save project output: %w", closeErr))
			}
		}
		logger.SetOutput(os.Stdout)
		statuses[i] = status
		icon := "✅"
		if strings.HasPrefix(status, "⏭") {
			icon = "⏭"
		}
		if updateErr != nil {
			icon = "❌"
		}
		if progress != nil {
			progress.finish(green+entry.Target+reset, icon)
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "%s [%s]\n", icon, entry.Target)
		}
		if updateErr != nil {
			projectErrors[i] = true
			statuses[i] = "❌ ошибка (подробности в logs/error.log)"
			if errorLogLimit == 0 {
				statuses[i] = "❌ ошибка (подробности в stderr)"
			}
			errorLogger.SetPrefix("[" + entry.Target + "] ")
			logPath := ""
			if transcript != nil {
				logPath = "; output: " + transcript.Name()
			}
			errorLogger.Printf("ERROR: %v%s", updateErr, logPath)
			failed = true
		} else if transcript != nil {
			if err := os.Remove(transcript.Name()); err != nil {
				return fmt.Errorf("remove successful project output log: %w", err)
			}
		}
		checkDisk()
	}
	if failed {
		return errors.New("one or more projects failed to update")
	}
	return nil
}
