"""values_equal: the spec section 5.6 comparison shared by the critic and the eval scorers."""

import pytest

from ai.agents.extraction.compare import values_equal


@pytest.mark.parametrize(("path", "a", "b", "equal"), [
    ("total_amount", "1,050.00", "1050", True),
    ("lines[2].tax.rate", "5%", "5.00", True),
    ("total_amount", "١٠٥٠٫٠٠", "1050.00", True),
    ("total_amount", "1050.01", "1050.00", False),
    ("issue_date", "15/03/2026", "2026-03-15", True),
    ("issue_date", "2026-03-15", "2026-15-03", False),
    ("seller_trn", "100 123 456 789 012", "100123456789012", True),
    ("currency", "aed", "AED", True),
    ("seller.name", "شركة  الأمل", "شركة الامل", True),
    ("buyer.name", "Sunrise Hotels LLC", "sunrise hotels llc", True),
    ("buyer_trn", "", "", True),
    ("buyer_trn", "100123456789012", "", False),
])
def test_values_equal(path, a, b, equal):
    assert values_equal(path, a, b) is equal
