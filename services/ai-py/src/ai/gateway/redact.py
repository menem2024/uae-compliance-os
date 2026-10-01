"""Masks PII in text that could echo model input (contract section 2). Not a licence to log content."""

import re

_EMAIL = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}")
_IBAN_AE = re.compile(r"\bAE\s?(?:\d\s?){21}\b", re.IGNORECASE)
_CARD = re.compile(r"\b(?:\d[ -]?){15}\d\b")
_PHONE_INTL = re.compile(r"(?:\+|00)971[\s-]?(?:\d[\s-]?){7,9}\d")
_PHONE_LOCAL = re.compile(r"\b0(?:5\d|[2-47-9])[\s-]?(?:\d[\s-]?){6}\d\b")


def redact(text: str) -> str:
    text = _EMAIL.sub("[email]", text)
    text = _IBAN_AE.sub("[iban]", text)
    text = _CARD.sub("[card]", text)
    text = _PHONE_INTL.sub("[phone]", text)
    return _PHONE_LOCAL.sub("[phone]", text)
