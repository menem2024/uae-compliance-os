"""Verifier-suite corruptions change exactly one field, deterministically."""

from ai.agents.extraction.schema import flatten
from ai.synthetic.corrupt import CORRUPTIONS, corrupt
from ai.synthetic.generator import generate


def test_each_corruption_changes_exactly_one_field_deterministically():
    seen: set[str] = set()
    for seed in range(200):
        truth = generate(seed, "ar" if seed % 2 else "en").truth
        out, c = corrupt(truth, seed)
        assert (out, c) == corrupt(truth, seed)
        before, after = flatten(truth), flatten(out)
        assert [p for p in before if before[p] != after[p]] == [c.path]
        assert (before[c.path], after[c.path]) == (c.truth_value, c.corrupted_value)
        assert flatten(truth) == before  # the input is untouched
        seen.add(c.code)
    assert seen == set(CORRUPTIONS)
