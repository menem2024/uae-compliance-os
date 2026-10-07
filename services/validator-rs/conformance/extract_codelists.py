#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.12"
# dependencies = []
# ///
"""Extract the code lists that the official PINT AE 1.0.4 asserts inline in their XPath.

    extract_codelists.py [--out codelists/codelists.tsv] [--min 5]

Reads `upstream/rules-base.tsv` and `upstream/rules-ae.tsv` (columns: id, flag, context, test,
message) and writes `list_id <TAB> code`, one row per code, for every inline list with at least
`--min` (default 5) codes. Two forms are recognised:

  * `contains(' A B C ', concat(' ', normalize-space(x), ' '))`   (17 ibr-cl-* rules and ibr-001/
    005/006/011/013/139-ae)
  * `matches(x, "^(AF|AX|...)$")`                                  (ibr-010-ae, ibr-012-ae)

`list_id` is the rule id, or `<rule id>#<n>` (1-based) when a rule has several qualifying lists.
Short XPath sequences such as `= ("AUH", "DXB", ...)` are not extracted; they stay literal in the
Rust rule. Codes keep their source order; the file is sorted by rule id and fully deterministic.
Rules whose lists are all shorter than `--min` (for example ibr-cl-01: `380 480` / `81 381`) are
not written.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

RULESET = Path(__file__).resolve().parent.parent / "rulesets" / "pint-ae-1.0.4"
CONTAINS = re.compile(r"contains\(\s*'((?: [^' ]+)+ )'")
MATCHES = re.compile(r'matches\([^"]*"\^\(([^")]+)\)\$"')


def lists_in(test: str) -> list[list[str]]:
    found: list[tuple[int, list[str]]] = []
    for m in CONTAINS.finditer(test):
        found.append((m.start(), m.group(1).split()))
    for m in MATCHES.finditer(test):
        found.append((m.start(), m.group(1).split("|")))
    found.sort(key=lambda t: t[0])  # source order within the test
    return [codes for _, codes in found]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", type=Path, default=RULESET / "codelists" / "codelists.tsv")
    ap.add_argument("--min", type=int, default=5, dest="min_codes")
    ap.add_argument("--rules-dir", type=Path, default=RULESET / "upstream")
    args = ap.parse_args()

    rules: dict[str, str] = {}
    for name in ("rules-base.tsv", "rules-ae.tsv"):
        for line in (args.rules_dir / name).read_text(encoding="utf-8").splitlines():
            cols = line.split("\t")
            if len(cols) != 5:
                raise SystemExit(f"{name}: expected 5 columns, got {len(cols)}: {line[:60]!r}")
            rules[cols[0]] = cols[3]

    out_rows: list[str] = ["list_id\tcode"]
    for rid in sorted(rules):
        qualifying = []
        for codes in lists_in(rules[rid]):
            uniq = list(dict.fromkeys(codes))
            if len(uniq) >= args.min_codes:
                qualifying.append(uniq)
        for n, codes in enumerate(qualifying, 1):
            list_id = rid if len(qualifying) == 1 else f"{rid}#{n}"
            out_rows.extend(f"{list_id}\t{c}" for c in codes)

    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text("\n".join(out_rows) + "\n", encoding="utf-8")
    n_lists = len({r.split("\t")[0] for r in out_rows[1:]})
    print(f"{n_lists} lists, {len(out_rows) - 1} codes -> {args.out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
