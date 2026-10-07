"""Intake agent output (spec section 5.5)."""

from typing import Literal

from pydantic import BaseModel, ConfigDict

type DocumentKind = Literal["invoice", "credit_note", "contract", "other"]
type Language = Literal["ar", "en", "mixed"]


class IntakeResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    kind: DocumentKind
    language: Language
    invoice_count: int  # invoices in the file (a multi-invoice PDF has > 1)
    seller_trn: str  # as printed, normalised to digits; "" when absent
    buyer_trn: str
    confidence: float  # the model's confidence in `kind`, [0, 1]
