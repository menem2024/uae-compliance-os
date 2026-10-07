#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.12"
# dependencies = []
# ///
"""Compare Rust validation results with the official schematron and the XSD (E2, CI 13).

    compare.py --rust rust.json --saxon saxon.json --xsd xsd.json --allow allowlist.tsv

Input shapes, all keyed by the document path relative to the corpus directory:

    rust.json  { doc: { "rules": {official rule id: count}, "errors": int, "exported": bool } }
               `errors` counts every error-severity issue, including platform `AE-*` rules.
    saxon.json { doc: { "kind": "invoice"|"credit_note", "failed": {assert id: count} } }
               or { doc: { "kind": ..., "error": str } } when a stylesheet raised a dynamic
               error on the document (written by run_schematron.py)
    xsd.json   { doc: { "valid": bool, "error": str } }   (written by xsd_check.py)
    allowlist.tsv  rule_id <TAB> document_kind <TAB> reason  (first line is the header)

Checks, one output line per difference; exit 1 when there is any:
  1. the three files describe the same set of documents;
  2. E2 schema:     every document with zero Rust errors is XSD-valid;
  3. E2 schematron: every document with zero Rust errors has no failed official assert;
  4. equivalence:   the multiset of official rule ids from Rust equals Saxon's.
Platform rules (`AE-*`) are Rust-only and never compared. An allow-list row (rule id, document
kind) removes that rule from checks 3 and 4 for documents of that kind.

Documented skip: a document on which an official stylesheet raised a dynamic error has no Saxon
result, so checks 2-4 cannot run on it. Such a document is skipped (one `SKIP` line each, and
the count in the summary) only when the error is listed in KNOWN_STYLESHEET_ERRORS, Rust
reports at least one error for it (it is never exported) and Rust reports one of the rules that
name the missing operand. Any other stylesheet error is a difference.
"""

from __future__ import annotations

import argparse
import csv
import json
import sys
from collections import Counter
from pathlib import Path


# Dynamic errors of the official PINT AE 1.0.4 stylesheets: message substring -> (reason, the
# official rules of which Rust must report at least one on that document).
KNOWN_STYLESHEET_ERRORS: dict[str, tuple[str, frozenset[str]]] = {
    "An empty sequence is not allowed as the first argument of u:slack()": (
        "XPTY0004 in the official aligned stylesheet: u:slack() declares its first parameter "
        "xs:decimal, and a VAT breakdown entry without TaxableAmount (IBT-116, "
        "aligned-ibrp-s-08, ibr-102-ae) or TaxAmount (IBT-117, aligned-ibrp-s-09) passes the "
        "empty sequence (rules/vat.rs, 'Type errors in the official stylesheet')",
        frozenset({"aligned-ibrp-045", "aligned-ibrp-046"}),
    ),
}


def known_error(message: str) -> tuple[str, frozenset[str]] | None:
    for needle, known in KNOWN_STYLESHEET_ERRORS.items():
        if needle in message:
            return known
    return None


def load_allow(path: Path) -> set[tuple[str, str]]:
    allow: set[tuple[str, str]] = set()
    with path.open(encoding="utf-8", newline="") as f:
        for row in csv.DictReader(
            (ln for ln in f if ln.strip() and not ln.startswith("#")), delimiter="\t"
        ):
            allow.add((row["rule_id"], row["document_kind"]))
    return allow


def official(counts: dict[str, int]) -> Counter[str]:
    return Counter({k: v for k, v in counts.items() if not k.startswith("AE-") and v > 0})


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--rust", type=Path, required=True)
    ap.add_argument("--saxon", type=Path, required=True)
    ap.add_argument("--xsd", type=Path, required=True)
    ap.add_argument("--allow", type=Path, required=True)
    args = ap.parse_args()

    rust = json.loads(args.rust.read_text())
    saxon = json.loads(args.saxon.read_text())
    xsd = json.loads(args.xsd.read_text())
    allow = load_allow(args.allow)

    diffs: list[str] = []
    skipped: list[str] = []
    for name, have in (("rust", rust), ("saxon", saxon), ("xsd", xsd)):
        for doc in sorted(set(rust) | set(saxon) | set(xsd)):
            if doc not in have:
                diffs.append(f"{doc}: missing from {name}.json")

    for doc in sorted(set(rust) & set(saxon) & set(xsd)):
        r, s, x = rust[doc], saxon[doc], xsd[doc]
        kind = s["kind"]
        if "error" in s:
            known = known_error(s["error"])
            if known is None:
                diffs.append(f"{doc}: official stylesheet error: {s['error']}")
            elif r["errors"] == 0:
                diffs.append(
                    f"{doc}: zero Rust errors but the official stylesheet raised: {s['error']}"
                )
            elif not known[1] & {k for k, v in r["rules"].items() if v > 0}:
                diffs.append(
                    f"{doc}: official stylesheet raised ({s['error']}) but Rust reports none of "
                    f"{sorted(known[1])}"
                )
            else:
                skipped.append(doc)
                print(f"SKIP {doc}: {known[0]}")
            continue
        allowed = {rid for rid, k in allow if k == kind}
        rr = Counter({k: v for k, v in official(r["rules"]).items() if k not in allowed})
        ss = Counter({k: v for k, v in official(s["failed"]).items() if k not in allowed})

        if r["errors"] == 0:
            if not x["valid"]:
                diffs.append(f"{doc}: zero Rust errors but not XSD-valid: {x['error']}")
            if ss:
                diffs.append(
                    f"{doc}: zero Rust errors but official schematron fails: {dict(sorted(ss.items()))}"
                )
        if rr != ss:
            only_r = dict(sorted((rr - ss).items()))
            only_s = dict(sorted((ss - rr).items()))
            diffs.append(f"{doc}: rule ids differ; rust extra {only_r}, saxon extra {only_s}")

    for d in diffs:
        print(d)
    checked = len(set(rust) & set(saxon) & set(xsd)) - len(skipped)
    print(
        f"compared {checked} documents, {len(skipped)} skipped (official stylesheet error), "
        f"{len(diffs)} differences"
    )
    return 1 if diffs else 0


if __name__ == "__main__":
    sys.exit(main())
