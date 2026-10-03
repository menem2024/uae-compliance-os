"""Binding field-accuracy scorer (spec section 5.6).

Equality is `values_equal` (the Verifier critic's comparison), so the Verifier's agreement check and the eval
scorer can never disagree about what "equal" means.
"""

from __future__ import annotations

import re
from collections.abc import Iterable, Mapping, Sequence

from ai.agents.extraction.compare import values_equal
from ai.agents.extraction.normalize import normalize_value
from ai.agents.extraction.schema import ExtractedInvoice, flatten
from ai.evals.core import CaseScore

_ROW = re.compile(r"^((?:lines|tax_breakdown)\[\d+\])\.")


def field_accuracy(truth_flat: Mapping[str, str], pred_flat: Mapping[str, str],
                   paths: Iterable[str]) -> tuple[float, Mapping[str, object]]:
    """`correct / counted` over `paths`.

    Header fields count when truth or prediction is non-empty. Lines and tax breakdown rows are matched by
    position: every field of every truth row counts (a missing predicted row is wrong field by field), and each
    extra predicted row adds its non-empty fields as wrong. Details carry the counts and the wrong paths only
    (never values).
    """
    truth_rows = {m.group(1) for p in truth_flat if (m := _ROW.match(p))}
    counted = correct = 0
    wrong: list[str] = []
    for path in dict.fromkeys(paths):
        t, p = truth_flat.get(path, ""), pred_flat.get(path, "")
        row = _ROW.match(path)
        if row and row.group(1) not in truth_rows:  # an extra predicted row
            if not normalize_value(path, p):
                continue
            counted += 1
            wrong.append(path)
            continue
        if not row and not (normalize_value(path, t) or normalize_value(path, p)):
            continue
        counted += 1
        if values_equal(path, t, p):
            correct += 1
        else:
            wrong.append(path)
    return (correct / counted if counted else 1.0), {"counted": counted, "correct": correct,
                                                     "wrong_paths": tuple(wrong)}


def score_invoices(truth: Sequence[ExtractedInvoice],
                   pred: Sequence[ExtractedInvoice]) -> tuple[float, Mapping[str, object]]:
    """field_accuracy over invoices matched by position; a missing predicted invoice is wrong field by field and
    an extra one adds its non-empty fields as wrong. Counts are summed, so the ratio is micro-averaged."""
    counted = correct = 0
    wrong: list[str] = []
    for i in range(max(len(truth), len(pred))):
        tf = flatten(truth[i]) if i < len(truth) else {}
        pf = flatten(pred[i]) if i < len(pred) else {}
        _, d = field_accuracy(tf, pf, [*tf, *pf])
        counted += int(d["counted"])  # type: ignore[call-overload]
        correct += int(d["correct"])  # type: ignore[call-overload]
        wrong += [f"[{i}].{p}" if len(truth) > 1 or len(pred) > 1 else p
                  for p in d["wrong_paths"]]  # type: ignore[attr-defined]
    return (correct / counted if counted else 1.0), {"counted": counted, "correct": correct,
                                                     "wrong_paths": tuple(wrong)}


def accuracy_metrics(acc: float, details: Mapping[str, object], name: str = "field_accuracy") -> dict[str, float]:
    return {name: acc, f"{name}#counted": float(details["counted"]),  # type: ignore[arg-type]
            f"{name}#correct": float(details["correct"])}  # type: ignore[arg-type]


def micro_average(scores: Sequence[CaseScore], metric: str) -> float:
    """Micro-average of `metric` over the scores that carry it: `sum(correct) / sum(counted)` when the metric
    ships its `#counted`/`#correct` companions, else the plain mean. Nothing to count is a perfect 1.0."""
    num = den = 0.0
    plain: list[float] = []
    weighted = False
    for s in scores:
        if metric not in s.metrics:
            continue
        if f"{metric}#counted" in s.metrics:
            weighted = True
            num += s.metrics[f"{metric}#correct"]
            den += s.metrics[f"{metric}#counted"]
        else:
            plain.append(s.metrics[metric])
    if weighted:
        return num / den if den else 1.0
    return sum(plain) / len(plain) if plain else 1.0
