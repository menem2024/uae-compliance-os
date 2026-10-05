# /// script
# requires-python = ">=3.12"
# dependencies = ["pytest>=8", "saxonche==13.0.0", "lxml==6.1.3"]
# ///
"""Tests for the Task 3 conformance tooling and data files.

Run:  uv run services/validator-rs/conformance/tests/test_conformance_tools.py
(needs `scripts/fetch-upstream.sh` to have been run once).
"""

from __future__ import annotations

import csv
import json
import subprocess
import sys
from pathlib import Path

import pytest

HERE = Path(__file__).resolve().parent
CONF = HERE.parent
RULESET = CONF.parent / "rulesets" / "pint-ae-1.0.4"
UPSTREAM = RULESET / "upstream"
EXAMPLES = UPSTREAM / "examples"

FAMILIES = ["header", "parties", "lines", "totals", "vat", "codelists", "platform"]
COLUMNS = [
    "rule_id", "family", "status", "severity", "business_term",
    "path", "fix", "message_en", "message_ar", "note",
]


def run(script: str, *args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [sys.executable, str(CONF / script), *args],
        capture_output=True, text=True, check=False,
    )


def read_tsv(path: Path) -> list[dict[str, str]]:
    with path.open(encoding="utf-8", newline="") as f:
        return list(csv.DictReader(f, delimiter="\t", quoting=csv.QUOTE_NONE))


# ---------------------------------------------------------------- coverage skeletons


def coverage_rows() -> list[dict[str, str]]:
    rows: list[dict[str, str]] = []
    for fam in FAMILIES:
        path = RULESET / "coverage" / f"{fam}.tsv"
        header = path.read_text(encoding="utf-8").splitlines()[0].split("\t")
        assert header == COLUMNS, f"{fam}.tsv header"
        for r in read_tsv(path):
            assert r["family"] == fam
            rows.append(r)
    return rows


def test_coverage_has_exactly_302_unique_official_rows():
    rows = coverage_rows()
    ids = [r["rule_id"] for r in rows]
    assert len(ids) == 302
    assert len(set(ids)) == 302
    upstream_ids = set()
    for name in ("rules-base.tsv", "rules-ae.tsv"):
        for line in (UPSTREAM / name).read_text(encoding="utf-8").splitlines():
            upstream_ids.add(line.split("\t")[0])
    assert set(ids) == upstream_ids


def test_coverage_family_counts_match_the_draft():
    counts = {fam: len(read_tsv(RULESET / "coverage" / f"{fam}.tsv")) for fam in FAMILIES}
    assert counts == {
        "header": 69, "parties": 71, "lines": 46, "totals": 41,
        "vat": 57, "codelists": 18, "platform": 0,
    }


def test_coverage_rows_are_pending_with_arabic_text():
    for r in coverage_rows():
        assert r["status"] == "pending"
        assert r["severity"] == "error"
        assert r["message_en"] == ""  # official rows take the upstream text
        assert r["message_ar"].strip() != ""


def test_mutation_files_exist_and_are_empty():
    for fam in FAMILIES:
        assert (RULESET / "mutations" / f"{fam}.jsonl").read_text() == ""


def test_allowlist_has_only_the_two_preapproved_rows():
    rows = read_tsv(CONF / "allowlist.tsv")
    assert [(r["rule_id"], r["document_kind"]) for r in rows] == [
        ("ibr-co-14", "credit_note"),
        ("ibr-124", "credit_note"),
    ]
    assert all(r["reason"] for r in rows)


# ---------------------------------------------------------------- code lists


def test_codelists_every_inline_list_has_at_least_five_codes(tmp_path: Path):
    out = tmp_path / "codelists.tsv"
    res = run("extract_codelists.py", "--out", str(out))
    assert res.returncode == 0, res.stderr
    rows = read_tsv(out)
    assert rows, "no code lists extracted"
    by_list: dict[str, list[str]] = {}
    for r in rows:
        by_list.setdefault(r["list_id"], []).append(r["code"])
    for list_id, codes in by_list.items():
        assert len(codes) >= 5, f"{list_id} has {len(codes)} codes"
        assert len(codes) == len(set(codes)), f"{list_id} has duplicate codes"
    rule_ids = {k.split("#")[0] for k in by_list}
    for must in ("ibr-cl-03", "ibr-cl-07", "ibr-cl-23", "ibr-001-ae", "ibr-005-ae", "ibr-006-ae",
                 "ibr-010-ae", "ibr-011-ae", "ibr-012-ae", "ibr-013-ae", "ibr-139-ae"):
        assert must in rule_ids, must
    # official numbers
    assert len(by_list["ibr-139-ae"]) == 6
    assert "AE" in by_list["ibr-010-ae"] and len(by_list["ibr-010-ae"]) == 249
    assert set(by_list["ibr-139-ae"]) == {"S", "E", "O", "AE", "Z", "N"}  # ASCII N (defect 1)


