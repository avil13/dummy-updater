# dummi-updater

Sequentially updates Git repositories and executes the command from the required
`run` field in each target.

## Requirements

- Linux or macOS (Intel / Apple Silicon).
- Git and repository access via SSH keys or a credential helper.
- Docker running and the `docker compose` command available.
- A Compose file in the root of each target repository.
- Go 1.22+ only for building and pre-commit; not needed for prebuilt binaries.

## Configuration

1. Add a project with `./update.sh add` or fill in `config.json` manually:
   `repo` is the clone URL, `target` is the project directory, and `run` is a nonempty launch command.
   All three fields are required. The optional `retry` field sets the number of
   additional `run` attempts after a failure (integer ≥ 0, default: 2).
   `retry: 0` disables retries. Add as many items as needed.
   `enabled: false` disables an item: updates skip it and `stop` omits it from the list.
   `enabled: true` enables it; an omitted field (or `null`) means `true` for compatibility.
   Disabled entries must still pass configuration validation.
   Changing this field alone does not stop running containers.
   For an existing project, `repo` must exactly match `git remote get-url origin`.
2. Install Git, Docker, and the Compose plugin on Linux or macOS.
3. Run `bash update.sh` or the appropriate binary:
   `./dist/updater-linux-amd64 -config ./config.json`.

Example `config.json` for two projects:

```json
{
  "$schema": "https://raw.githubusercontent.com/avil13/dummy-updater/master/schema/v1/schema.json",
  "log": {
    "max_err_mb": 1,
    "max_project_mb": 0.5,
    "retention_days": 7,
    "free_size": "10%"
  },
  "items": [
    {
      "repo": "git@github.com:YOUR_USER/backend.git",
      "target": "/srv/backend",
      "run": "docker compose up -d --build --remove-orphans",
      "retry": 2,
      "enabled": true
    },
    {
      "repo": "git@github.com:YOUR_USER/frontend.git",
      "target": "/srv/frontend",
      "enabled": true,
      "run": "docker compose up -d --build --remove-orphans"
    }
  ]
}
```

The `$schema` field connects the [JSON Schema](schema/v1/schema.json) for editor
suggestions and configuration validation. The CLI accepts this field without
downloading the schema and preserves the URL during `add`. The CLI still checks
normalized target uniqueness and repository state.
The GitHub URL becomes available once the schema is published on the `master` branch.

Replace the URLs and paths with your own values. The key is `repo`, without a colon.
On macOS, use accessible paths such as `/Users/your-user/projects/backend`.

## Running

```sh
# Automatically select the binary and JSON next to the script
bash update.sh

# Command and option help (both forms are equivalent)
./update.sh -h
./update.sh --help

# Use a different configuration
bash update.sh -config /absolute/path/config.json

# Run directly, for example on Apple Silicon
./dist/updater-darwin-arm64 -config ./config.json
```

`update.sh` automatically selects Linux/macOS and amd64/arm64.
For macOS, use `dist/updater-darwin-amd64` (Intel) or
`dist/updater-darwin-arm64` (Apple Silicon).

Without `-config`, JSON is loaded from beside the binary. Relative targets are
resolved relative to the JSON file. For a standalone installation, copy the
binary and JSON into the same directory.

## Adding a project

```sh
./update.sh add
./update.sh add ../path/to/repo
./update.sh add -config /absolute/path/config.json
# Directly through the binary
./dist/updater-darwin-arm64 add -config ./config.json
```

