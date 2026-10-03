from dataclasses import replace
from pathlib import Path

from ai.evals.cli import Threshold, exit_code, failures, load_thresholds
from ai.evals.runner import Report

TOML = Path(__file__).resolve().parents[2] / "evals" / "thresholds.toml"
TAGS = ("lang:ar", "lang:en", "format:pdf", "format:image")


def test_thresholds_toml_is_exactly_the_six_values_of_spec_5_6():
    assert load_thresholds(TOML) == [
        Threshold("extraction", "field_accuracy", ">=", 0.90),
        Threshold("extraction", "field_accuracy", ">=", 0.85, TAGS),
        Threshold("intake", "kind_accuracy", ">=", 0.95),
        Threshold("importer", "field_accuracy", "==", 1.0),
        Threshold("verifier", "catch_rate", ">=", 0.80),
        Threshold("verifier", "false_flag_rate", "<=", 0.10),
    ]


def _extraction(total: float = 0.95, tag: float = 0.95) -> Report:
    return Report("extraction", "fake", "full", {"field_accuracy": total},
                  {t: {"field_accuracy": tag} for t in TAGS}, 200, False)


def test_a_report_just_under_a_threshold_fails_the_exit_code():
    th = load_thresholds(TOML)
    assert exit_code([_extraction()], th) == 0
    assert exit_code([_extraction(total=0.90)], th) == 0  # the threshold itself passes
    assert exit_code([_extraction(total=0.8999)], th) == 1
    assert exit_code([_extraction(tag=0.8499)], th) == 1
    assert any("[lang:ar]" not in f and "total" in f for f in failures(_extraction(total=0.8), th))


def test_importer_must_be_exact_and_verifier_false_flags_are_bounded():
    th = load_thresholds(TOML)
    imp = Report("importer", "fake", "full", {"field_accuracy": 1.0}, {}, 40, False)
    assert exit_code([imp], th) == 0
    assert exit_code([replace(imp, totals={"field_accuracy": 0.9999})], th) == 1
    ver = Report("verifier", "fake", "full", {"catch_rate": 0.9, "false_flag_rate": 0.10}, {}, 200, False)
    assert exit_code([ver], th) == 0
    assert exit_code([replace(ver, totals={"catch_rate": 0.9, "false_flag_rate": 0.11})], th) == 1


def test_a_missing_metric_or_tag_fails_and_a_skipped_report_is_ignored():
    th = load_thresholds(TOML)
    assert exit_code([replace(_extraction(), per_tag={})], th) == 1
    assert exit_code([replace(_extraction(), totals={})], th) == 1
    assert exit_code([Report("extraction", "fake", "full", {}, {}, 0, False, status="skipped")], th) == 0


def test_missing_recordings_fail_replay():
    th = load_thresholds(TOML)
    assert exit_code([replace(_extraction(), mode="replay", missing_recordings=3)], th) == 1