def test_committed_codelists_are_current(tmp_path: Path):
    out = tmp_path / "codelists.tsv"
    assert run("extract_codelists.py", "--out", str(out)).returncode == 0
    committed = RULESET / "codelists" / "codelists.tsv"
    assert committed.read_text(encoding="utf-8") == out.read_text(encoding="utf-8")


# ---------------------------------------------------------------- runners on the 30 examples


@pytest.fixture(scope="module")
def saxon_json(tmp_path_factory: pytest.TempPathFactory) -> dict:
    out = tmp_path_factory.mktemp("sax") / "saxon.json"
    res = run("run_schematron.py", str(EXAMPLES), "--out", str(out))
    assert res.returncode == 0, res.stderr
    return json.loads(out.read_text())


@pytest.fixture(scope="module")
def xsd_json(tmp_path_factory: pytest.TempPathFactory) -> dict:
    out = tmp_path_factory.mktemp("xsd") / "xsd.json"
    res = run("xsd_check.py", str(EXAMPLES), "--out", str(out))
    assert res.returncode == 0, res.stderr
    return json.loads(out.read_text())


def test_v1_schematron_zero_failed_asserts_on_all_30_examples(saxon_json: dict):
    assert len(saxon_json) == 30
    assert {d["kind"] for d in saxon_json.values()} == {"invoice", "credit_note"}
    assert sum(1 for d in saxon_json.values() if d["kind"] == "credit_note") == 3
    for name, d in saxon_json.items():
        assert d["failed"] == {}, name


def test_v1_xsd_29_of_30_valid(xsd_json: dict):
    assert len(xsd_json) == 30
    invalid = [k for k, d in xsd_json.items() if not d["valid"]]
    assert [Path(k).name for k in invalid] == ["Volume-discount-credit-note.xml"]  # defect 3


def test_runner_detects_a_mutation(tmp_path: Path):
    src = EXAMPLES / "trn-invoice" / "Standard tax invoice.xml"
    text = src.read_text(encoding="utf-8")
    assert "<cbc:UUID>" in text
    import re

    mutated = re.sub(r"<cbc:UUID>.*?</cbc:UUID>", "", text, flags=re.S)
    d = tmp_path / "m"
    d.mkdir()
    (d / "mut.xml").write_text(mutated, encoding="utf-8")
    out = tmp_path / "s.json"
    assert run("run_schematron.py", str(d), "--out", str(out)).returncode == 0
    failed = json.loads(out.read_text())["mut.xml"]["failed"]
    assert failed, "removing the UUID must fail at least one official assert"


def test_runner_records_a_stylesheet_error_per_document_and_continues(tmp_path: Path):
    # A VAT breakdown entry without IBT-116 makes the official aligned stylesheet raise
    # XPTY0004 in u:slack() (Task 9 concern). That document gets {"error": ...}; the others are
    # still validated.
    src = EXAMPLES / "trn-invoice" / "Standard tax invoice.xml"
    text = src.read_text(encoding="utf-8")
    import re

    crashing, n = re.subn(r"<cbc:TaxableAmount[^>]*>[^<]*</cbc:TaxableAmount>", "", text)
    assert n == 1
    d = tmp_path / "m"
    d.mkdir()
    (d / "a-crash.xml").write_text(crashing, encoding="utf-8")
    (d / "b-ok.xml").write_text(text, encoding="utf-8")
    out = tmp_path / "s.json"
    res = run("run_schematron.py", str(d), "--out", str(out))
    assert res.returncode == 0, res.stderr
    got = json.loads(out.read_text())
    assert set(got) == {"a-crash.xml", "b-ok.xml"}
    assert "u:slack()" in got["a-crash.xml"]["error"]
    assert "failed" not in got["a-crash.xml"]
    assert got["a-crash.xml"]["kind"] == "invoice"
    assert got["b-ok.xml"] == {"kind": "invoice", "failed": {}}
    assert "1 stylesheet error" in res.stdout


# ---------------------------------------------------------------- compare.py


def write(tmp: Path, name: str, obj) -> str:
    p = tmp / name
    p.write_text(json.dumps(obj))
    return str(p)


def cmp(tmp: Path, rust: dict, saxon: dict, xsd: dict) -> subprocess.CompletedProcess[str]:
    return run(
        "compare.py",
        "--rust", write(tmp, "rust.json", rust),
        "--saxon", write(tmp, "saxon.json", saxon),
        "--xsd", write(tmp, "xsd.json", xsd),
        "--allow", str(CONF / "allowlist.tsv"),
    )


def doc(kind="invoice", failed=None):
    return {"kind": kind, "failed": failed or {}}


OK_XSD = {"valid": True, "error": ""}


