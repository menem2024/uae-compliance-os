"""VerdictResult -> compliance.v1.VerifierVerdict (contract section 4.1)."""

from ai.agents.verifier.core import VerdictResult
from ai.gen.compliance.v1 import agents_pb2

VERDICTS = {"accept": agents_pb2.VERDICT_ACCEPT, "revise": agents_pb2.VERDICT_REVISE,
            "escalate": agents_pb2.VERDICT_ESCALATE}


def to_proto(v: VerdictResult) -> agents_pb2.VerifierVerdict:
    msg = agents_pb2.VerifierVerdict(verdict=VERDICTS[v.verdict], confidence=v.confidence,
                                     critic_model=v.critic_model, revisions=v.revisions)
    for f in v.findings:
        msg.findings.add(path=f.path, code=f.code, severity=f.severity, observed=f.observed, expected=f.expected,
                         evidence_ref=f.evidence_ref, source=f.source)
    return msg