The command prompts for `repo`, `target`, `run`, `retry`, and `enabled` in English.
If a local Git repository path is supplied, it is resolved relative to the current
directory: its absolute path is suggested as `target`, and its `origin` URL as `repo`.
Enter accepts the suggested value; other input replaces it.
The directory must contain `.git` and an `origin` remote, otherwise the configuration is unchanged.
In a terminal, prompts are blue and success messages are green.
Colors are disabled when output is redirected, when `TERM=dumb`, or when `NO_COLOR`
is nonempty (for example, `NO_COLOR=1 ./update.sh add`).
`repo` and `target` are required. The suggested `run` command is
`docker compose up -d --build --remove-orphans`: press Enter to accept it or enter your own command.
For `retry`, Enter saves an explicit value of `3` (3 additional attempts);
`0` disables retries. This default applies to new entries created by `add`;
existing entries are unchanged.
For `enabled`, Enter or `true` saves `true`; `false` disables the item.
Other values are rejected without changing the configuration.
Relative `target` paths are resolved relative to the configuration directory.
If the JSON file does not exist, the command creates it. An existing configuration
must be valid; its entries and relative paths are preserved.
On a successful `add`, missing global settings (including `null` values) are filled in:
`$schema` uses the example URL, `log.max_err_mb` is `1`, `log.max_project_mb` is `0.5`,
`log.retention_days` is `7`, and `log.free_size` is `"10%"`.
The `tg` block is saved with all fields: defaults are `enabled: false`,
`only_errors: false`, `bot_token: ""`, and `chat_id: ""`.
Existing values, including `0`, `false`, and `"0%"`, are not replaced.
This enables disk space checking for configurations without `free_size`;
set it to `"0%"` to disable checking. An added `tg` block has `enabled: false`:
enable notifications manually. Empty `bot_token` and `chat_id` values are read
from the corresponding `TELEGRAM_*` environment variables.
Optional fields in existing items are not filled in.
Duplicate targets and invalid input are rejected without changing the JSON.
Interrupted input also leaves the file unchanged; correct the values and try again.
The file is saved atomically with `0600` permissions (owner-only access), using
the same lock as updates. When a repository path is supplied, Git is used only
to read `origin`. Docker and project commands are not executed.

## Interactive project list

```sh
./update.sh ls
./update.sh ls -config /absolute/path/config.json
# Directly through the binary; -config can also precede ls
./dist/updater-darwin-arm64 -config ./config.json ls
```

Requires an interactive terminal and the standard `stty` utility (Linux/macOS).
The list shows all `config.items`, including disabled entries.
Use ↑/↓ or `j`/`k` to select a project; Enter opens its menu.
`q` or Esc goes back one level, or exits `ls` from the project list.
Ctrl+C exits the command; terminal settings are restored on exit.

The project menu provides:
- **Edit settings** — prompts for `repo`, `target`, `run`, `retry`, and `enabled`.
  Enter without input keeps the current value (for omitted `retry` and `enabled`,
  the defaults are 2 and true).
  Backspace deletes a character; Esc cancels all changes; `q` is ordinary text in a field.
  Global `log` and `tg` settings are not editable here.
  After validating the entire configuration, the file is atomically replaced with `0600` permissions.
  Relative paths are preserved; errors, a lock, or another process changing the
  configuration during input prevent the write.
- **Run project** — performs the normal Git update and `run` handling for the
  selected target only, with the same retries, locking, logs, and Telegram notifications.
  For a disabled project, first set `enabled: true`.
  During execution, `q`/Esc do not interrupt commands; use Ctrl+C to cancel.
- **View logs** — shows errors for this target from `error.log` and its saved
  output files (newest first). Enter opens a log, ↑/↓ scrolls, and Enter/`q`/Esc goes back.
  Output from successful runs is still not saved. History may be unavailable
  when logging is disabled or after cleanup.

Output files are now named `project-output-<SHA256 of absolute target>-<random suffix>.log`.
This allows locating a log even after its beginning or `error.log` has been truncated.
Older files without an identifier are shown if this project's error entry still
contains a reference to them. Symbolic links and directories are not opened as output files.
Changing a target leaves its previous history associated with the old path.
Control characters are removed in the log viewer to prevent command output from controlling the terminal.
The menu requires a terminal at least 20 lines high.

## Stopping containers

```sh
./update.sh stop
./update.sh stop -config /absolute/path/config.json
# Directly through the binary
./dist/updater-darwin-arm64 stop -config ./config.json
```

The command shows a numbered list of `items` (their `target` paths). Enter a project
number to execute `docker compose stop` in its directory, stopping all services
of that Compose project without removing containers, networks, or volumes.
Other projects are unaffected. Enter cancels selection; invalid or interrupted
input exits without running Docker.
Git and `run` are not executed, and there are no retries. The same configuration
lock is used; stop errors are written to `logs/error.log` and stderr.
The command uses the standard Compose file and current process environment,
not `-f`/`-p` options from `run`. The JSON format is unchanged.
The next normal updater invocation will execute `run` again and may start the containers.

## Update behavior