def test_compare_equal_passes(tmp_path: Path):
    rust = {"a.xml": {"rules": {"ibr-co-10": 2}, "errors": 2, "exported": False}}
    saxon = {"a.xml": doc(failed={"ibr-co-10": 2})}
    r = cmp(tmp_path, rust, saxon, {"a.xml": OK_XSD})
    assert r.returncode == 0, r.stdout + r.stderr


def test_compare_multiplicity_difference_fails(tmp_path: Path):
    rust = {"a.xml": {"rules": {"ibr-co-10": 1}, "errors": 1, "exported": False}}
    saxon = {"a.xml": doc(failed={"ibr-co-10": 2})}
    r = cmp(tmp_path, rust, saxon, {"a.xml": OK_XSD})
    assert r.returncode == 1
    assert "ibr-co-10" in r.stdout


def test_compare_platform_rules_are_not_compared(tmp_path: Path):
    rust = {"a.xml": {"rules": {"AE-FMT-001": 1}, "errors": 1, "exported": False}}
    r = cmp(tmp_path, rust, {"a.xml": doc()}, {"a.xml": OK_XSD})
    assert r.returncode == 0, r.stdout


def test_compare_allowlist_applies_only_to_listed_kind(tmp_path: Path):
    rust = {
        "cn.xml": {"rules": {"ibr-co-14": 1}, "errors": 1, "exported": False},
        "inv.xml": {"rules": {"ibr-co-14": 1}, "errors": 1, "exported": False},
    }
    saxon = {"cn.xml": doc("credit_note"), "inv.xml": doc("invoice")}
    xsd = {"cn.xml": OK_XSD, "inv.xml": OK_XSD}
    r = cmp(tmp_path, rust, saxon, xsd)
    assert r.returncode == 1
    assert "inv.xml" in r.stdout and "cn.xml" not in r.stdout


def test_compare_error_free_doc_must_be_xsd_valid(tmp_path: Path):
    rust = {"a.xml": {"rules": {}, "errors": 0, "exported": True}}
    xsd = {"a.xml": {"valid": False, "error": "boom"}}
    r = cmp(tmp_path, rust, {"a.xml": doc()}, xsd)
    assert r.returncode == 1 and "XSD" in r.stdout


def test_compare_xsd_invalid_is_fine_when_rust_reports_errors(tmp_path: Path):
    rust = {"a.xml": {"rules": {"ibr-co-10": 1}, "errors": 1, "exported": False}}
    r = cmp(
        tmp_path, rust, {"a.xml": doc(failed={"ibr-co-10": 1})},
        {"a.xml": {"valid": False, "error": "x"}},
    )
    assert r.returncode == 0, r.stdout


def test_compare_error_free_doc_must_pass_saxon(tmp_path: Path):
    rust = {"a.xml": {"rules": {}, "errors": 0, "exported": True}}
    r = cmp(tmp_path, rust, {"a.xml": doc(failed={"ibr-co-10": 1})}, {"a.xml": OK_XSD})
    assert r.returncode == 1


def test_compare_missing_document_fails(tmp_path: Path):
    rust = {"a.xml": {"rules": {}, "errors": 0, "exported": True}}
    r = cmp(tmp_path, rust, {}, {"a.xml": OK_XSD})
    assert r.returncode == 1 and "a.xml" in r.stdout


SLACK_ERROR = " An empty sequence is not allowed as the first argument of u:slack()"


def crashed(kind="invoice", error=SLACK_ERROR):
    return {"kind": kind, "error": error}


def test_compare_skips_a_known_stylesheet_error_on_a_document_rust_rejects(tmp_path: Path):
    rust = {
        "a.xml": {"rules": {"aligned-ibrp-045": 1}, "errors": 1, "exported": False},
        "b.xml": {"rules": {}, "errors": 0, "exported": True},
    }
    saxon = {"a.xml": crashed(), "b.xml": doc()}
    r = cmp(tmp_path, rust, saxon, {"a.xml": OK_XSD, "b.xml": OK_XSD})
    assert r.returncode == 0, r.stdout
    assert "SKIP a.xml" in r.stdout and "u:slack" in r.stdout
    assert "1 skipped" in r.stdout


def test_compare_a_stylesheet_error_on_a_document_rust_accepts_fails(tmp_path: Path):
    rust = {"a.xml": {"rules": {}, "errors": 0, "exported": True}}
    r = cmp(tmp_path, rust, {"a.xml": crashed()}, {"a.xml": OK_XSD})
    assert r.returncode == 1
    assert "a.xml" in r.stdout and "SKIP" not in r.stdout


def test_compare_an_unknown_stylesheet_error_fails(tmp_path: Path):
    rust = {"a.xml": {"rules": {"ibr-co-10": 1}, "errors": 1, "exported": False}}
    saxon = {"a.xml": crashed(error="XTDE0640 something else")}
    r = cmp(tmp_path, rust, saxon, {"a.xml": OK_XSD})
    assert r.returncode == 1
    assert "a.xml" in r.stdout and "XTDE0640" in r.stdout


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-q", *sys.argv[1:]]))
