#!/usr/bin/env bash
# ARTEX update script: (1) Docker update (pull the new image and recreate)  (2) local compile update (rebuild the binary)
# Pairs with install.sh: install handles the first installation, update handles upgrading to a new version.
# You do not need to run DB migrations yourself. artex idempotently reruns schema.sql on every start (including ADD COLUMN/CREATE
# INDEX IF NOT EXISTS), so "restart is migration". Data (the pgdata volume, ./data, ./skills) is not affected.
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }
ask(){  local p="$1" d="${2:-}" a; read -rp "$p${d:+ [$d]}: " a; echo "${a:-$d}"; }

# ── Optional: sync the repo to the latest code (this updates compose, scripts and local compile sources alike) ───────
sync_repo(){
  [ -d .git ] && command -v git >/dev/null 2>&1 || { warn "not a git working copy, skipping git pull"; return; }
  [ "$(ask 'Fetch the latest code (git pull --ff-only)? (y/n)' y)" = y ] || return
  if ! git pull --ff-only; then
    warn "git pull could not fast-forward (there are local changes or the branch has diverged). Resolve it yourself and try again. Using the current code for now"
  fi
}

# ── (1) Docker update ───────────────────────────────
update_docker(){
  command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 \
    || die "docker / docker compose not found. Install and deploy with ./install.sh first"
  [ -f .env ] || die ".env not found. Complete the first deployment with ./install.sh first"

  # Optional: upgrade to a specific version tag (empty follows ARTEX_TAG in .env, which defaults to latest)
  local tag; tag="$(ask 'Target image tag (press Enter to use .env / latest)' '')"
  if [ -n "$tag" ]; then
    if grep -q '^ARTEX_TAG=' .env; then
      sed -i.bak "s|^ARTEX_TAG=.*|ARTEX_TAG=${tag}|" .env && rm -f .env.bak
    else
      printf '\nARTEX_TAG=%s\n' "$tag" >> .env
    fi
    ok "set ARTEX_TAG to ${tag}"
  fi

  # Only artex is touched. postgres is pinned to 16-alpine, so it does not need to be bumped along (pulling it wastes bandwidth,
  # and a major version change risks compatibility). artex declares depends_on postgres, so `up` with the service name
  # starts pg automatically if it is not running, and leaves it as is without recreating it if it already is.
  info "pulling the new image (artex only)…"
  if ! docker compose pull artex; then
    warn "could not pull the image set in ARTEX_IMAGE (check that it names an image you trust and that the network works)"
    die "image update failed. You can also update via \"2) Local update (recompile with go)\""
  fi
  info "recreating and starting (artex migrates the schema automatically on restart)…"
  docker compose up -d artex
  ok "update complete → http://localhost:8787"
  info "view logs: docker compose logs -f artex"
  info "clean up old images (optional): docker image prune -f"
}

# ── (2) Local compile update ──────────────────────────────
update_local(){
  command -v go >/dev/null 2>&1 || die "Go not found (>=1.26): https://go.dev/dl/"
  [ -f config.json ] || warn "config.json not found. For a first deployment, use ./install.sh"
  ok "Go: $(go version)"

  if command -v npm >/dev/null 2>&1; then
    info "rebuilding static frontend output…"
    ( cd web && npm ci && npm run build:static )
    rm -rf server/webui/dist && cp -r web/out server/webui/dist
    info "recompiling the single binary with the embedded frontend…"
    CGO_ENABLED=0 go build -tags embedui -trimpath -o artex ./cmd/artex
  else
    warn "npm not found. Compiling only the backend without the embedded frontend (run the frontend separately with npm run dev)"
    CGO_ENABLED=0 go build -o artex ./cmd/artex
  fi
  ok "compiled → ./artex"
  warn "restart the running artex process to apply the changes (the schema is migrated automatically on restart)"
}

echo "=============================="
echo "  ARTEX update"
echo "  1) Docker update (pull the new image and recreate)"
echo "  2) Local update (recompile with go)"
echo "=============================="
case "$(ask 'Choice' 1)" in
  1) sync_repo; update_docker ;;
  2) sync_repo; update_local ;;
  *) die "invalid choice" ;;
esac