A missing target is cloned. An existing target is updated with
`git pull --ff-only` (using the current branch and its upstream).
Local changes, including untracked files, cause the project to be skipped.
Errors do not stop other projects; the final exit code is 1 if any project fails.
The `HEAD` hash is read before and after `pull`. If the hash is unchanged,
only `docker compose config --services` and `docker compose ps --all --format json`
checks are run in `target`. If every service has a container, all containers
are `running`, and their healthchecks (if configured) report `healthy`, `run` is skipped.
These checks do not restart containers. Without a healthcheck, only the container
state is checked, not application health.
The standard Compose file and updater environment are used; `-f`/`-p` options
and variables in the `run` string are not taken into account.
When the hash changes, after cloning, when containers are stopped/unhealthy,
when services are missing, or when a check fails, `run` is executed through `sh -c`
in `target`. On failure, `run` is retried immediately, up to `retry` additional
times; retries stop after success. Git operations are not retried.
Stopping updater also stops retries.
Quotes, environment variables, and shell operators (`&&`, `|`, and others) are supported.
The `docker compose ps` command after execution and `docker compose logs --tail=30`
on failure remain fixed.
For `docker compose up`, this is not an unconditional restart: Compose may leave
unchanged containers running.

Logs are stored in a `logs/` directory beside the JSON file (created with `0700` permissions).
Project update errors are written to `logs/error.log` and stderr, including the
project path and the cause of the error.
The size of `error.log` is controlled by `log.max_err_mb` (default: 1 MB): the end
of the log is kept, with the first line truncated if necessary. The limit applies
to updates and `stop`; an existing oversized log is truncated when opened.
`log.max_err_mb: 0` disables the file, but errors still appear on stderr.
Informational messages, warnings, and command output remain in the console.
In a terminal, the current project's output is refreshed in a 10-line block under
`Checking project`. A completed block is replaced with a `✅ [target]` or
`❌ [target]` line; previous project results remain above it.
Projects skipped because of local changes are shown as `⏭ [target]`.
Long lines are truncated to the terminal width; at least 12 lines of terminal height are required.
Two newlines are added before the first project.
The path in brackets is green unless `NO_COLOR` is set.
When stdout is redirected, `TERM` is empty, or `TERM=dumb`, plain output is used
without ANSI or a 10-line limit, with two newlines separating projects.

Each project's output is temporarily recorded in `logs/`. `log.max_project_mb`
sets the maximum size of each `project-output-*.log` file; the default is `0.5` MB
(500,000 bytes). Both limits use MB (1 MB = 1,000,000 bytes) and accept fractional values.
An omitted field or `null` uses the default value.
When the limit is exceeded, only the end of the output is kept; its beginning may
be in the middle of a line. On failure, the file is retained and its path is
included in `error.log` and stderr; on success, the file is deleted. File permissions are `0600`.
`log.max_project_mb: 0` disables these files, but not `error.log`.
The old `output_log_max_kb` field has been replaced with `log.max_project_mb`.
Disabling log writes does not disable cleanup according to `log.retention_days`.
After an abnormal exit, the ring-buffer file may be out of order; after normal
project completion, its entries are chronological.
Command output may contain secrets: review these files before publishing them.
`log.retention_days` sets project log retention in whole days
(default: `7`; an omitted field or `null` uses this default).
At the start of updates or `stop`, only regular `logs/project-output-*.log` files
last modified at least that many days ago are removed.
`0` disables cleanup. Other files, subdirectories, and symbolic links are untouched;
`error.log` is limited by size, not age.
`log.free_size: "10%"` enables free disk space checks for each `target` before and
after processing the project. If the target does not yet exist, the disk of its
nearest existing parent directory is checked.
Disabled projects are not checked. Space available to the current user is used.
The value is a string containing an integer percentage from `0%` to `100%`;
an omitted field or `null` disables checking, and `0%` disables warnings.
If free space is strictly below the threshold, a warning is printed to stderr
(red in a terminal unless `NO_COLOR` is set) and immediately sent to Telegram
if enabled, including when `only_errors: true`.
A warning is sent at most once per target per invocation; updates are not interrupted.
Check failures are reported as warnings. This is not background monitoring:
disk space is not checked while a command is running. `add` and `stop` do not perform this check.

Old logs beside the JSON file are not moved or deleted automatically.
Failure to create the log directory or clean up logs aborts execution before commands run.
The lock prevents concurrent runs with the same JSON file and is released by the OS
when the process exits. Do not run different configurations with overlapping targets concurrently.
Use only trusted repositories and JSON: `run` executes arbitrary shell commands
with the updater user's permissions, and Compose runs repository code.
The `run` command must support repeated execution: after a failure, the entire command is retried.
Do not put tokens in repository URLs; use SSH keys or a Git credential helper.

## Telegram

Add an optional block at the root of `config.json`:

```json
"tg": {
  "enabled": true,
  "only_errors": true,
  "bot_token": "TOKEN_FROM_BOTFATHER",
  "chat_id": "-1001234567890"
}
```

