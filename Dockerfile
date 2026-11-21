# syntax=docker/dockerfile:1
#
# Runtime-only image (no compilation inside the image): installs common tools only and adds a **precompiled Linux single binary**.
# The binary is cross-compiled by CI's binaries job (pure Go, no QEMU) and placed per target architecture at
# dist/<TARGETARCH>/artex in the build context. This way, in multi-arch builds arm64 only needs to emulate the
# apt layer, and Next/Go compilation is no longer emulated, which is much faster.
#
# When building the image manually locally, prepare the binary yourself first:
#   cd web && npm run build:static && cd ..
#   mkdir -p server/webui && cp -r web/out server/webui/dist
#   CGO_ENABLED=0 GOARCH=amd64 go build -tags embedui -o dist/amd64/artex ./cmd/artex
#   docker build -t artex:local .
FROM python:3.12-slim-bookworm
ARG TARGETARCH
# Common tools: ripgrep / curl / vim plus a bundle of tools often used for recon (add or remove as needed).
# Node 20.x is installed from NodeSource: bookworm's default apt nodejs is 18, and Playwright requires >=20.
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates ripgrep curl wget vim git jq unzip \
      dnsutils iputils-ping netcat-openbsd inetutils-telnet whois nmap \
    && curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*
# Preinstall Playwright MCP and CLI globally (so nothing is downloaded over the network via npx at runtime).
# @playwright/mcp: the browser MCP runs directly via `npx @playwright/mcp` (installed globally, no -y/@latest needed).
# @playwright/cli: provides playwright-cli; after installation, --help verifies it runs.
# Next install playwright (provides browser management) and preinstall chromium and its system dependencies with --with-deps.
# This lets the MCP/CLI in the container work immediately on first start without downloading a browser over the network.
RUN npm install -g @playwright/mcp@latest @playwright/cli@latest playwright@latest \
    && playwright-cli --help \
    && playwright install --with-deps chromium \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
# Precompiled binary for this architecture (dist/amd64/artex or dist/arm64/artex)
COPY dist/${TARGETARCH}/artex /app/artex
# Supervisor start script: after the process exits, decides from the exit code whether to restart it; the UI's one-click update completes the swap through this script.
# The script also forwards SIGTERM to artex: docker stop only signals PID 1, so without
# forwarding artex never receives the signal, cannot shut down gracefully, and is SIGKILLed after 10 seconds.
COPY start.sh /app/start.sh
RUN chmod +x /app/artex /app/start.sh
COPY skills/ /app/skills/
# Persistence point for data/ (SQLite + jwt.key)
VOLUME ["/app/data"]
EXPOSE 8787 8788
ENTRYPOINT ["/app/start.sh"]
CMD ["-addr", ":8787", "-proxy", ":8788"]
