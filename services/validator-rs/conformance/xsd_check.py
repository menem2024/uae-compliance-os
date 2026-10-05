#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.12"
# dependencies = ["lxml==6.1.3"]
# ///
"""Validate every XML under a directory against the OASIS UBL 2.1 XSD.

    xsd_check.py <dir> --out xsd.json [--upstream <upstream dir>]

`UBL-Invoice-2.1.xsd` for an Invoice root, `UBL-CreditNote-2.1.xsd` for a CreditNote root.
Output JSON, keyed by the path relative to <dir> (posix):

    { "<rel path>": { "kind": "invoice" | "credit_note", "valid": bool, "error": "<first error or ''>" } }

Always exits 0 when it could run; `compare.py` decides what a failure means.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

DEFAULT_UPSTREAM = Path(__file__).resolve().parent.parent / "rulesets" / "pint-ae-1.0.4" / "upstream"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("dir", type=Path)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--upstream", type=Path, default=DEFAULT_UPSTREAM)
    args = ap.parse_args()

    from lxml import etree

    files = sorted(args.dir.rglob("*.xml"))
    if not files:
        print(f"no XML files under {args.dir}", file=sys.stderr)
        return 2
    maindoc = args.upstream / "xsd" / "maindoc"
    schemas = {
        "invoice": etree.XMLSchema(etree.parse(str(maindoc / "UBL-Invoice-2.1.xsd"))),
        "credit_note": etree.XMLSchema(etree.parse(str(maindoc / "UBL-CreditNote-2.1.xsd"))),
    }
    result: dict[str, dict] = {}
    for f in files:
        try:
            tree = etree.parse(str(f))
        except etree.XMLSyntaxError as e:
            result[f.relative_to(args.dir).as_posix()] = {
                "kind": "unknown", "valid": False, "error": f"not well-formed: {e}",
            }
            continue
        kind = "credit_note" if etree.QName(tree.getroot()).localname == "CreditNote" else "invoice"
        schema = schemas[kind]
        ok = schema.validate(tree)
        err = "" if ok else str(schema.error_log.last_error)
        result[f.relative_to(args.dir).as_posix()] = {"kind": kind, "valid": ok, "error": err}
    args.out.write_text(json.dumps(result, indent=1, sort_keys=True) + "\n")
    valid = sum(1 for d in result.values() if d["valid"])
    print(f"{valid}/{len(result)} valid")
    for name, d in result.items():
        if not d["valid"]:
            print(f"INVALID {name}: {d['error']}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