When `enabled: true`, the token and chat ID must be set in the configuration or environment.
Empty, omitted, `null`, or whitespace-only `bot_token` and `chat_id` values are read
from `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID`, respectively.
Nonempty configuration values take precedence; environment secrets are not written to JSON.
Notifications are disabled in `config.example.json`: fill in the fields and enable them.

### Channel setup

Delivery works like [ssh-telegram-alert](https://github.com/avil13/ssh-telegram-alert):
an HTTP POST to Telegram Bot API `sendMessage` with the bot token and channel ID.
Link previews are disabled. Messages are sent as plain text without Markdown,
so special characters in paths cannot break the report. Sending finishes before updater exits.

1. Create a bot through @BotFather and obtain its token.
2. Create a private or public channel. Add the bot as an administrator with
   permission to post messages, then publish a new post in the channel.
3. Open the following URL in a browser, replacing `<TOKEN>` with the bot token:

   ```text
   https://api.telegram.org/bot<TOKEN>/getUpdates
   ```

4. Find `channel_post.chat.id` in the response. For example, the `chat` object might be:

   ```json
   {
     "id": -1001234567890,
     "title": "Updater notifications",
     "type": "channel"
   }
   ```

5. Copy the ID into `tg.chat_id` **as a string**, preserving the minus sign,
   as shown in the configuration example above. Set the token in `tg.bot_token`
   and enable `tg.enabled: true`.

For a public channel, `"@channel_name"` can be used instead of a numeric ID.
Groups and private chats are also supported; their ID is available in `message.chat.id`.

If the response contains `"result": []`, publish a new post and refresh the page.
This method does not work when a webhook is configured.
Do not publish the token or the URL containing it: they grant control over the bot.

### Report contents

`only_errors: true` sends only projects with a final error. If there are no errors,
no message is sent. Skipped projects, disabled projects, and failed `run` attempts
followed by a successful retry are not included in this report.
A startup preparation error (for example, missing Git) after reading the configuration
and opening the log also triggers a brief notification; details remain on stderr.
Configuration reading, locking, and log opening errors do not trigger notifications.

With `only_errors: false` (the default), a brief report is sent after updating for
each target: cloned, updated, no new commits (with `run` executed or skipped when
Compose is healthy), error, local changes, disabled, or not run.
Error details remain in `logs/error.log`.
The report includes free disk space for each target at the time of sending,
for example: `💾 free 42.50 GB (18.30%)`.
1 GB = 1,000,000,000 bytes; space available to the current user is used.
If the target does not exist, the nearest existing parent directory is used;
if the check fails, the report shows "unavailable" instead of numbers.
This information is independent of the `log.free_size` threshold but follows
`only_errors`: if no report is sent, there is no separate disk space message.
Long reports are split into multiple messages; HTML and command output are not sent.
Telegram errors are printed as warnings and do not change the update result.
The `add` and `stop` commands do not send reports.

`tg.enabled: false` or `tg: {}` disables notifications. If `tg` is omitted,
`TELEGRAM_ENABLED=true`, `TELEGRAM_BOT_TOKEN`, and `TELEGRAM_CHAT_ID` are used
for compatibility; these also enable the final report instead of individual notifications.

## Source layout

- `cmd/updater/main.go` — CLI entry point.
- `internal/updater/` — configuration, project updates, CLI commands, Telegram,
  and logging; tests live beside the implementation.
- `cmd/updater/main_test.go` — tests for the built CLI.

## Building and pre-commit

Go 1.22+ and `golangci-lint` on `PATH` are required. Enable the hook in each new clone:

```sh
git config core.hooksPath .githooks
```

On commit, the hook first runs `golangci-lint run --fix` in the working directory.
If Go files change, the commit is stopped: review the changes, stage them, and retry.
The hook does not stage fixed source files automatically. Lint errors also block the commit.
If there are no fixes, the hook tests the **staged** source snapshot and builds
all four `dist/updater-{linux,darwin}-{amd64,arm64}` binaries, then stages them for the commit.
Before the first commit, stage the source files.
Neither the tests nor the hook run Docker.

## Verification

```sh
go test ./...
go vet ./...
```

Current tests cover JSON reading and validation, executing `run` in the target
(including retries and failures), interactive project addition, editing and
navigation in `ls`, selective execution, log filtering, and error-only logging;
they do not run Docker.
Check Compose behavior separately in your own environment.

## Manual build

```sh
mkdir -p dist
for os in linux darwin; do
  for arch in amd64 arm64; do
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "dist/updater-$os-$arch" ./cmd/updater
  done
done
```
