"""Value equality under the spec section 5.6 normalisation (shared by the critic and the eval scorers)."""

from ai.agents.extraction.normalize import fold_text, normalize_value, to_decimal
from ai.agents.extraction.schema import DECIMAL_FIELDS, leaf


def values_equal(path: str, a: str, b: str) -> bool:
    """Decimals compare numerically ("5" == "5.00"), dates as ISO strings, TRNs and codes exactly after
    normalisation, text after NFKC + casefold + whitespace collapse + Arabic letter-variant folding."""
    x, y = normalize_value(path, a), normalize_value(path, b)
    if leaf(path) in DECIMAL_FIELDS:
        dx, dy = to_decimal(x), to_decimal(y)
        if dx is not None and dy is not None:
            return dx == dy
        return x == y
    return fold_text(x) == fold_text(y)
