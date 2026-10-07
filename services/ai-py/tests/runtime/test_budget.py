import pytest

from ai.gateway.types import Usage
from ai.runtime.budget import BudgetMeter
from ai.runtime.clock import FakeClock
from ai.runtime.errors import BudgetExceeded
from ai.runtime.types import Budget


def test_llm_call_limit_checked_before_the_call():
    m = BudgetMeter(Budget(max_llm_calls=1), FakeClock())
    m.check_can_call()
    m.add_usage(Usage(llm_calls=1, input_tokens=10))
    with pytest.raises(BudgetExceeded) as e:
        m.check_can_call()
    assert e.value.limit == "llm_calls"


def test_cache_hit_counts_nothing_but_the_hit():
    m = BudgetMeter(Budget(), FakeClock())
    m.add_usage(Usage(response_cache_hit=True))
    t = m.totals
    assert (t.llm_calls, t.response_cache_hits, t.cost_micro_usd, t.input_tokens) == (0, 1, 0, 0)


def test_input_tokens_include_cache_reads_and_writes():
    m = BudgetMeter(Budget(max_input_tokens=100), FakeClock())
    with pytest.raises(BudgetExceeded) as e:
        m.add_usage(Usage(llm_calls=1, input_tokens=40, cache_read_input_tokens=40,
                          cache_creation_input_tokens=40))
    assert e.value.limit == "input_tokens" and m.totals.input_tokens == 120


def test_cost_limit_after_crossing():
    m = BudgetMeter(Budget(max_cost_micro_usd=100), FakeClock())
    m.add_usage(Usage(llm_calls=1, cost_micro_usd=100))
    assert m.remaining_cost_micro_usd == 0
    with pytest.raises(BudgetExceeded):
        m.check_can_call()


def test_steps_and_deadline():
    clock = FakeClock()
    m = BudgetMeter(Budget(max_steps=1, deadline_s=10), clock)
    m.check_step()
    with pytest.raises(BudgetExceeded) as e:
        m.check_step()
    assert e.value.limit == "steps"
    m2 = BudgetMeter(Budget(deadline_s=10), clock)
    clock.advance(10)
    with pytest.raises(BudgetExceeded) as e:
        m2.check_can_call()
    assert e.value.limit == "deadline" and m2.remaining_s() == 0
