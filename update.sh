#!/usr/bin/env bash

# ============================================
# Auto-update Docker Compose project
# ============================================

set -Eeuo pipefail

# ============================================
# Configuration
# ============================================

# Project directory
PROJECT_DIR="/home/master/git/fi-ends"

# Directory where logs will be stored
LOG_DIR="$PROJECT_DIR/../log"

# Log file
LOG_FILE="$LOG_DIR/update.log"

# Project name
PROJECT_NAME="fi-ends"

# ============================================
# Telegram
# ============================================

# Set to "true" to enable Telegram notifications
# Set to "false" to disable Telegram notifications
TELEGRAM_ENABLED=false

TELEGRAM_BOT_TOKEN="YOUR_BOT_TOKEN"
TELEGRAM_CHAT_ID="YOUR_CHAT_ID"

# ============================================
# Runtime
# ============================================

LOCK_DIR="/tmp/${PROJECT_NAME}-update.lock"

# ============================================
# Prepare log directory
# ============================================

mkdir -p "$LOG_DIR"

# ============================================
# Logging
# ============================================

log() {
  local level="$1"
  shift

  echo "[$(date '+%Y-%m-%d %H:%M:%S')] [$level] $*" | tee -a "$LOG_FILE"
}

log_info() {
  log "INFO" "$@"
}

log_success() {
  log "SUCCESS" "$@"
}

log_warning() {
  log "WARNING" "$@"
}

log_error() {
  log "ERROR" "$@"
}

# ============================================
# Telegram
# ============================================

send_telegram() {
  local message="$1"

  if [[ "$TELEGRAM_ENABLED" != "true" ]]; then
    return 0
  fi

  if [[ -z "$TELEGRAM_BOT_TOKEN" || "$TELEGRAM_BOT_TOKEN" == "YOUR_BOT_TOKEN" ]]; then
    log_warning "Telegram is enabled but BOT TOKEN is not configured"
    return 0
  fi

  if [[ -z "$TELEGRAM_CHAT_ID" || "$TELEGRAM_CHAT_ID" == "YOUR_CHAT_ID" ]]; then
    log_warning "Telegram is enabled but CHAT ID is not configured"
    return 0
  fi

  if ! command -v curl >/dev/null 2>&1; then
    log_warning "curl is not installed. Telegram notification skipped."
    return 0
  fi

  if curl -fsS \
    --max-time 15 \
    -X POST \
    "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage" \
    --data-urlencode "chat_id=${TELEGRAM_CHAT_ID}" \
    --data-urlencode "text=${message}" \
    --data-urlencode "parse_mode=HTML" \
    >/dev/null; then

    log_success "Telegram notification sent"

  else
    log_warning "Failed to send Telegram notification"
  fi
}

# ============================================
# Cleanup
# ============================================

cleanup() {
  rm -rf "$LOCK_DIR"
}

trap cleanup EXIT

# ============================================
# Prevent parallel execution
# ============================================

if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  log_warning "Another update process is already running. Exiting."
  exit 0
fi

# ============================================
# Error handler
# ============================================

on_error() {
  local exit_code=$?

  log_error "Update failed with exit code: $exit_code"

  if [[ -d "$PROJECT_DIR" ]]; then
    cd "$PROJECT_DIR" || true

    log_error "Docker Compose status:"
    docker compose ps 2>&1 | tee -a "$LOG_FILE" || true

    log_error "Last Docker Compose logs:"
    docker compose logs --tail=30 2>&1 | tee -a "$LOG_FILE" || true
  fi

  exit "$exit_code"
}

trap on_error ERR

# ============================================
# Start
# ============================================

log_info "============================================"
log_info "Checking project: $PROJECT_NAME"
log_info "============================================"

# ============================================
# Validate project directory
# ============================================

if [[ ! -d "$PROJECT_DIR" ]]; then
  log_error "Project directory does not exist: $PROJECT_DIR"
  exit 1
fi

cd "$PROJECT_DIR"

# ============================================
# Validate dependencies
# ============================================

if ! command -v git >/dev/null 2>&1; then
  log_error "git is not installed or not available in PATH"
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  log_error "docker is not installed or not available in PATH"
  exit 1
fi

if [[ ! -d ".git" ]]; then
  log_error "Not a git repository: $PROJECT_DIR"
  exit 1
fi

# ============================================
# Current version
# ============================================

OLD_COMMIT=$(git rev-parse HEAD)
OLD_COMMIT_SHORT=$(git rev-parse --short HEAD)

log_info "Current commit: $OLD_COMMIT_SHORT"

# ============================================
# Protect local changes
# ============================================

if [[ -n "$(git status --porcelain)" ]]; then
  log_warning "Working tree contains local changes."
  log_warning "Automatic update skipped."
  exit 0
fi

# ============================================
# Fetch remote changes
# ============================================

log_info "Fetching updates from git..."

git fetch --prune

REMOTE_COMMIT=$(git rev-parse @{u})
REMOTE_COMMIT_SHORT=$(git rev-parse --short @{u})

log_info "Remote commit: $REMOTE_COMMIT_SHORT"

# ============================================
# Check for updates
# ============================================

if [[ "$OLD_COMMIT" == "$REMOTE_COMMIT" ]]; then
  log_info "No updates available."
  exit 0
fi

# ============================================
# Get commit information
# ============================================

COMMIT_MESSAGE=$(git log -1 --pretty=%s "$REMOTE_COMMIT")
COMMIT_AUTHOR=$(git log -1 --pretty=%an "$REMOTE_COMMIT")

log_info "New version available."
log_info "Commit: $OLD_COMMIT_SHORT -> $REMOTE_COMMIT_SHORT"
log_info "Author: $COMMIT_AUTHOR"
log_info "Message: $COMMIT_MESSAGE"

# ============================================
# Update repository
# ============================================

log_info "Pulling changes..."

git pull --ff-only

UPDATED_COMMIT=$(git rev-parse HEAD)
UPDATED_COMMIT_SHORT=$(git rev-parse --short HEAD)

log_success "Repository updated."
log_success "$OLD_COMMIT_SHORT -> $UPDATED_COMMIT_SHORT"

# ============================================
# Docker Compose
# ============================================

log_info "Building and starting Docker Compose..."

docker compose up -d --build --remove-orphans

log_success "Docker Compose started successfully."

# ============================================
# Docker status
# ============================================

log_info "Docker Compose status:"

docker compose ps | tee -a "$LOG_FILE"

# ============================================
# Telegram notification
# ============================================

HOSTNAME=$(hostname)

TELEGRAM_MESSAGE="🚀 <b>${PROJECT_NAME} updated</b>

<b>Commit:</b> <code>${UPDATED_COMMIT_SHORT}</code>
<b>Author:</b> ${COMMIT_AUTHOR}
<b>Message:</b> ${COMMIT_MESSAGE}

<b>Previous:</b> <code>${OLD_COMMIT_SHORT}</code>
<b>Current:</b> <code>${UPDATED_COMMIT_SHORT}</code>

<b>Host:</b> ${HOSTNAME}
<b>Status:</b> ✅ Docker Compose is running"

send_telegram "$TELEGRAM_MESSAGE"

# ============================================
# Done
# ============================================

log_success "============================================"
log_success "Update completed successfully."
log_success "============================================"
