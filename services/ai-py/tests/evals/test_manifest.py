from pathlib import Path

from ai.evals.manifest import Manifest, load_manifest, matches_current, prompt_versions, write_manifest


def test_missing_manifest_does_not_match(tmp_path: Path):
    assert load_manifest(tmp_path, "extraction") is None
    assert not matches_current(None, {"extraction.invoice": 1})


def test_stale_prompt_versions_do_not_match():
    m = Manifest("extraction", {"extraction.invoice": 1}, "2026-01-01T00:00:00+00:00")
    assert matches_current(m, {"extraction.invoice": 1})
    assert not matches_current(m, {"extraction.invoice": 2})
    assert not matches_current(m, {"extraction.invoice": 1, "extraction.revise": 1})
    assert not matches_current(m, {})


def test_write_then_load_round_trips(tmp_path: Path):
    write_manifest(tmp_path, "intake", {"intake.classify": 3})
    m = load_manifest(tmp_path, "intake")
    assert m is not None and (m.suite, m.prompt_versions) == ("intake", {"intake.classify": 3})
    assert m.recorded_at
    assert (tmp_path / "intake" / "MANIFEST.json").is_file()


def test_garbage_manifest_is_treated_as_missing(tmp_path: Path):
    (tmp_path / "x").mkdir()
    (tmp_path / "x" / "MANIFEST.json").write_text("{not json")
    assert load_manifest(tmp_path, "x") is None


def test_current_versions_come_from_the_agent_prompt_modules():
    assert prompt_versions(("extraction.invoice", "intake.classify", "verifier.critic")) == {
        "extraction.invoice": 1, "intake.classify": 1, "verifier.critic": 1}
    assert prompt_versions(()) == {}
