#!/usr/bin/env python3
"""Check that external links (http, https) referenced by tracked Markdown docs are still alive.

The sibling script `check-doc-links.py` only looks at internal links and anchors in
the repo and skips external URLs by design (they depend on the network and are
flaky, so the merge gate does not handle them). This script is that "separate
check". External links that the top of the README, the defense guide and the
detections READMEs point visitors and defenders to (incident reporting channels
boho.or.kr, privacy.go.kr, pipc.go.kr, fsec.or.kr, CISA KEV, OWASP, SigmaHQ,
Suricata, MITRE, MISP, the original demo artex-demo.vercel.app, GitHub badges,
etc.) stay silently broken when they change, move or shut down; this catches them
periodically or on demand.

Rules for choosing URLs to check
- Scan every tracked `.md`, but skip URLs inside fenced code blocks (``` or ~~~)
  and inline code spans (`` `...` ``). URLs there are command examples, config
  values or quoted external assets (e.g. `id.redhaze.top` in the side-question
  validation doc), not "reference links for readers to follow".
- Collect http/https addresses from Markdown links and images (`[text](url)`,
  `![alt](url)`), autolinks (`<url>`) and bare URLs left in the prose.
- Exclude reserved and placeholder hosts: localhost, private/loopback IPs (127.,
  10., 192.168., 169.254., 172.16-31., 0.0.0.0, ::1), RFC 2606/6761 reserved names
  (example.com/org/net/edu, `*.example.*`, `.test`, `.invalid`, `.local`, `.tld`),
  and dotless names that are not FQDNs (e.g. `target`).

How liveness is checked (the lessons of MAINTAINING.md 8.2 turned into code)
Korean public and security agency sites reject HEAD requests and the default
User-Agent, or redirect several times, so a naive check mistakes healthy links for
broken ones. This script therefore checks **with a browser User-Agent, using GET,
following redirects**. Results fall into three states:
- OK: final status 2xx/3xx. The link is valid.
- RESTRICTED: 401, 403, 405, 429. The host is alive; the check was just blocked by
  the server's access policy (bot blocking, method rejection, rate limiting), not a
  broken link. Reported but not counted as a failure.
- DOWN: 404, 410, 5xx (even after retries), DNS/connection/timeout/SSL errors (even
  after retries). Most likely genuinely broken.

Network errors, 5xx and 429 are retried after a short delay to tell transient
blips from real outages. Uses only the standard library. This script only sends
GET requests to reference URLs that the repo's **public docs already point to**, to
confirm they are alive; it does not scan or probe any target.

Known exceptions (allowlist)
If a URL listed in `scripts/external-links-allowlist.txt` comes back DOWN, it is
classified separately as "ALLOWED" and does not affect the strict exit code. This
transparently records dead links inherited by upstream-preserved files we do not
own and cannot fix (e.g. the upstream CHANGELOG.zh.md), so the periodic strict check
does not stay red forever because of them and turns red **only when a new link
breaks**.

Running
- Default (report only, exit code 0): `python3 -I scripts/check-external-links.py`
- Periodic/release check rather than a merge gate (red when a new link breaks):
  `--strict` (exit code 1 if there is any DOWN not on the allowlist). RESTRICTED
  and ALLOWED do not count as failures even in strict mode.
- Preview the extracted set without network access: `--list` (prints the URLs to
  check and where they come from, without making any requests).
"""
import argparse
import http.client
import os
import re
import socket
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
from collections import defaultdict
from urllib.parse import urlsplit

# [text](url) body links (not images) and ![alt](url) images. The path runs up to a space or closing paren.
MD_LINK = re.compile(r"(?<!\!)\[[^\]]*\]\(([^)\s]+)")
MD_IMAGE = re.compile(r"!\[[^\]]*\]\(([^)\s]+)")
# <https://...> autolinks.
AUTOLINK = re.compile(r"<(https?://[^>\s]+)>")
# Bare URLs in prose. Trailing punctuation is stripped afterwards.
BARE_URL = re.compile(r"https?://[^\s)>\]\"'`]+")
# Inline code spans `...` (single backticks). Blanked out before extraction.
INLINE_CODE = re.compile(r"`[^`]*`")
# Punctuation that commonly trails URLs in prose.
TRAILING_PUNCT = ".,;:!?\"'»)]}>"

