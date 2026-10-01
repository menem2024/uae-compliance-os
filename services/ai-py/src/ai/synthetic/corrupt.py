"""Seeded, realistic extraction corruptions for the verifier suite (spec section 5.6).

A corruption models a reading error of the extractor (a swapped digit, a dropped TRN digit, a day/month swap),
applied to the truth of a document. The document itself is unchanged, so the critic can detect it.
"""

import random
from dataclasses import dataclass
from decimal import Decimal

from ai.agents.extraction.schema import ExtractedInvoice, flatten

CORRUPTIONS: tuple[str, ...] = (
    "total_digit_swap", "vat_digit_swap", "line_net_digit_swap", "line_quantity_changed", "seller_trn_digit_drop",
    "buyer_trn_digit_changed", "issue_date_day_month_swap", "invoice_number_digit_swap", "line_tax_rate_changed",
)


@dataclass(frozen=True, slots=True)
class Corruption:
    code: str
    path: str
    truth_value: str
    corrupted_value: str


def _swap_digits(value: str, rng: random.Random) -> str | None:
    idx = [i for i in range(len(value) - 1)
           if value[i].isdigit() and value[i + 1].isdigit() and value[i] != value[i + 1]]
    if not idx:
        return None
    i = rng.choice(idx)
    return value[:i] + value[i + 1] + value[i] + value[i + 2:]


def _drop_digit(trn: str, rng: random.Random) -> str | None:
    if len(trn) != 15:
        return None
    i = rng.randrange(3, 15)
    return trn[:i] + trn[i + 1:]


def _change_digit(trn: str, rng: random.Random) -> str | None:
    if len(trn) != 15:
        return None
    i = rng.randrange(3, 15)
    return trn[:i] + str((int(trn[i]) + rng.randint(1, 9)) % 10) + trn[i + 1:]


def _swap_day_month(iso: str) -> str | None:
    y, m, d = (iso.split("-") + ["", "", ""])[:3]
    return f"{y}-{d}-{m}" if d and m and d != m and int(d) <= 12 else None


def _options(t: ExtractedInvoice, rng: random.Random) -> list[tuple[str, str, str | None]]:
    """(code, path, corrupted value or None when not applicable), in CORRUPTIONS order."""
    li = rng.randrange(len(t.lines)) if t.lines else 0
    ln = t.lines[li] if t.lines else None
    return [
        ("total_digit_swap", "total_amount", _swap_digits(t.total_amount, rng)),
        ("vat_digit_swap", "vat_amount", _swap_digits(t.vat_amount, rng)),
        ("line_net_digit_swap", f"lines[{li}].net_amount", _swap_digits(ln.net_amount, rng) if ln else None),
        ("line_quantity_changed", f"lines[{li}].quantity",
         str(Decimal(ln.quantity) + 1) if ln and ln.quantity else None),
        ("seller_trn_digit_drop", "seller_trn", _drop_digit(t.seller_trn, rng)),
        ("buyer_trn_digit_changed", "buyer_trn", _change_digit(t.buyer_trn, rng)),
        ("issue_date_day_month_swap", "issue_date", _swap_day_month(t.issue_date)),
        ("invoice_number_digit_swap", "invoice_number", _swap_digits(t.invoice_number, rng)),
        ("line_tax_rate_changed", f"lines[{li}].tax.rate",
         ("0" if ln.tax.rate == "5" else "5") if ln and ln.tax.rate else None),
    ]


def set_path(t: ExtractedInvoice, path: str, value: str) -> None:
    """Sets one field-set path (`lines[2].tax.rate`, `seller.name`) in place."""
    head, _, rest = path.partition("].")
    obj: object = t
    dotted = path
    if rest:
        name, idx = head.split("[")
        obj, dotted = getattr(t, name)[int(idx)], rest
    *parents, last = dotted.split(".")
    for p in parents:
        obj = getattr(obj, p)
    setattr(obj, last, value)


def corrupt(truth: ExtractedInvoice, seed: int) -> tuple[ExtractedInvoice, Corruption]:
    """One applicable corruption, chosen by seed; the input is not modified."""
    rng = random.Random(f"verifier-corruption:{seed}")
    options = [(c, p, v) for c, p, v in _options(truth, rng) if v is not None]
    code, path, value = options[rng.randrange(len(options))]
    assert value is not None
    before = flatten(truth)[path]
    out = truth.model_copy(deep=True)
    set_path(out, path, value)
    return out, Corruption(code=code, path=path, truth_value=before, corrupted_value=value)
