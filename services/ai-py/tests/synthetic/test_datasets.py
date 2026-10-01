"""Eval dataset plans, build and --check (spec sections 5.5, 5.6)."""

import hashlib
import json
from collections import Counter

from ai.synthetic.__main__ import main
from ai.synthetic.datasets import (
    DATASETS,
    build,
    extraction_cases,
    importer_cases,
    intake_extra_cases,
    load_manifest,
    load_truth,
    verifier_cases,
)


def test_case_mix_is_fixed():
    ext = list(extraction_cases())
    assert len(ext) == 200 and len({c.case_id for c in ext}) == 200
    slices = Counter(next(t for t in c.tags if t.startswith("lang:")) + "/" +
                     next(t for t in c.tags if t.startswith("format:")) for c in ext)
    assert slices == {"lang:ar/format:pdf": 50, "lang:ar/format:image": 50, "lang:en/format:pdf": 50,
                      "lang:en/format:image": 50}
    assert sum("subset:pr" in c.tags for c in ext) == 40
    assert len(list(intake_extra_cases())) == 20
    imp = list(importer_cases())
    assert len(imp) == 40 and Counter(c.file.rsplit(".", 1)[1] for c in imp) == {"xlsx": 20, "csv": 20}
    assert any("encoding:cp1256" in c.tags for c in imp)
    ver = list(verifier_cases())
    assert len(ver) == 200 and sum("corrupted" in c.tags for c in ver) == 100
    assert all(c.file.startswith("../extraction-v1/files/") for c in ver)


def test_build_then_check(tmp_path):
    assert build(tmp_path, limit=2) == []
    for name in DATASETS:
        cases = load_manifest(tmp_path, name)
        truth = load_truth(tmp_path, name)
        assert [c["case_id"] for c in cases] == list(truth)
        for c in cases:
            data = (tmp_path / name / str(c["file"])).resolve().read_bytes()
            assert hashlib.sha256(data).hexdigest() == c["sha256"]
    assert build(tmp_path, limit=2, check=True) == []

    manifest = tmp_path / "importer-v1" / "manifest.json"
    doc = json.loads(manifest.read_text())
    doc["cases"][0]["sha256"] = "0" * 64
    manifest.write_text(json.dumps(doc))
    problems = build(tmp_path, ["importer-v1"], limit=2, check=True)
    assert problems and "import-ar-xlsx-000" in problems[1]


def test_cli_exit_codes(tmp_path):
    assert main(["build", "--root", str(tmp_path), "--dataset", "importer-v1"]) == 0
    assert main(["build", "--root", str(tmp_path), "--dataset", "importer-v1", "--check"]) == 0
    (tmp_path / "importer-v1" / "truth.jsonl").write_text("")
    assert main(["build", "--root", str(tmp_path), "--dataset", "importer-v1", "--check"]) == 1
