from ai.gateway.redact import redact


def test_masks_email_phone_iban_card():
    s = "mail a.b@x.ae call +971 50 123 4567 or 0501234567 iban AE070331234567890123456 card 4111 1111 1111 1111"
    out = redact(s)
    assert "a.b@x.ae" not in out and "[email]" in out
    assert "123 4567" not in out and out.count("[phone]") == 2
    assert "AE070331234567890123456" not in out and "[iban]" in out
    assert "4111" not in out and "[card]" in out


def test_keeps_trn_and_amounts():
    s = "TRN 100234567800003 total 1050.00 invoice INV-7"
    assert redact(s) == s
