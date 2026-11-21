#!/bin/sh
# ARTEX daemon start script (Linux / macOS / Docker ENTRYPOINT)
#
# Usage:
#   ./start.sh                       run in the foreground (Ctrl-C to stop)
#   nohup ./start.sh >artex.log 2>&1 &   keep running in the background
#   ./start.sh -addr :9000           extra arguments are passed through to artex
#
# This script does one thing: it runs artex and, when the process exits, decides from the exit code whether to restart it.
#
#   0      user stopped it normally      → exit the loop
#   75     program requested a restart   → rerun immediately ("One-click update" or "Rollback" pressed in the UI)
#   other  abnormal exit                 → rerun after backoff (1→2→4… up to 60s)
#
# Downloading, SHA256 checksum verification and version swapping are deliberately not done here. That logic would need two
# copies, in sh and bat, and it is exactly the part that must never be wrong. Once swapped to a binary that does not run, this
# script would faithfully restart it over and over, and the user could only recover by logging into the machine by hand. So all
# verification and swapping live in Go (the selfupdate package), handled by artex itself at startup, and the script stays simple.
set -u

cd "$(dirname "$0")" || exit 1

BIN=./artex
[ -x "$BIN" ] || { echo "[artex] executable not found: $BIN" >&2; exit 1; }

RESTART_CODE=75
MAX_DELAY=60

child=0
stopping=0

# Forward stop signals to the artex process.
#
# This is essential in Docker: docker stop sends SIGTERM only to PID 1 (this script), not to
# child processes. Without forwarding, artex never receives the signal and cannot shut down gracefully;
# it is SIGKILLed after 10 seconds and any running work is cut off midway.
forward() {
	stopping=1
	if [ "$child" -ne 0 ]; then
		kill -TERM "$child" 2>/dev/null || true
	fi
}
trap forward INT TERM

delay=1
while :; do
	"$BIN" "$@" &
	child=$!

	# A signal interrupts wait and makes it return a value greater than 128. The child is still shutting down gracefully at that point,
	# so wait once more to get the real exit code.
	wait "$child"
	code=$?
	if [ "$code" -gt 128 ]; then
		wait "$child"
		code=$?
	fi
	child=0

	if [ "$stopping" -eq 1 ]; then
		echo "[artex] stopped"
		exit 0
	fi

	case "$code" in
		0)
			echo "[artex] exited normally"
			exit 0
			;;
		"$RESTART_CODE")
			# Update/rollback is ready. On rerun, artex completes the version swap at startup (see selfupdate.Bootstrap).
			echo "[artex] restart requested (applying new version)…"
			delay=1
			;;
		*)
			echo "[artex] abnormal exit (code=$code), restarting in ${delay}s" >&2
			sleep "$delay"
			delay=$((delay * 2))
			[ "$delay" -gt "$MAX_DELAY" ] && delay=$MAX_DELAY
			;;
	esac
done
