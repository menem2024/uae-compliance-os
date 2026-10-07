"""Per-run budget meter (contract section 3.2)."""

from ai.gateway.types import Usage
from ai.runtime.clock import Clock
from ai.runtime.errors import BudgetExceeded
from ai.runtime.types import Budget, MicroUSD, RunTotals


class BudgetMeter:
    """Counts steps, model calls, tokens and cost for one run.

    input_tokens counts every prompt token the provider saw: uncached input plus cache reads and
    cache writes. A gateway response-cache hit adds nothing (usage.llm_calls == 0, cost 0).
    """

    def __init__(self, budget: Budget, clock: Clock) -> None:
        self._budget = budget
        self._clock = clock
        self._started = clock.monotonic()
        self._steps = 0
        self._llm_calls = 0
        self._hits = 0
        self._in = 0
        self._out = 0
        self._cost = 0

    @property
    def budget(self) -> Budget:
        return self._budget

    def elapsed_s(self) -> float:
        return self._clock.monotonic() - self._started

    def remaining_s(self) -> float:
        return max(0.0, self._budget.deadline_s - self.elapsed_s())

    def _check_deadline(self) -> None:
        if self.elapsed_s() >= self._budget.deadline_s:
            raise BudgetExceeded("deadline")

    def check_can_call(self) -> None:
        self._check_deadline()
        b = self._budget
        if self._llm_calls >= b.max_llm_calls:
            raise BudgetExceeded("llm_calls")
        if self._cost >= b.max_cost_micro_usd:
            raise BudgetExceeded("cost")
        if self._in >= b.max_input_tokens:
            raise BudgetExceeded("input_tokens")
        if self._out >= b.max_output_tokens:
            raise BudgetExceeded("output_tokens")

    def check_step(self) -> None:
        """Called before every node attempt; reserves one step."""
        self._check_deadline()
        if self._steps >= self._budget.max_steps:
            raise BudgetExceeded("steps")
        self._steps += 1

    def add_usage(self, usage: Usage) -> None:
        self._llm_calls += usage.llm_calls
        self._hits += 1 if usage.response_cache_hit else 0
        self._in += usage.input_tokens + usage.cache_read_input_tokens + usage.cache_creation_input_tokens
        self._out += usage.output_tokens
        self._cost += usage.cost_micro_usd
        b = self._budget
        if self._cost > b.max_cost_micro_usd:
            raise BudgetExceeded("cost")
        if self._in > b.max_input_tokens:
            raise BudgetExceeded("input_tokens")
        if self._out > b.max_output_tokens:
            raise BudgetExceeded("output_tokens")

    @property
    def totals(self) -> RunTotals:
        return RunTotals(steps=self._steps, llm_calls=self._llm_calls, response_cache_hits=self._hits,
                         input_tokens=self._in, output_tokens=self._out, cost_micro_usd=self._cost)

    @property
    def remaining_cost_micro_usd(self) -> MicroUSD:
        return max(0, self._budget.max_cost_micro_usd - self._cost)
