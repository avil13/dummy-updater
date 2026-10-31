package updater

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func parseFreeSize(value string) (int, error) {
	percent, err := strconv.Atoi(strings.TrimSuffix(value, "%"))
	if err != nil || percent < 0 || percent > 100 || strconv.Itoa(percent)+"%" != value {
		return 0, fmt.Errorf("log.free_size: must be an integer percentage from 0%% to 100%%")
	}
	return percent, nil
}

func lowDiskSpace(available, total uint64, threshold int) bool {
	return total > 0 && float64(available)/float64(total)*100 < float64(threshold)
}

func targetDiskStats(path string) (syscall.Statfs_t, error) {
	var stat syscall.Statfs_t
	for {
		err := syscall.Statfs(path, &stat)
		parent := filepath.Dir(path)
		if !os.IsNotExist(err) || parent == path {
			return stat, err
		}
		path = parent
	}
}

func diskSpaceSummary(path string) string {
	stat, err := targetDiskStats(path)
	if err != nil || stat.Blocks == 0 {
		return "💾 свободное место: не удалось определить"
	}
	freeGB := float64(stat.Bavail) * float64(stat.Bsize) / 1_000_000_000
	percent := float64(stat.Bavail) / float64(stat.Blocks) * 100
	return fmt.Sprintf("💾 свободно %.2f ГБ (%.2f%%)", freeGB, percent)
}

func checkFreeSpace(ctx context.Context, path string, threshold int, tg telegramConfig, logger *log.Logger) bool {
	stat, err := targetDiskStats(path)
	if err != nil {
		logger.Printf("WARNING: check free disk space for %s: %v", path, err)
		return false
	}
	if !lowDiskSpace(uint64(stat.Bavail), uint64(stat.Blocks), threshold) {
		return false
	}
	host, _ := os.Hostname()
	message := fmt.Sprintf("⚠️ Мало свободного места — %s: %s; свободно %.2f%%, порог %d%%", host, path, float64(stat.Bavail)/float64(stat.Blocks)*100, threshold)
	red, reset := "", ""
	if os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "" && os.Getenv("TERM") != "dumb" {
		if info, err := os.Stderr.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			red, reset = "\033[31m", "\033[0m"
		}
	}
	log.New(os.Stderr, "", log.LstdFlags).Print(red + message + reset)
	notify(ctx, tg, message, logger)
	return true
}
