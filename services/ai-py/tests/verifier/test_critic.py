"""InvoiceCritic: which paths it re-reads, the request it sends, and how it judges the answers."""

from ai.agents.extraction.schema import ExtractionOutput, FieldConfidence
from ai.agents.verifier.core import Finding
from ai.agents.verifier.critic import CriticOutput, CriticReview, InvoiceCritic
from ai.gateway.types import OPUS, SONNET, DocumentPart, TextPart
from ai.synthetic.generator import generate

INV = generate(7, "en", defect_rate=0.0).truth
OUT = ExtractionOutput(invoice=INV, language="en",
                       field_confidence=[FieldConfidence(path="lines[0].quantity", confidence=0.4)])
FOCUS = [Finding("total_amount", "arithmetic.total", "block", related=("totals.tax_exclusive_amount", "vat_amount"))]


def test_review_paths_focus_first_then_critical_sample():
    paths = InvoiceCritic.review_paths(OUT, FOCUS)
    assert paths[:3] == ["total_amount", "totals.tax_exclusive_amount", "vat_amount"]
    assert paths[3:] == ["invoice_number", "issue_date", "seller_trn", "buyer_trn", "currency"]
    assert InvoiceCritic.review_paths(OUT, [Finding("", "x", "block")])[0] == "invoice_number"


def test_request_shape():
    critic = InvoiceCritic(model=OPUS, prompt_id="verifier.escalate")
    doc = DocumentPart.of(b"%PDF-1.7 fake")
    req = critic.build_request(OUT, [doc], ["total_amount", "seller.name"])
    assert (req.model, req.prompt_id, req.prompt_version, req.effort, req.max_tokens) == (
        OPUS, "verifier.escalate", 1, "medium", 2048)
    assert req.output_model is CriticOutput and req.cacheable
    (msg,) = req.messages
    assert msg.parts[0] is doc and isinstance(msg.parts[1], TextPart)
    assert f'- total_amount: "{INV.total_amount}"' in msg.parts[1].text
    assert "Firm" not in req.system  # rule 9: no tenant records in the request


def test_findings_are_judged_on_normalised_values():
    paths = ["total_amount", "vat_amount", "seller_trn", "buyer_trn", "issue_date"]
    trn = INV.seller_trn
    parsed = CriticOutput(reviews=[
        CriticReview(path="total_amount", document_value=f"AED {INV.total_amount}", matches=False),  # equal
        CriticReview(path="vat_amount", document_value="1.00", matches=True),  # model says match; values differ
        CriticReview(path="seller_trn", document_value=f"{trn[:3]} {trn[3:]}", matches=True),
        CriticReview(path="buyer_trn", document_value="", matches=False),
    ])
    got = {f.path: f for f in InvoiceCritic.findings(OUT, paths, parsed)}
    assert set(got) == {"vat_amount", "buyer_trn", "issue_date"}
    assert got["vat_amount"].code == "critic.mismatch" and got["vat_amount"].expected == "1.00"
    assert got["vat_amount"].severity == "block" and got["vat_amount"].source == "critic"
    assert got["buyer_trn"].code == "critic.not_found" and got["buyer_trn"].severity == "warn"
    assert got["issue_date"].code == "critic.unreviewed"


def test_default_role_is_the_sonnet_critic():
    c = InvoiceCritic(model=SONNET)
    assert (c.prompt_id, c.effort, c.model) == ("verifier.critic", "medium", SONNET)
