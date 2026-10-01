"""Pure normalisation of extracted values (spec section 5.5) and comparison folding (spec section 5.6).

Stored values: Western digits, decimal strings without separators or symbols (decimals kept as printed),
ISO dates, upper-case codes, NFKC text with collapsed whitespace. Arabic letter-variant folding is used
only by `fold_text` (comparison), never on stored values.
"""

import re
import unicodedata
from decimal import Decimal, InvalidOperation

from ai.agents.extraction.schema import (
    CODE_FIELDS,
    DATE_FIELDS,
    DECIMAL_FIELDS,
    TRN_FIELDS,
    ExtractedInvoice,
    flatten,
    leaf,
)

_DIGITS = str.maketrans("٠١٢٣٤٥٦٧٨٩۰۱۲۳۴۵۶۷۸۹", "01234567890123456789")
_ARABIC_SEPARATORS = str.maketrans({"٫": ".", "٬": ",", "،": ","})
_CURRENCY = re.compile(r"(?i)\b(?:aed|usd|eur|dhs?|dirhams?)\b|د\.إ|درهم|دراهم|[$€£%]")
_DECIMAL = re.compile(r"^[+-]?[0-9]+(?:\.[0-9]+)?$")
_WS = re.compile(r"\s+")
_FOLD = str.maketrans({"أ": "ا", "إ": "ا", "آ": "ا", "ى": "ي", "ة": "ه"})

_MONTHS = {
    "january": 1, "february": 2, "march": 3, "april": 4, "may": 5, "june": 6, "july": 7, "august": 8,
    "september": 9, "october": 10, "november": 11, "december": 12,
    "jan": 1, "feb": 2, "mar": 3, "apr": 4, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "sept": 9, "oct": 10,
    "nov": 11, "dec": 12,
    "يناير": 1, "فبراير": 2, "مارس": 3, "ابريل": 4, "مايو": 5, "يونيو": 6, "يوليو": 7, "اغسطس": 8,
    "سبتمبر": 9, "اكتوبر": 10, "نوفمبر": 11, "ديسمبر": 12,
    "كانون الثاني": 1, "شباط": 2, "اذار": 3, "نيسان": 4, "ايار": 5, "حزيران": 6, "تموز": 7, "اب": 8,
    "ايلول": 9, "تشرين الاول": 10, "تشرين الثاني": 11, "كانون الاول": 12,
}
_YMD = re.compile(r"^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})$")
_DMY = re.compile(r"^(\d{1,2})[-/.](\d{1,2})[-/.](\d{4})$")
_D_MONTH_Y = re.compile(r"^(\d{1,2})\s+(.+?)\s+(\d{4})$")


def western_digits(s: str) -> str:
    return s.translate(_DIGITS)


def normalize_text(s: str) -> str:
    return _WS.sub(" ", unicodedata.normalize("NFKC", s)).strip()


def fold_text(s: str) -> str:
    """Comparison only: NFKC + casefold + whitespace collapse + Arabic letter-variant folding."""
    return normalize_text(s).casefold().translate(_FOLD)


def normalize_decimal(s: str) -> str:
    """'١٬٢٣٤٫٥٠ د.إ' -> '1234.50'; '5%' -> '5'; '(12.00)' -> '-12.00'; '' -> ''.

    Commas are thousands separators (UAE convention). Returns the cleaned text unchanged when it is not a
    decimal after cleaning, so a garbled value never compares equal to the truth.
    """
    t = western_digits(normalize_text(s)).translate(_ARABIC_SEPARATORS)
    if not t:
        return ""
    neg = t.startswith("(") and t.endswith(")")
    t = _CURRENCY.sub("", t.strip("()")).replace(",", "").replace(" ", "")
    if neg and not t.startswith("-"):
        t = "-" + t
    t = t.removeprefix("+")
    return t if _DECIMAL.match(t) else normalize_text(s)


def to_decimal(s: str) -> Decimal | None:
    v = normalize_decimal(s)
    if not v or not _DECIMAL.match(v):
        return None
    try:
        return Decimal(v)
    except InvalidOperation:
        return None


def normalize_date(s: str) -> str:
    """To YYYY-MM-DD. Accepts ISO, DD/MM/YYYY, DD-MM-YYYY, DD.MM.YYYY, YYYY/MM/DD and `15 March 2026` /
    `15 مارس 2026` (Gulf and Levantine month names). Returns the cleaned text when it cannot parse."""
    t = western_digits(normalize_text(s))
    if not t:
        return ""
    y = mo = d = 0
    if m := _YMD.match(t):
        y, mo, d = int(m[1]), int(m[2]), int(m[3])
    elif m := _DMY.match(t):
        d, mo, y = int(m[1]), int(m[2]), int(m[3])
    elif (m := _D_MONTH_Y.match(t)) and (month := _MONTHS.get(fold_text(m[2]).rstrip("."))):
        d, mo, y = int(m[1]), month, int(m[3])
    if 1 <= mo <= 12 and 1 <= d <= 31 and 1900 <= y <= 2100:
        return f"{y:04d}-{mo:02d}-{d:02d}"
    return t


def normalize_trn(s: str) -> str:
    """Western digits with spaces and hyphens removed; anything else is kept (the checks flag it)."""
    return re.sub(r"[\s-]", "", western_digits(normalize_text(s)))


def normalize_code(s: str) -> str:
    return western_digits(normalize_text(s)).upper()


def normalize_value(path: str, value: str) -> str:
    """Normalise one value by its field-set name (`leaf(path)`)."""
    name = leaf(path)
    if name in DECIMAL_FIELDS:
        return normalize_decimal(value)
    if name in DATE_FIELDS:
        return normalize_date(value)
    if name in TRN_FIELDS:
        return normalize_trn(value)
    if name in CODE_FIELDS:
        return normalize_code(value)
    return normalize_text(value)


def _set(obj: object, dotted: str, value: str) -> None:
    *parents, last = dotted.split(".")
    for p in parents:
        obj = getattr(obj, p)
    setattr(obj, last, value)


def normalize_invoice(inv: ExtractedInvoice) -> ExtractedInvoice:
    """A normalised copy; the input is not modified."""
    out = inv.model_copy(deep=True)
    for path, value in flatten(inv).items():
        head, _, rest = path.partition("].")
        if rest:
            name, idx = head.split("[")
            target: object = getattr(out, name)[int(idx)]
            _set(target, rest, normalize_value(path, value))
        else:
            _set(out, path, normalize_value(path, value))
    return out