# IPv4 private/loopback/link-local ranges (172.16-31. has its own pattern).
PRIVATE_IPV4 = re.compile(r"^(127\.|10\.|192\.168\.|169\.254\.|0\.0\.0\.0$)")
PRIVATE_IPV4_172 = re.compile(r"^172\.(1[6-9]|2\d|3[01])\.")

BROWSER_UA = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
    "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36"
)

# Status codes where the host is alive but the check was blocked by server policy (not broken).
RESTRICTED_CODES = {401, 403, 405, 429}

ALLOWLIST_PATH = os.path.join("scripts", "external-links-allowlist.txt")


def load_allowlist(root: str) -> set:
    """Read the set of known DOWN URLs we cannot fix (an empty set if the file is missing).

    One URL per line. Text after `#` is a comment; blank and comment-only lines are ignored.
    """
    path = os.path.join(root, ALLOWLIST_PATH)
    allow = set()
    if not os.path.exists(path):
        return allow
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            line = line.split("#", 1)[0].strip()
            if line:
                allow.add(line)
    return allow


def strip_code(line: str) -> str:
    """Blank out inline code spans in a line outside fences (URLs inside them are not references)."""
    return INLINE_CODE.sub(" ", line)


def is_checkable(url: str) -> bool:
    """Filter out reserved/placeholder hosts, keeping only external URLs worth checking."""
    parts = urlsplit(url)
    if parts.scheme not in ("http", "https"):
        return False
    host = parts.hostname
    if not host:
        return False
    host = host.lower()
    if host == "localhost" or host.endswith(".localhost"):
        return False
    if host == "::1":
        return False
    if PRIVATE_IPV4.match(host) or PRIVATE_IPV4_172.match(host):
        return False
    if host in ("example.com", "example.org", "example.net", "example.edu"):
        return False
    if host.endswith((".example.com", ".example.org", ".example.net", ".example")):
        return False
    if host.endswith((".test", ".invalid", ".local", ".localdomain", ".tld")):
        return False
    if "." not in host:  # bare name that is not an FQDN (e.g. target)
        return False
    return True


def extract(root: str, md_files: list) -> dict:
    """Build a dict of external URL to check -> [(file, line number), ...] from tracked .md files."""
    found = defaultdict(list)
    for md in md_files:
        with open(os.path.join(root, md), encoding="utf-8", errors="ignore") as fh:
            lines = fh.readlines()
        in_fence = False
        for lineno, raw in enumerate(lines, 1):
            stripped = raw.lstrip()
            if stripped.startswith("```") or stripped.startswith("~~~"):
                in_fence = not in_fence
                continue
            if in_fence:
                continue
            line = strip_code(raw)
            candidates = []
            for pattern in (MD_LINK, MD_IMAGE, AUTOLINK):
                candidates.extend(m.group(1) for m in pattern.finditer(line))
            for m in BARE_URL.finditer(line):
                candidates.append(m.group(0).rstrip(TRAILING_PUNCT))
            for url in candidates:
                url = url.strip().rstrip(TRAILING_PUNCT)
                if is_checkable(url):
                    where = (md, lineno)
                    if where not in found[url]:
                        found[url].append(where)
    return found


