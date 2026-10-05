#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.12"
# dependencies = ["saxonche==13.0.0"]
# ///
"""Run the official PINT AE 1.0.4 compiled schematron (SaxonC-HE) over every XML under a directory.

    run_schematron.py <dir> --out saxon.json [--upstream <upstream dir>]

Both compiled stylesheets of the document's kind (chosen by root element: Invoice -> trn-invoice,
CreditNote -> trn-creditnote) are run on each `<dir>/**/*.xml`. Output JSON, keyed by the path
relative to <dir> (posix):

    { "<rel path>": { "kind": "invoice" | "credit_note", "failed": { "<assert id>": <count>, ... } } }

`failed` counts every `svrl:failed-assert` with multiplicity, summed over both stylesheets.

A stylesheet that raises a dynamic error on a document (the official aligned stylesheet raises
XPTY0004 in `u:slack()` when a VAT breakdown entry has no TaxableAmount, IBT-116) records
`{"kind": ..., "error": "<message>"}` for that document instead of `failed`, and the run continues
with the next document; `compare.py` decides what such a document means. SaxonC also prints the
error's stack to stderr.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import xml.etree.ElementTree as ET
from collections import Counter
from pathlib import Path

SVRL = "{http://purl.oclc.org/dsdl/svrl}"
SHEETS = ("PINT-UBL-validation-preprocessed.xslt", "PINT-jurisdiction-aligned-rules.xslt")
KINDS = {"Invoice": ("invoice", "trn-invoice"), "CreditNote": ("credit_note", "trn-creditnote")}
DEFAULT_UPSTREAM = Path(__file__).resolve().parent.parent / "rulesets" / "pint-ae-1.0.4" / "upstream"


def root_kind(path: Path) -> tuple[str, str]:
    for _, el in ET.iterparse(path, events=("start",)):
        local = el.tag.rsplit("}", 1)[-1]
        if local not in KINDS:
            raise SystemExit(f"{path}: unsupported root element {local!r}")
        return KINDS[local]
    raise SystemExit(f"{path}: empty document")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("dir", type=Path)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--upstream", type=Path, default=DEFAULT_UPSTREAM)
    args = ap.parse_args()

    files = sorted(args.dir.rglob("*.xml"))
    if not files:
        print(f"no XML files under {args.dir}", file=sys.stderr)
        return 2

    from saxonche import PySaxonApiError, PySaxonProcessor

    result: dict[str, dict] = {}
    errors = 0
    t0 = time.time()
    with PySaxonProcessor(license=False) as proc:
        xslt = proc.new_xslt30_processor()
        compiled: dict[str, list] = {}
        for f in files:
            kind, folder = root_kind(f)
            if folder not in compiled:
                compiled[folder] = [
                    xslt.compile_stylesheet(
                        stylesheet_file=str(args.upstream / "schematron" / folder / s)
                    )
                    for s in SHEETS
                ]
            rel = f.relative_to(args.dir).as_posix()
            failed: Counter[str] = Counter()
            try:
                for exe in compiled[folder]:
                    svrl = exe.transform_to_string(source_file=str(f))
                    for el in ET.fromstring(svrl).iter(SVRL + "failed-assert"):
                        failed[el.get("id") or el.get("location", "?")] += 1
            except PySaxonApiError as e:
                errors += 1
                result[rel] = {"kind": kind, "error": str(e).strip() or type(e).__name__}
                continue
            result[rel] = {"kind": kind, "failed": dict(sorted(failed.items()))}
    args.out.write_text(json.dumps(result, indent=1, sort_keys=True) + "\n")
    ok = [d for d in result.values() if "failed" in d]
    bad = sum(1 for d in ok if d["failed"])
    total = sum(sum(d["failed"].values()) for d in ok)
    print(
        f"{len(result)} documents, {bad} with failed asserts, {total} failed asserts, "
        f"{errors} stylesheet error{'' if errors == 1 else 's'} "
        f"({(time.time() - t0) * 1000:.0f} ms)"
    )
    for name, d in sorted(result.items()):
        if "error" in d:
            print(f"ERROR {name}: {d['error']}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
