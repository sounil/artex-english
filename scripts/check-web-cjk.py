#!/usr/bin/env python3
"""Check whether Chinese (Han characters) leaked into the HTML of the statically exported web UI (`web/out`).

A core achievement and differentiator of this fork is that "no Chinese appears on
user-facing screens" (the zero-Han property of the `out` HTML established by
backlog B4). If that property is left to manual checks only, a regression where a
contributor hard-codes a Chinese string or a translation goes missing stays silently
broken until someone happens to notice it. This script blocks that regression at
the merge gate.

How it checks
-------------
It scans the **raw content** of every `.html` file under `web/out` (not only the
rendered markup but also inline scripts and React Server Components flight data)
and exits with code 1 if there is even one Han character. The raw content is
checked because user-visible text travels in the flight data (JSON-like strings)
shipped with the HTML; stripping tags alone would miss leaks along that path.

Out of scope (honest limits)
----------------------------
This gate only sees the static export output, so it cannot check **what the browser
renders at runtime in MOCK mode (`NEXT_PUBLIC_MOCK=1`, the mode the public demo
uses)**. `web/src/lib/mock/data.ts`, `handler.ts` and each page's hard-coded samples
are fetched by `useEffect` via `mockHandle()` after hydration and rendered from React
state, so they live only in the runtime JS bundle, not in the `out` HTML. Chinese
arriving by this path passes the gate (sample logs on the public demo's
`/system/logs` once silently kept Chinese this way; backlog G114 fixed them).

That surface is guarded by source review. Any Chinese remaining in rendered files
(`.tsx` and client data under `web/src`) must be one of: a code comment, an i18n
fallback guarded by `t.has(...)` and therefore never actually used, a legacy alias
that parses data written before English became the source language (Chinese chat
mention labels, the `[模型]` decision-source prefix), a functional matcher, or a
literal in a branch shown only when the locale is `zh`. Anything that fits none
of these and is "just Chinese on screen" must be translated (backlog G115 audited
this classification exhaustively).

What is allowed and detected
----------------------------
- **Hangul (`가-힣` etc.) is allowed.** The Han and Hangul blocks do not overlap in
  Unicode, so detecting only the Han blocks lets Hangul through automatically.
- **Only Han (CJK ideographs) is detected**: unified ideographs plus extension A
  (`㐀`-`鿿`), compatibility ideographs (`豈`-`﫿`), and extension B and above in the
  supplementary planes (`𠀀`-). Kana and full-width punctuation are not "Han" and are
  not targeted by this gate (the range is narrowed to Han to avoid false positives
  from verbatim code blocks or quoted evidence).

Intentional Han kept for upstream fidelity
------------------------------------------
The `out` HTML currently contains no Han characters at all, so the allowlist is
empty. If the original (Chinese) text ever has to stay on screen on purpose (e.g.
the `中文` label in the language switcher), add those characters to `ALLOWED_HAN`
below with a comment explaining why. That keeps the gate strict while letting only
the intended exceptions through.

Uses only the standard library and never touches the network.
Run: `python3 -I scripts/check-web-cjk.py [out directory]`
(Defaults to `web/out` at the repo root. CI calls it from the `web` working
directory as `python3 -I ../scripts/check-web-cjk.py`.)
"""
import os
import re
import subprocess
import sys

# CJK Han blocks only. Hangul (AC00-D7A3), kana and full-width punctuation are not included.
# Ranges: extension A + unified ideographs (U+3400-U+9FFF = 㐀-鿿), compatibility
#         ideographs (U+F900-U+FAFF = 豈-﫿), supplementary-plane extension B and above
#         (U+20000-U+2FFFF).
HAN = re.compile(r"[㐀-鿿豈-﫿\U00020000-\U0002ffff]")

# Han characters deliberately left on screen (e.g. the language switcher's `中文`).
# Empty for now: with it empty, any Han character fails the gate. When adding an
# exception, record the characters together with the reason.
# e.g. ALLOWED_HAN = {"中", "文"}  # language switcher '中文' label (upstream comparison)
ALLOWED_HAN: set = set()


def find_out_dir() -> str:
    """Decide which `out` directory to check.

    Uses the path given as an argument, or else `web/out` at the git repo root.
    """
    if len(sys.argv) > 1:
        return os.path.abspath(sys.argv[1])
    root = subprocess.check_output(
        ["git", "rev-parse", "--show-toplevel"], text=True
    ).strip()
    return os.path.join(root, "web", "out")


def main() -> int:
    out_dir = find_out_dir()
    if not os.path.isdir(out_dir):
        print(
            f"Error: directory to check does not exist: {out_dir}\n"
            "Build the static export with `npm run build:static` first, then run this again."
        )
        return 2

    checked = 0
    offenders = []  # (relative path, Han count, preview)
    for dirpath, _dirnames, filenames in os.walk(out_dir):
        for name in filenames:
            if not name.endswith(".html"):
                continue
            checked += 1
            path = os.path.join(dirpath, name)
            with open(path, encoding="utf-8", errors="ignore") as fh:
                text = fh.read()
            found = [ch for ch in HAN.findall(text) if ch not in ALLOWED_HAN]
            if found:
                rel = os.path.relpath(path, out_dir)
                kinds = "".join(sorted(set(found)))
                offenders.append((rel, len(found), kinds))

    print(f"Checked {checked} HTML files (target: {out_dir})")
    if offenders:
        print(f"Chinese (Han) leak in {len(offenders)} file(s): user-facing HTML contains Han characters:")
        for rel, count, kinds in offenders:
            preview = kinds if len(kinds) <= 30 else kinds[:30] + "…"
            print(f"  out/{rel}: {count} Han characters, distinct: {preview}")
        print(
            "\nTranslate the text, or if it is intentional Han that must be kept for upstream "
            "fidelity, add it to ALLOWED_HAN in scripts/check-web-cjk.py along with the reason."
        )
        return 1
    if checked == 0:
        print("Warning: no HTML files were checked; make sure the build ran correctly.")
        return 2
    print("0 Chinese (Han) leaks: user-facing HTML contains no Han characters.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
