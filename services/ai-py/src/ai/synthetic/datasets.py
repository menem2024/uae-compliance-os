"""Eval datasets (spec sections 5.5, 5.6): `extraction-v1`, `intake-extra-v1`, `importer-v1`, `verifier-v1`.

Each dataset directory holds a committed `manifest.json` (cases, tags, sha256 per binary) and a committed
`truth.jsonl`; the binaries under `files/` are regenerated (CI runs `build --check`) and never committed. The
case mix and seeds are fixed (spec section 8): changing them is a plan change, because replay keys depend on the
exact bytes.
"""

from __future__ import annotations

import hashlib
import json
import os
import tempfile
from collections.abc import Callable, Iterator, Sequence
from dataclasses import dataclass, field
from functools import partial
from pathlib import Path
from typing import Literal

from ai.agents.extraction.schema import ExtractedInvoice
from ai.canonical import canonical_json
from ai.synthetic.corrupt import corrupt
from ai.synthetic.generator import SyntheticInvoice, generate
from ai.synthetic.render_image import pdf_to_jpeg
from ai.synthetic.render_pdf import render_letter, render_pdf
from ai.synthetic.tabular import write_csv, write_xlsx

EXTRACTION = "extraction-v1"
INTAKE_EXTRA = "intake-extra-v1"
IMPORTER = "importer-v1"
VERIFIER = "verifier-v1"
PR_PER_SLICE = 10
SLICE_SIZE = 50

type Lang = Literal["ar", "en"]


@dataclass(frozen=True, slots=True)
class Case:
    case_id: str
    tags: tuple[str, ...]
    file: str  # relative to the dataset directory
    media_type: str
    truth: dict[str, object]
    render: Callable[[], bytes] = field(compare=False, repr=False)


def _sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def _render_invoice(inv: SyntheticInvoice, fmt: str, seed: int) -> bytes:
    pdf = render_pdf(inv)
    return pdf if fmt == "pdf" else pdf_to_jpeg(pdf, seed)


def _render_letter(lang: str, fmt: str, seed: int) -> bytes:
    pdf = render_letter(seed, lang)
    return pdf if fmt == "pdf" else pdf_to_jpeg(pdf, seed)


def _render_tabular(invoices: Sequence[ExtractedInvoice], lang: Lang, fmt: str,
                    encoding: Literal["utf-8-sig", "cp1256"]) -> bytes:
    return write_xlsx(invoices, lang) if fmt == "xlsx" else write_csv(invoices, lang, encoding=encoding)


# ------------------------------------------------------------------ case plans (no rendering)
def extraction_cases() -> Iterator[Case]:
    """200 cases: 50 each of ar-pdf, ar-image, en-pdf, en-image; the first 10 of each slice are `subset:pr`."""
    langs: tuple[Lang, ...] = ("ar", "en")
    for lang in langs:
        for fmt in ("pdf", "image"):
            for n in range(SLICE_SIZE):
                seed = n if fmt == "pdf" else SLICE_SIZE + n
                inv = generate(seed, lang)
                tags = [f"lang:{lang}", f"format:{fmt}", f"layout:{inv.layout}", *(f"defect:{d}" for d in inv.defects)]
                if n < PR_PER_SLICE:
                    tags.append("subset:pr")
                ext, media = ("pdf", "application/pdf") if fmt == "pdf" else ("jpg", "image/jpeg")
                case_id = f"{lang}-{fmt}-{n:03d}"
                yield Case(case_id, tuple(tags), f"files/{case_id}.{ext}", media,
                           {"invoice": inv.truth.model_dump(mode="json"), "kind": "invoice", "language": lang,
                            "defects": list(inv.defects)}, partial(_render_invoice, inv, fmt, seed))


def intake_extra_cases() -> Iterator[Case]:
    """20 non-invoice pages (a service agreement: kind `contract`), 10 per language, half as images."""
    for lang in ("ar", "en"):
        for n in range(10):
            seed = 1000 + n
            fmt = "pdf" if n % 2 == 0 else "image"
            ext, media = ("pdf", "application/pdf") if fmt == "pdf" else ("jpg", "image/jpeg")
            case_id = f"letter-{lang}-{fmt}-{n:03d}"
            yield Case(case_id, (f"lang:{lang}", f"format:{fmt}", "kind:contract"), f"files/{case_id}.{ext}", media,
                       {"kind": "contract", "language": lang}, partial(_render_letter, lang, fmt, seed))


