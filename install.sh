#!/usr/bin/env bash
# ARTEX install script: (1) all Docker  (2) local compile and run
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }
ask(){  local p="$1" d="${2:-}" a; read -rp "$p${d:+ [$d]}: " a; echo "${a:-$d}"; }
rand(){ head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24; }

# ── Detect / auto-install docker ──────────────────
ensure_docker(){
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    ok "docker and docker compose found"; return
  fi
  warn "docker / docker compose not found"
  case "$(uname -s)" in
    Linux)
      if [ "$(ask 'Install Docker automatically? (y/n)' y)" = y ]; then
        curl -fsSL https://get.docker.com | sh
        sudo usermod -aG docker "$USER" || true
        ok "Docker installed (the user group change takes effect without sudo only after you log in again)"
      else
        die "please install docker yourself and run this again"
      fi ;;
    Darwin) die "on macOS, please install Docker Desktop: https://www.docker.com/products/docker-desktop/" ;;
    *)      die "please install docker yourself and run this again" ;;
  esac
}

# ── (1) All Docker ──────────────────────────────
install_docker(){
  ensure_docker
  if [ ! -f .env ]; then
    cp .env.example .env 2>/dev/null || true
    local pw key
    pw="$(ask 'Postgres password (press Enter to generate a random one)' "$(rand)")"
    key="$(ask 'ANTHROPIC_API_KEY (may be left empty and set later in the UI)' '')"
    sed -i.bak "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=${pw}|" .env
    sed -i.bak "s|^ANTHROPIC_API_KEY=.*|ANTHROPIC_API_KEY=${key}|" .env
    rm -f .env.bak
    ok "created the .env file (POSTGRES_PASSWORD set)"
  else
    info "using the existing .env file as is"
  fi
  info "pulling images and starting…"
  docker compose pull || true
  if ! docker compose up -d; then
    warn "could not start the ARTEX image (check ARTEX_IMAGE in .env: empty means artex-global:local, built from this checkout as described in the Dockerfile header)"
    warn "you can run this script again and choose \"2) Local run (go compile)\", or build from source (see the Installation section of README.md)"
    die "Docker Compose (all Docker) failed to start"
  fi
  ok "started → http://localhost:8787"
  info "on first visit you must enter the \"setup token\" on the /setup screen to create the admin password"
  info "find the setup token: docker compose logs artex | grep -F '[auth]'   (published on 127.0.0.1 by default; change with ARTEX_BIND in .env)"
  info "view logs: docker compose logs -f artex"
}

# ── (2) Local compile and run ────────────────────────────
install_local(){
  echo "Database setup:"
  echo "  1) Connect to an existing PostgreSQL"
  echo "  2) Start a PostgreSQL with Docker (requires docker)"
  case "$(ask 'Choice' 1)" in
    2)
      ensure_docker
      local pw; pw="$(ask 'Postgres password (press Enter for random)' "$(rand)")"
      docker run -d --name artex-pg -p 5432:5432 \
        -e POSTGRES_USER=artex -e POSTGRES_PASSWORD="$pw" -e POSTGRES_DB=artex \
        -v artex-pg:/var/lib/postgresql/data postgres:16-alpine
      DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=artex DB_PASS="$pw" DB_NAME=artex DB_SSL=disable ;;
    *)
      DB_HOST="$(ask 'Database host' 127.0.0.1)"
      DB_PORT="$(ask 'Port' 5432)"
      DB_USER="$(ask 'User' artex)"
      DB_PASS="$(ask 'Password' '')"
      DB_NAME="$(ask 'Database name' artex)"
      DB_SSL="$(ask 'sslmode (disable/require)' disable)" ;;
  esac

  # Generate config.json
  cat > config.json <<JSON
{
  "database": {
    "host": "${DB_HOST}",
    "port": ${DB_PORT},
    "user": "${DB_USER}",
    "password": "${DB_PASS}",
    "dbname": "${DB_NAME}",
    "sslmode": "${DB_SSL}"
  }
}
JSON
  ok "created the config.json file"

  # Check the go environment
  command -v go >/dev/null 2>&1 || die "Go not found. Please install Go (>=1.26) first: https://go.dev/dl/"
  ok "Go: $(go version)"

  # Embedding the frontend requires building the static output with node
  if command -v npm >/dev/null 2>&1; then
    info "building static frontend output…"
    ( cd web && npm ci && npm run build:static )
    rm -rf server/webui/dist && mkdir -p server/webui && cp -r web/out server/webui/dist
    info "compiling the single binary with the embedded frontend…"
    CGO_ENABLED=0 go build -tags embedui -trimpath -o artex ./cmd/artex
  else
    warn "npm not found: compiling only the backend without the embedded frontend (run the frontend separately with npm run dev)"
    CGO_ENABLED=0 go build -o artex ./cmd/artex
  fi
  ok "compiled → ./artex"

  info "starting… (Ctrl-C to stop). Binds to 127.0.0.1:8787 by default; on first visit, enter the [auth] setup token from the log below on the /setup screen"
  ./artex
}

echo "=============================="
echo "  ARTEX install"
echo "  1) Install everything with Docker"
echo "  2) Local run (go compile)"
echo "=============================="
case "$(ask 'Choice' 1)" in
  1) install_docker ;;
  2) install_local ;;
  *) die "invalid choice" ;;
esac
