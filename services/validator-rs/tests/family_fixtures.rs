//! Generic per-family fixture harness (spec 5.2.8), reused by the family tasks: a family adds its
//! rules, its coverage rows and `rulesets/pint-ae-1.0.4/mutations/<family>.jsonl`; the test of
//! that family below then checks
//!
//! * **pass cases**: no issue of the family's rules on any of the 30 official examples;
//! * **fail cases**: every fixture of the family's JSONL, applied to its base, makes the RuleSet
//!   report exactly the registered part of its `expect` multiset (`Mutation::check`), and the
//!   fixture's own rule (`<rule id>#<n>`) is a rule of this family;
//! * every registered rule of the family is in the `expect` list of at least one of its fixtures.
//!
//! The same fixtures are exported by `conformance mutations --family <f>` and compared with the
//! official schematron (`compare.py`), so every `expect` list is also checked against Saxon.

use std::collections::BTreeSet;

use validator_rs::catalog::{Family, Status};
use validator_rs::conformance::{self, Mutation};
use validator_rs::ruleset::{self, RuleSet};

/// Runs the pass and fail cases of `family` with the default RuleSet; panics listing every
/// problem.
pub fn run_family(family: &str) {
    let f = Family::parse(family).unwrap_or_else(|| panic!("unknown family {family:?}"));
    let fixtures = conformance::mutations(f).unwrap_or_else(|e| panic!("{e}"));
    let problems = check_family(f, &fixtures, ruleset::default_ruleset());
    assert!(
        problems.is_empty(),
        "{family}: {} problem(s)\n{}",
        problems.len(),
        problems.join("\n")
    );
}

/// Every problem of `fixtures` as the cases of family `f` under `rs`.
fn check_family(f: Family, fixtures: &[Mutation], rs: &RuleSet) -> Vec<String> {
    let family_of = |id: &str| rs.catalog().get(id).map(|e| e.family);
    let bases = conformance::examples();
    let mut problems = Vec::new();

    for (slug, inv) in &bases {
        for issue in rs.validate(inv).issues {
            if family_of(&issue.rule_id) == Some(f) {
                problems.push(format!(
                    "pass case {slug}: {} at {}",
                    issue.rule_id, issue.path
                ));
            }
        }
    }

    let mut expected = BTreeSet::new();
    for m in fixtures {
        let own = m.id.split('#').next().unwrap_or_default();
        if family_of(own) != Some(f) {
            problems.push(format!("{}: {own} is not a {} rule", m.id, f.as_str()));
        }
        if let Err(e) = m.apply(&bases).and_then(|inv| m.check(&inv, rs)) {
            problems.push(e);
        }
        expected.extend(m.expect.iter().map(String::as_str));
    }

    for e in rs.catalog().entries() {
        if e.family == f && e.status == Status::Implemented && !expected.contains(e.rule_id) {
            problems.push(format!("{} has no failing fixture", e.rule_id));
        }
    }
    problems
}

#[test]
fn header() {
    run_family("header");
}

#[test]
fn parties() {
    run_family("parties");
}

#[test]
fn lines() {
    run_family("lines");
}

#[test]
fn totals() {
    run_family("totals");
}

#[test]
fn vat() {
    run_family("vat");
}

#[test]
fn codelists() {
    run_family("codelists");
}

#[test]
fn platform() {
    run_family("platform");
}

/// The harness itself: a wrong multiset, a fixture filed under another family and a registered
/// rule without a failing fixture are each reported.
#[test]
fn the_harness_reports_every_kind_of_problem() {
    let rs = ruleset::default_ruleset();
    let fixtures = conformance::mutations(Family::Platform).unwrap();
    assert!(
        fixtures.len() >= 20,
        "platform has {} fixtures",
        fixtures.len()
    );
    assert_eq!(
        check_family(Family::Platform, &fixtures, rs),
        Vec::<String>::new()
    );

    let mut wrong = fixtures.clone();
    let i = wrong.iter().position(|m| m.id == "AE-EXP-003#1").unwrap();
    wrong[i].expect.push("AE-EXP-003".into());
    let problems = check_family(Family::Platform, &wrong, rs);
    assert_eq!(problems.len(), 1, "{problems:?}");
    assert!(
        problems[0].starts_with("AE-EXP-003#1: rule ids"),
        "{problems:?}"
    );

    let foreign = check_family(Family::Header, &fixtures[..1], rs);
    assert!(
        foreign.iter().any(|p| p.contains("is not a header rule")),
        "{foreign:?}"
    );

    let without: Vec<Mutation> = fixtures
        .iter()
        .filter(|m| !m.expect.iter().any(|e| e == "AE-EXP-010"))
        .cloned()
        .collect();
    assert_eq!(
        check_family(Family::Platform, &without, rs),
        ["AE-EXP-010 has no failing fixture"]
    );
}
