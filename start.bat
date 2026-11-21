@echo off
rem Switch the console to UTF-8 so any non-ASCII output is not garbled on legacy code-page (e.g. GBK) terminals.
chcp 65001 >nul 2>&1
rem ARTEX daemon start script (Windows)
rem
rem Usage:
rem   start.bat                  run in the foreground (Ctrl-C to stop)
rem   start.bat -addr :9000      extra arguments are passed through to artex
rem
rem This script does one thing: it runs artex.exe and, when the process exits, decides from the exit code whether to restart it.
rem
rem   0      user stopped it normally    -> exit the loop
rem   75     program requested a restart -> rerun immediately ("One-click update" or "Rollback" pressed in the UI)
rem   other  abnormal exit               -> rerun after backoff (1->2->4... up to 60s)
rem
rem Downloading, SHA256 checksum verification and version swapping are not done here; artex handles all of it itself at startup
rem (the selfupdate package). The script stays simple. See the header comment of start.sh for details.

setlocal enabledelayedexpansion
cd /d "%~dp0"

set "BIN=artex.exe"
if not exist "%BIN%" (
	echo [artex] executable not found: %BIN% 1>&2
	exit /b 1
)

set "RESTART_CODE=75"
set "MAX_DELAY=60"
set /a delay=1

:loop
"%BIN%" %*
set "code=!ERRORLEVEL!"

if "!code!"=="0" (
	echo [artex] exited normally
	exit /b 0
)

if "!code!"=="%RESTART_CODE%" (
	rem Update/rollback is ready. On rerun, artex completes the version swap at startup.
	echo [artex] restart requested: applying new version...
	set /a delay=1
	goto loop
)

echo [artex] abnormal exit ^(code=!code!^), restarting in !delay!s 1>&2
rem timeout fails on a redirected console, so ping is used instead (an N-second delay needs N+1 pings).
set /a pings=!delay!+1
ping -n !pings! 127.0.0.1 >nul 2>&1
set /a delay=!delay!*2
if !delay! gtr %MAX_DELAY% set /a delay=%MAX_DELAY%
goto loop
