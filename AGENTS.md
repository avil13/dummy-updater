# Repository guidance

## Project

`dummi-updater` is a Go 1.22 CLI that sequentially updates Git repositories and runs a configured shell command in each target. Supported platforms are Linux and macOS, on amd64 and arm64. It currently uses only the Go standard library.

## Layout

- `cmd/updater/main.go`: thin CLI entry point; CLI integration tests live beside it.
- `internal/updater/`: config validation, Git updates, command retries, locking, logging, Telegram notifications, and CLI handling. Tests live beside the implementation.
- `update.sh`: selects the platform binary and defaults to config beside the script.
- `config.example.json`: sample configuration; keep real credentials out of the repository.
- `README.md`: user documentation, written in Russian; preserve its language when editing.
- `.githooks/pre-commit`: tests and builds the staged source snapshot.
- `dist/`: binaries for all four supported platform/architecture combinations.

## Working conventions

- Make focused changes; keep application logic in the single `internal/updater` package with a thin `cmd/updater` entry point, and prefer the standard library.
- Format changed Go files with `gofmt`.
- Add focused regression tests for changed behavior. Use temporary directories and fake executables on `PATH`, following existing tests; do not require real Docker, remote repositories, or Telegram.
- Update the README and example config when changing user-facing behavior or configuration.
- Whenever config fields, types, defaults, required fields, or validation rules change, update `schema/v1/schema.json` in the same change. Keep the schema consistent with config validation, `config.example.json`, and the README.
- Do not run `update.sh` or the updater against real targets as a test: they can pull repositories and execute deployment commands.

## Behavior to preserve

- Require nonempty `repo`, `target`, and `run`; reject unknown JSON fields and duplicate normalized targets. Relative targets resolve against the config directory.
- Clone missing targets; require existing targets to be repositories with an exactly matching `origin`. Skip dirty repositories, including untracked files; use `git pull --ff-only` without discarding local work.
- Execute trusted `run` commands via `sh -c` in the target, even without new commits. Default to two additional attempts; `retry: 0` disables retries. Do not retry Git operations; respect cancellation.
- Process projects sequentially, continuing after individual failures and returning a failure exit status if any project fails.
- Keep the per-config OS lock beside the config and error-only `logs/error.log` in a `logs/` directory beside the config, with errors also sent to stderr. Retention cleanup must only delete expired regular `project-output-*.log` files inside that directory, never unrelated files, directories, or symlinks.
- Treat configs and repositories as trusted executable input. Never commit credentials or expose Telegram tokens in logs.

## Verification

Run from the repository root after Go changes:

```sh
go test ./...
go vet ./...
```

For shell changes, check syntax with `bash -n update.sh .githooks/pre-commit`. Tests do not exercise real Docker Compose; report that limitation when relevant.

## Build workflow

Enable the commit hook in a new clone with `git config core.hooksPath .githooks` when requested. The hook first runs `golangci-lint run --fix` on the working tree and aborts if Go files change, leaving fixes unstaged for review. It then tests the **staged** snapshot, builds all four binaries with CGO disabled, and stages them in `dist/`. Do not invoke it merely as a check: it modifies binaries and the index. See `README.md` for manual cross-build commands; avoid regenerating binaries for documentation-only changes.
