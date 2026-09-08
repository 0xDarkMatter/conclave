#!/usr/bin/env python3
"""Export the Diagram Design sources in docs/diagrams/src/*.html to standalone SVGs.

Usage:   python docs/diagrams/export.py            # writes docs/diagrams/<slug>.svg and <slug>-dark.svg
Output:  one light and one dark SVG per source; the README embeds both via <picture>.
Exit:    0 ok, 1 a source had no <svg> or the light/dark id rewrite failed.

Why this exists instead of the toolkit's export command: the README wants the diagram
ALONE (no eyebrow/title header, which the sources already omit) in BOTH themes, and
GitHub renders README SVGs inside <img>, where external fonts never load. So:
  - the SVG is extracted verbatim (the a11y <title>/<desc> contract is preserved),
  - the Google Fonts @import is injected per the toolkit's export.md (renders right when
    the file is opened directly; harmless inside GitHub's sandbox),
  - the dark variant is derived by the style-guide inversion rule (paper<->ink, muted,
    soft, accent, accent-tint, backend white -> paper-2) with the a11y ids suffixed
    "-dark" so both variants can sit on one page without duplicate ids.
Keep the token map in step with references/style-guide.md if the skin ever changes.
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
SRC = HERE / "src"

FONTS = ("<style>@import url('https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600"
         "&amp;family=Geist+Mono:wght@400;500;600&amp;display=swap');</style>")

# Light -> dark, applied via placeholders so the paper/ink swap cannot cascade.
DARK = [
    ("#f5f5f5", "#2d3142"),                       # paper -> dark paper
    ("#2d3142", "#f5f5f5"),                       # ink -> dark ink
    ("#ffffff", "#393e53"),                       # backend white -> paper-2
    ("#4f5d75", "#bfc0c0"),                       # muted
    ("#7a8399", "#8e98ac"),                       # soft
    ("#eb6c36", "#f08a59"),                       # accent
    ("rgba(235,108,54,0.08)", "rgba(240,138,89,0.10)"),  # accent-tint
    ("rgba(235,108,54,", "rgba(240,138,89,"),     # other accent alphas
    ("rgba(45,49,66,", "rgba(245,245,245,"),      # ink alphas
    ("rgba(79,93,117,", "rgba(191,192,192,"),     # muted alphas
    ("rgba(122,131,153,", "rgba(142,152,172,"),   # soft alphas
]


def extract_svg(html: str) -> str:
    m = re.search(r"<svg\b.*?</svg>", html, re.S)
    if not m:
        raise SystemExit("no <svg> in source")
    svg = m.group(0)
    if 'xmlns="http://www.w3.org/2000/svg"' not in svg:
        svg = svg.replace("<svg", '<svg xmlns="http://www.w3.org/2000/svg"', 1)
    if "viewBox" not in svg:
        raise SystemExit("source svg has no viewBox")
    svg = svg.replace("<defs>", "<defs>\n    " + FONTS, 1)
    return '<?xml version="1.0" encoding="UTF-8"?>\n' + svg + "\n"


def darken(svg: str, slug: str) -> str:
    out = svg
    for i, (light, _) in enumerate(DARK):
        out = out.replace(light, f"\x00{i}\x00")
    for i, (_, dark) in enumerate(DARK):
        out = out.replace(f"\x00{i}\x00", dark)
    n = 0
    for attr in ("title", "desc"):
        out, k = re.subn(rf'id="{slug}-{attr}"', f'id="{slug}-dark-{attr}"', out)
        n += k
    out = out.replace(f'aria-labelledby="{slug}-title {slug}-desc"',
                      f'aria-labelledby="{slug}-dark-title {slug}-dark-desc"')
    if n != 2:
        raise SystemExit(f"{slug}: a11y ids not rewritten for dark variant")
    return out


def main() -> int:
    sources = sorted(SRC.glob("*.html"))
    if not sources:
        print("no sources under docs/diagrams/src", file=sys.stderr)
        return 1
    for src in sources:
        slug = src.stem
        light = extract_svg(src.read_text(encoding="utf-8"))
        (HERE / f"{slug}.svg").write_text(light, encoding="utf-8", newline="\n")
        (HERE / f"{slug}-dark.svg").write_text(darken(light, slug), encoding="utf-8", newline="\n")
        print(f"{slug}: light + dark")
    return 0


if __name__ == "__main__":
    sys.exit(main())