def importer_cases() -> Iterator[Case]:
    """40 files: 20 XLSX then 20 CSV, alternating ar/en, 1-4 invoices each; every other Arabic CSV is CP1256."""
    for i in range(40):
        lang: Lang = "ar" if i % 2 == 0 else "en"
        fmt = "xlsx" if i < 20 else "csv"
        invoices = [generate(2000 + i * 10 + k, lang).truth for k in range(1 + i % 4)]
        encoding: Literal["utf-8-sig", "cp1256"] = (
            "cp1256" if fmt == "csv" and lang == "ar" and i % 4 == 0 else "utf-8-sig")
        ext, media = (("xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
                      if fmt == "xlsx" else ("csv", "text/csv"))
        case_id = f"import-{lang}-{fmt}-{i:03d}"
        tags = (f"lang:{lang}", f"format:{fmt}", f"encoding:{encoding}")
        yield Case(case_id, tags, f"files/{case_id}.{ext}", media,
                   {"invoices": [inv.model_dump(mode="json") for inv in invoices]},
                   partial(_render_tabular, invoices, lang, fmt, encoding))


def verifier_cases() -> Iterator[Case]:
    """200 cases over the extraction-v1 documents: odd positions carry one seeded corruption, even ones are clean."""
    for i, src in enumerate(extraction_cases()):
        truth = ExtractedInvoice.model_validate(src.truth["invoice"])
        corrupted = i % 2 == 1
        extraction, corruption = corrupt(truth, i) if corrupted else (truth, None)
        base = [t for t in src.tags if t.startswith(("lang:", "format:", "defect:"))]
        tags = (*base, "corrupted" if corrupted else "clean",
                *((f"corruption:{corruption.code}",) if corruption else ()))
        yield Case(f"verify-{src.case_id}", tags, f"../{EXTRACTION}/{src.file}", src.media_type,
                   {"source_case": src.case_id, "extraction": extraction.model_dump(mode="json"),
                    "corruption": None if corruption is None else {
                        "code": corruption.code, "path": corruption.path, "truth_value": corruption.truth_value,
                        "corrupted_value": corruption.corrupted_value}},
                   src.render)


DATASETS: dict[str, Callable[[], Iterator[Case]]] = {
    EXTRACTION: extraction_cases, INTAKE_EXTRA: intake_extra_cases, IMPORTER: importer_cases, VERIFIER: verifier_cases,
}


# ------------------------------------------------------------------ build and check
def _atomic_write(path: Path, data: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists() and path.read_bytes() == data:
        return
    fd, tmp = tempfile.mkstemp(dir=path.parent, prefix=".tmp-")
    with os.fdopen(fd, "wb") as f:
        f.write(data)
    Path(tmp).replace(path)


def manifest_and_truth(name: str, cases: list[Case], shas: dict[str, str]) -> tuple[bytes, bytes]:
    manifest = {"dataset": name, "version": 1, "cases": [
        {"case_id": c.case_id, "tags": list(c.tags), "file": c.file, "media_type": c.media_type,
         "sha256": shas[c.case_id]} for c in cases]}
    truth = "".join(canonical_json({"case_id": c.case_id, "truth": c.truth}) + "\n" for c in cases)
    return (json.dumps(manifest, ensure_ascii=False, indent=1) + "\n").encode(), truth.encode()


def build(root: Path, names: list[str] | None = None, *, check: bool = False, limit: int | None = None) -> list[str]:
    """Renders every binary into `<root>/<name>/files/` and writes manifest.json and truth.jsonl, or with
    check=True compares them with the committed ones instead (the binaries are still written, for the evals).
    Returns the problems found (empty = OK). The verifier set reuses extraction-v1's files."""
    problems: list[str] = []
    rendered: dict[str, str] = {}  # extraction case file -> sha, shared with the verifier set
    for name in names or list(DATASETS):
        cases = list(DATASETS[name]())[:limit]
        shas: dict[str, str] = {}
        for c in cases:
            path = (root / name / c.file).resolve()
            key = str(path)
            if key not in rendered:
                data = c.render()
                _atomic_write(path, data)
                rendered[key] = _sha(data)
            shas[c.case_id] = rendered[key]
        manifest, truth = manifest_and_truth(name, cases, shas)
        for fname, data in (("manifest.json", manifest), ("truth.jsonl", truth)):
            target = root / name / fname
            if not check:
                _atomic_write(target, data)
            elif not target.exists() or target.read_bytes() != data:
                problems.append(f"{name}/{fname} differs from the regenerated one")
                if target.exists() and fname == "manifest.json":
                    old = {c["case_id"]: c["sha256"] for c in json.loads(target.read_text())["cases"]}
                    problems += [f"{name}: {cid} sha256 {old.get(cid)} != {sha}" for cid, sha in shas.items()
                                 if old.get(cid) != sha][:20]
    return problems


def load_manifest(root: Path, name: str) -> list[dict[str, object]]:
    return list(json.loads((root / name / "manifest.json").read_text(encoding="utf-8"))["cases"])


def load_truth(root: Path, name: str) -> dict[str, dict[str, object]]:
    out: dict[str, dict[str, object]] = {}
    for line in (root / name / "truth.jsonl").read_text(encoding="utf-8").splitlines():
        if line:
            row = json.loads(line)
            out[row["case_id"]] = row["truth"]
    return out
