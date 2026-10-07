//! The golden snapshot test (spec 5.2.4): the live results on the differential corpus (30
//! examples, every mutation fixture, 2,000 fuzz documents of seed 20260929) equal the frozen
//! `rulesets/pint-ae-1.0.4/snapshot/r1.jsonl`.
//!
//! A failure here means the RuleSet's behaviour changed. The fix is never to edit `r1.jsonl`: a
//! behaviour change bumps the revision (`pint-ae@1.0.4+r2`) and writes `snapshot/r2.jsonl` with
//! `cargo run --bin conformance -- snapshot --write`.

use validator_rs::conformance::fuzz::{DEFAULT_COUNT, DEFAULT_SEED};
use validator_rs::conformance::snapshot;
use validator_rs::ruleset::default_ruleset;

#[test]
fn live_results_equal_the_frozen_snapshot() {
    let rs = default_ruleset();
    let path = snapshot::path(rs).unwrap_or_else(|e| panic!("{e}"));
    let frozen = std::fs::read_to_string(&path).unwrap_or_else(|e| {
        panic!(
            "{}: {e}; write it once with `conformance snapshot --write`",
            path.display()
        )
    });
    let live = snapshot::render_default(rs).unwrap_or_else(|e| panic!("{e}"));
    snapshot::compare(&frozen, &live).unwrap_or_else(|e| panic!("{e}"));
}

#[test]
fn the_snapshot_covers_30_examples_every_fixture_and_the_fuzz_corpus() {
    let rs = default_ruleset();
    let frozen = std::fs::read_to_string(snapshot::path(rs).unwrap()).unwrap();
    let docs: Vec<&str> = frozen.lines().collect();
    let in_group = |p: &str| {
        docs.iter()
            .filter(|l| l.starts_with(&format!("{{\"doc\":\"{p}")))
            .count()
    };
    assert_eq!(in_group("examples/"), 30);
    assert_eq!(in_group("fuzz/"), DEFAULT_COUNT);
    let fixtures: usize = validator_rs::catalog::Family::ALL
        .iter()
        .map(|f| validator_rs::conformance::mutations(*f).unwrap().len())
        .sum();
    assert_eq!(in_group("mutations/"), fixtures);
    assert_eq!(DEFAULT_SEED, 20_260_929);
    // The 30 official examples are clean, so their frozen lines are empty.
    assert!(
        docs.iter()
            .filter(|l| l.contains("\"doc\":\"examples/"))
            .all(|l| l.ends_with("\"issues\":[]}"))
    );
}

#[test]
fn a_changed_result_is_reported_as_a_difference() {
    let rs = default_ruleset();
    let frozen = std::fs::read_to_string(snapshot::path(rs).unwrap()).unwrap();
    // Drop one issue from the first document that has any: the comparison must fail on it.
    let first = frozen
        .lines()
        .find(|l| !l.ends_with("\"issues\":[]}"))
        .expect("some document has issues");
    let tampered = frozen.replacen(first, &first.replace("\"error\"", "\"warning\""), 1);
    let err = snapshot::compare(&tampered, &frozen).unwrap_err();
    assert!(err.contains("snapshot differs"), "{err}");
}