def probe(url: str, timeout: float, retries: int, delay: float):
    """Send a GET to the URL with a browser UA and return (verdict, detail).

    The verdict is "OK", "RESTRICTED" or "DOWN". Network errors, 5xx and 429 are retried.
    """
    req = urllib.request.Request(
        url,
        method="GET",
        headers={
            "User-Agent": BROWSER_UA,
            "Accept": "*/*",
            "Accept-Language": "ko,en;q=0.8",
        },
    )
    last = ""
    for attempt in range(retries + 1):
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                code = resp.getcode()
                if code in RESTRICTED_CODES:
                    return "RESTRICTED", f"HTTP {code}"
                return "OK", f"HTTP {code}"
        except urllib.error.HTTPError as exc:
            code = exc.code
            if code in RESTRICTED_CODES:
                return "RESTRICTED", f"HTTP {code}"
            if code >= 500 or code == 408:
                last = f"HTTP {code}"  # may be transient, so retry
            else:
                return "DOWN", f"HTTP {code}"  # 404, 410 etc. are definitive, no retry needed
        except (
            urllib.error.URLError,
            http.client.HTTPException,
            ssl.SSLError,
            socket.timeout,
            ConnectionError,
            TimeoutError,
            OSError,
        ) as exc:
            reason = getattr(exc, "reason", exc)
            last = f"{type(exc).__name__}: {reason}"
        if attempt < retries:
            time.sleep(delay * (attempt + 1))
    return "DOWN", last


def main() -> int:
    ap = argparse.ArgumentParser(description="Check liveness of external links in tracked Markdown")
    ap.add_argument("--strict", action="store_true",
                    help="exit code 1 if anything is DOWN (for periodic/release checks)")
    ap.add_argument("--list", action="store_true",
                    help="print only the URLs to check and their sources, without network calls")
    ap.add_argument("--timeout", type=float, default=15.0, help="request timeout (seconds)")
    ap.add_argument("--retries", type=int, default=2, help="number of retries for network errors, 5xx and 429")
    ap.add_argument("--delay", type=float, default=0.5, help="base delay between requests and retries (seconds)")
    args = ap.parse_args()

    root = subprocess.check_output(
        ["git", "rev-parse", "--show-toplevel"], text=True
    ).strip()
    tracked = subprocess.check_output(["git", "ls-files"], cwd=root, text=True).splitlines()
    # Skip tracked files deleted or renamed in the working tree but not yet committed.
    md_files = [
        f for f in tracked
        if f.endswith(".md") and os.path.exists(os.path.join(root, f))
    ]

    allow = load_allowlist(root)
    found = extract(root, md_files)
    urls = sorted(found)
    print(f"Collected {len(urls)} external URLs to check from {len(md_files)} tracked Markdown files")

    if args.list:
        for url in urls:
            where = ", ".join(f"{f}:{ln}" for f, ln in found[url])
            tag = " [allowlist]" if url in allow else ""
            print(f"  {url}{tag}  ({where})")
        return 0

    results = {"OK": [], "RESTRICTED": [], "ALLOWED": [], "DOWN": []}
    for i, url in enumerate(urls):
        if i:
            time.sleep(args.delay)  # small delay between requests (avoids sandbox throttling)
        verdict, detail = probe(url, args.timeout, args.retries, args.delay)
        if verdict == "DOWN" and url in allow:
            verdict = "ALLOWED"  # known DOWN inherited from upstream that we cannot fix
        results[verdict].append((url, detail))
        print(f"  [{verdict:10}] {url} — {detail}")

    print(
        f"\nSummary: OK {len(results['OK'])} · RESTRICTED {len(results['RESTRICTED'])} · "
        f"ALLOWED {len(results['ALLOWED'])} · DOWN {len(results['DOWN'])}"
    )
    if results["RESTRICTED"]:
        print("RESTRICTED (host alive, check was blocked; not a broken link):")
        for url, detail in results["RESTRICTED"]:
            print(f"  {url} — {detail}")
    if results["ALLOWED"]:
        print("ALLOWED (known DOWN on the allowlist; we cannot fix it, so excluded from strict):")
        for url, detail in results["ALLOWED"]:
            where = ", ".join(f"{f}:{ln}" for f, ln in found[url])
            print(f"  {url} — {detail}  (referenced at: {where})")
    if results["DOWN"]:
        print("DOWN (likely broken; needs checking):")
        for url, detail in results["DOWN"]:
            where = ", ".join(f"{f}:{ln}" for f, ln in found[url])
            print(f"  {url} — {detail}  (referenced at: {where})")

    if args.strict and results["DOWN"]:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
