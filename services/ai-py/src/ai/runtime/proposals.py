"""ProposalDraft and deterministic proposal ids (contract section 5)."""

import uuid
from collections.abc import Mapping
from dataclasses import dataclass, field
from typing import Literal

from google.protobuf.message import Message as ProtoMessage

from ai.canonical import canonical_json, sha256_hex
from ai.runtime.types import RunIdentity

PROPOSAL_NS = uuid.UUID("6f1c9a52-6c55-4c2e-9d0f-3b7f0b1f5e21")


@dataclass(frozen=True, slots=True)
class FieldChangeDraft:
    path: str
    old_value: str
    new_value: str


@dataclass(frozen=True, slots=True)
class EvidenceDraft:
    kind: str
    ref: str
    excerpt: str = ""


@dataclass(frozen=True, slots=True)
class ProposalDraft:
    kind: str  # "<domain>.<action>", e.g. "document.attribution", "invoice.field_fix"
    target_type: Literal["document", "invoice", "validation_issue", "source", "client_company"]
    target_id: str
    summary_key: str  # i18n key (owner track's web namespace)
    summary_args: Mapping[str, str] = field(default_factory=dict)
    rationale: str = ""
    confidence: float = 0.0
    changes: tuple[FieldChangeDraft, ...] = ()
    detail: ProtoMessage | None = None  # packed into Any
    evidence: tuple[EvidenceDraft, ...] = ()
    expires_in_s: int | None = 14 * 86400


def proposal_id(run: RunIdentity, draft: ProposalDraft) -> str:
    changes = [[c.path, c.old_value, c.new_value] for c in draft.changes]
    name = "|".join([run.firm_id, run.subject_type, run.subject_id, draft.kind, draft.target_type,
                     draft.target_id, sha256_hex(canonical_json(changes))])
    return str(uuid.uuid5(PROPOSAL_NS, name))
