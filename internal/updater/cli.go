package updater

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

// Main runs the updater command-line interface.
func Main() {
	executable, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	path := flag.String("config", filepath.Join(filepath.Dir(executable), "config.json"), "path to config.json")
	flag.Usage = func() {
		_, _ = fmt.Fprintln(flag.CommandLine.Output(), `Usage: updater [-config path] [add [directory]|stop]

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
