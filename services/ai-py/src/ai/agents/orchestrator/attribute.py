"""`intake.attribute` (spec section 5.2) and `direction_of`.

Runs off the first-stage extraction, in parallel with the verify block. When the extracted seller/buyer TRN
matches exactly one candidate ClientCompany other than the chosen one, and the chosen one's TRN matches
neither party, it raises a `document.attribution` proposal (contract section 5; api-go's AttributionApplier
reads `client_company_id` and the optional `direction`). Nothing here reaches a model (contract rule 9).
"""

from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass
from typing import Literal

from ai.agents.extraction.normalize import normalize_trn
from ai.agents.extraction.schema import ExtractedInvoice, ExtractionOutput
from ai.agents.verifier.invoice import UNKNOWN_CONFIDENCE
from ai.gen.compliance.v1 import documents_pb2
from ai.runtime.context import NodeContext
from ai.runtime.errors import ToolTransientError
from ai.runtime.graph import Node
from ai.runtime.proposals import EvidenceDraft, FieldChangeDraft, ProposalDraft
from ai.runtime.types import StepKind

type Direction = Literal["issued", "received", "unknown"]

AGENT = "intake"
NODE_ID = "intake.attribute"
SOURCE_NODE = "extraction.extract"
KIND = "document.attribution"
SUMMARY_KEY = "P1Agents.proposal.attribution"


@dataclass(frozen=True, slots=True)
class AttributionResult:
    suggested_client_company_id: str | None


def direction_of(invoice: ExtractedInvoice, client_company_trn: str) -> Direction:
    """Relative to the ClientCompany (contract section 4.2): `issued` when it is the seller."""
    if not client_company_trn:
        return "unknown"
    if invoice.seller_trn == client_company_trn:
        return "issued"
    if invoice.buyer_trn == client_company_trn:
        return "received"
    return "unknown"


def client_company_trn(candidates: Sequence[documents_pb2.ClientCompanyRef], client_company_id: str) -> str:
    return next((normalize_trn(c.trn) for c in candidates if c.client_company_id == client_company_id), "")


def suggest(invoice: ExtractedInvoice, candidates: Sequence[documents_pb2.ClientCompanyRef],
            client_company_id: str) -> tuple[documents_pb2.ClientCompanyRef, str] | None:
    """(the one other matching candidate, the matched path) or None."""
    parties = [(p, t) for p, t in (("seller_trn", invoice.seller_trn), ("buyer_trn", invoice.buyer_trn)) if t]
    chosen = client_company_trn(candidates, client_company_id)
    if not parties or (chosen and any(t == chosen for _, t in parties)):
        return None
    matches: dict[str, tuple[documents_pb2.ClientCompanyRef, str]] = {}
    for c in candidates:
        trn = normalize_trn(c.trn)
        if c.client_company_id == client_company_id or not trn:
            continue
        path = next((p for p, t in parties if t == trn), None)
        if path is not None:
            matches.setdefault(c.client_company_id, (c, path))
    return next(iter(matches.values())) if len(matches) == 1 else None


def attribute_node(candidates: Sequence[documents_pb2.ClientCompanyRef], client_company_id: str,
                   document_id: str) -> Node:
    candidates = tuple(candidates)

    async def fn(ctx: NodeContext) -> AttributionResult:
        out = ctx.result(SOURCE_NODE, ExtractionOutput)
        hit = suggest(out.invoice, candidates, client_company_id)
        if hit is None:
            return AttributionResult(None)
        cand, path = hit
        trn = normalize_trn(cand.trn)
        conf = {fc.path: fc.confidence for fc in out.field_confidence}.get(path, UNKNOWN_CONFIDENCE)
        draft = ProposalDraft(
            kind=KIND, target_type="document", target_id=document_id, summary_key=SUMMARY_KEY,
            summary_args={"trn": trn}, confidence=round(min(1.0, max(0.0, conf)), 3),
            changes=(FieldChangeDraft("client_company_id", client_company_id, cand.client_company_id),
                     FieldChangeDraft("direction",
                                      direction_of(out.invoice, client_company_trn(candidates, client_company_id)),
                                      direction_of(out.invoice, trn))),
            evidence=(EvidenceDraft("extracted_field", path, trn),))
        try:
            await ctx.propose(draft)
        except Exception as exc:
            # EventSink.proposal re-raises its publish error (contract 3.5 rule 9); as ToolTransientError the
            # node's RetryPolicy retries it, and a failure that outlives the retries naks the run.
            raise ToolTransientError("proposal publish failed") from exc
        return AttributionResult(cand.client_company_id)

    return Node(NODE_ID, AGENT, "attribute", StepKind.DETERMINISTIC, fn, depends_on=(SOURCE_NODE,),
                critical=False, feed_key="intake.attribution_checked")
