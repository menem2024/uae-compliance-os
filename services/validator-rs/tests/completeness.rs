//! The completeness test (E1, AC-1, AC-2): the coverage TSVs account for every one of the 302
//! official PINT-AE 1.0.4 asserts, and every row stands on something checkable.
//!
//! * the official rows are exactly the upstream ids, each once, plus the platform rules;
//! * no row is `pending`;
//! * every `implemented` row has a registered rule, a passing case (no official example makes it
//!   fail) and a failing case (a mutation fixture makes the RuleSet report it);
//! * every `structural` and `upstream_noop` row names a test that exists (`#[test] fn` in the
//!   named source file).
//!
//! [`problems`] holds the logic over plain inputs so that the tests below can show that each kind
//! of gap is reported (the test is not vacuous).

use std::collections::{BTreeSet, HashSet};
use std::path::Path;

use validator_rs::catalog::{
    Catalog, Family, PINT_AE_1_0_4_COVERAGE, PINT_AE_1_0_4_UPSTREAM, Status,
};
use validator_rs::conformance::{self, examples};
use validator_rs::rules;
use validator_rs::ruleset::default_ruleset;

/// Everything the checks look at besides the TSVs.
struct Inputs {
    /// Ids of the rules registered in the engine.
    registered: BTreeSet<String>,
    /// Rule ids reported on at least one official example (a pass case must not fail).
    fired_on_examples: BTreeSet<String>,
    /// Rule ids reported on at least one mutation fixture (the failing cases).
    fired_on_fixtures: BTreeSet<String>,
}

/// `(source file under src/, test function)` of every test a note names, in order of appearance.
///
/// A note names a test as a module path (`rules::lines::tests::some_test`, `export::tests::x`) or
/// as a file path (`src/rules/totals.rs::tests::some_test`); anything else with `::` in a note
/// (`El::push`) is prose.
fn named_tests(note: &str) -> Vec<(String, String)> {
    let token = regex::Regex::new(r"[A-Za-z0-9_./]+(?:::[A-Za-z0-9_]+)+").expect("valid regex");
    let mut out = Vec::new();
    for m in token.find_iter(note) {
        let path = m.as_str();
        let segs: Vec<&str> = path.split("::").collect();
        let (file, name) = if let Some(rest) = segs[0].strip_prefix("src/") {
            if !rest.ends_with(".rs") || !segs.contains(&"tests") {
                continue;
            }
            (format!("src/{rest}"), segs[segs.len() - 1].to_string())
        } else {
            let Some(at) = segs.iter().position(|s| *s == "tests") else {
                continue;
            };
            if at == 0 || at + 2 != segs.len() {
                continue;
            }
            // `rules::lines` is src/rules/lines.rs; `export` is src/export/mod.rs (resolved by
            // `test_exists`, which tries both).
            (
                format!("src/{}", segs[..at].join("/")),
                segs[segs.len() - 1].to_string(),
            )
        };
        out.push((file, name));
    }
    out
}

/// Whether `src/<module>.rs` or `src/<module>/mod.rs` (or the named `.rs` file) holds
/// `#[test] fn <name>`.
fn test_exists(root: &Path, file: &str, name: &str) -> bool {
    let candidates = if file.ends_with(".rs") {
        vec![root.join(file)]
    } else {
        vec![
            root.join(format!("{file}.rs")),
            root.join(file).join("mod.rs"),
        ]
    };
    candidates.iter().any(|p| {
        let Ok(text) = std::fs::read_to_string(p) else {
            return false;
        };
        let lines: Vec<&str> = text.lines().collect();
        lines.iter().enumerate().any(|(i, l)| {
            let t = l.trim_start();
            let declares = t
                .strip_prefix("fn ")
                .is_some_and(|rest| rest.starts_with(name) && rest[name.len()..].starts_with('('));
            declares
                && lines[i.saturating_sub(3)..i]
                    .iter()
                    .any(|a| a.trim() == "#[test]")
        })
    })
}

/// Every gap of the coverage data `(coverage, upstream)` against `inputs`, one line each.
fn problems(
    coverage: &[(Family, &'static str)],
    upstream: &[&'static str],
    inputs: &Inputs,
    src: &Path,
) -> Vec<String> {
    let catalog = match Catalog::parse(coverage, upstream) {
        Ok(c) => c,
        Err(e) => return vec![format!("the coverage TSVs do not parse: {e}")],
    };
    let mut out = Vec::new();

    let official: HashSet<&str> = catalog
        .entries()
        .iter()
        .filter(|e| e.official)
        .map(|e| e.rule_id)
        .collect();
    let upstream_ids: HashSet<&str> = catalog.upstream_ids().collect();
    for id in upstream_ids.difference(&official) {
        out.push(format!("{id}: an official assert with no coverage row"));
    }
    for id in official.difference(&upstream_ids) {
        out.push(format!("{id}: an official row that is no upstream assert"));
    }

    let mut implemented = BTreeSet::new();
    for e in catalog.entries() {
        match e.status {
            Status::Pending => out.push(format!("{}: still pending", e.rule_id)),
            Status::Implemented => {
                implemented.insert(e.rule_id);
                if !inputs.registered.contains(e.rule_id) {
                    out.push(format!(
                        "{}: implemented but no rule is registered",
                        e.rule_id
                    ));
                }
                if !inputs.fired_on_fixtures.contains(e.rule_id) {
                    out.push(format!(
                        "{}: implemented but no fixture makes it fail",
                        e.rule_id
                    ));
                }
                if inputs.fired_on_examples.contains(e.rule_id) {
                    out.push(format!(
                        "{}: fails on an official example (no pass case)",
                        e.rule_id
                    ));
                }
            }
            Status::Structural | Status::UpstreamNoop => {
                let tests = named_tests(e.note);
                if tests.is_empty() {
                    out.push(format!("{}: the note names no test", e.rule_id));
                }
                for (file, name) in tests {
                    if !test_exists(src, &file, &name) {
                        out.push(format!(
                            "{}: the note names {file}::{name}, which is no #[test] fn",
                            e.rule_id
                        ));
                    }
                }
            }
        }
    }
    for id in &inputs.registered {
        if !implemented.contains(id.as_str()) {
            out.push(format!("{id}: registered but its row is not implemented"));
        }
    }
    out
}

fn src_dir() -> std::path::PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).to_path_buf()
}

/// The facts of the real engine and corpus.
fn live_inputs() -> Inputs {
    let rs = default_ruleset();
    let bases = examples();
    let mut fired_on_examples = BTreeSet::new();
    for (_, inv) in &bases {
        fired_on_examples.extend(rs.validate(inv).issues.into_iter().map(|i| i.rule_id));
    }
    let mut fired_on_fixtures = BTreeSet::new();
    for family in Family::ALL {
        for m in conformance::mutations(family).expect("fixtures parse") {
            let inv = m.apply(&bases).expect("fixture applies");
            fired_on_fixtures.extend(rs.validate(&inv).issues.into_iter().map(|i| i.rule_id));
        }
    }
    Inputs {
        registered: rules::all().iter().map(|r| r.id.to_string()).collect(),
        fired_on_examples,
        fired_on_fixtures,
    }
}

fn leak(s: String) -> &'static str {
    s.leak()
}

/// The embedded TSVs with `edit` applied to the text of `family`.
fn edited(family: Family, edit: impl Fn(&str) -> String) -> Vec<(Family, &'static str)> {
    PINT_AE_1_0_4_COVERAGE
        .iter()
        .map(|&(f, text)| (f, if f == family { leak(edit(text)) } else { text }))
        .collect()
}

#[test]
fn the_coverage_is_complete() {
    let inputs = live_inputs();
    let found = problems(
        &PINT_AE_1_0_4_COVERAGE,
        &PINT_AE_1_0_4_UPSTREAM,
        &inputs,
        &src_dir(),
    );
    assert!(
        found.is_empty(),
        "{} problem(s)\n{}",
        found.len(),
        found.join("\n")
    );
}

#[test]
fn there_are_302_official_asserts_each_in_exactly_one_status() {
    let catalog = validator_rs::catalog::pint_ae_1_0_4();
    let upstream: Vec<&str> = PINT_AE_1_0_4_UPSTREAM
        .iter()
        .flat_map(|f| f.lines())
        .map(|l| l.split('\t').next().unwrap())
        .collect();
    assert_eq!(upstream.len(), 302);
    let unique: HashSet<&str> = upstream.iter().copied().collect();
    assert_eq!(unique.len(), 302, "upstream ids are unique");
    let official: Vec<_> = catalog.entries().iter().filter(|e| e.official).collect();
    assert_eq!(official.len(), 302);
    for e in &official {
        assert!(unique.contains(e.rule_id), "{}", e.rule_id);
        assert!(
            matches!(
                e.status,
                Status::Implemented | Status::Structural | Status::UpstreamNoop
            ),
            "{}",
            e.rule_id
        );
    }
    let platform = catalog.entries().iter().filter(|e| !e.official).count();
    assert_eq!(platform, 12);
}

#[test]
fn a_pending_row_fails_the_check() {
    let inputs = live_inputs();
    let cov = edited(Family::Header, |t| {
        t.replacen("\timplemented\t", "\tpending\t", 1)
    });
    let found = problems(&cov, &PINT_AE_1_0_4_UPSTREAM, &inputs, &src_dir());
    assert!(!found.is_empty());
    assert!(found.iter().any(|p| p.contains("pending")), "{found:?}");
}

#[test]
fn a_missing_official_row_fails_the_check() {
    let inputs = live_inputs();
    let cov = edited(Family::Vat, |t| {
        let mut lines = t.lines();
        let header = lines.next().unwrap();
        let rest: Vec<&str> = lines.skip(1).collect();
        format!("{header}\n{}\n", rest.join("\n"))
    });
    let found = problems(&cov, &PINT_AE_1_0_4_UPSTREAM, &inputs, &src_dir());
    assert!(
        found.iter().any(|p| p.contains("no coverage row")),
        "{found:?}"
    );
}

#[test]
fn a_structural_row_without_an_existing_test_fails_the_check() {
    let inputs = live_inputs();
    let src = src_dir();
    // A real test name that does not exist, a note naming nothing, and a real one that exists.
    let swap = |text: &str, to: &str| {
        let mut done = false;
        text.lines()
            .map(|l| {
                if !done && l.contains("\tstructural\t") {
                    done = true;
                    let mut cols: Vec<&str> = l.split('\t').collect();
                    cols[9] = to;
                    cols.join("\t")
                } else {
                    l.to_string()
                }
            })
            .collect::<Vec<_>>()
            .join("\n")
            + "\n"
    };
    let ghost = edited(Family::Header, |t| {
        swap(t, "Proof: rules::header::tests::this_test_does_not_exist.")
    });
    let found = problems(&ghost, &PINT_AE_1_0_4_UPSTREAM, &inputs, &src);
    assert!(
        found.iter().any(|p| p.contains("no #[test] fn")),
        "{found:?}"
    );
    let none = edited(Family::Header, |t| {
        swap(t, "the exporter makes it impossible.")
    });
    let found = problems(&none, &PINT_AE_1_0_4_UPSTREAM, &inputs, &src);
    assert!(
        found.iter().any(|p| p.contains("names no test")),
        "{found:?}"
    );
    let real = edited(Family::Header, |t| {
        swap(
            t,
            "Proof: rules::header::tests::structural_rules_hold_on_every_export.",
        )
    });
    assert!(problems(&real, &PINT_AE_1_0_4_UPSTREAM, &inputs, &src).is_empty());
}

#[test]
fn an_implemented_row_needs_a_rule_a_failing_fixture_and_a_pass_case() {
    let live = live_inputs();
    let src = src_dir();
    let go = |inputs: &Inputs| {
        problems(
            &PINT_AE_1_0_4_COVERAGE,
            &PINT_AE_1_0_4_UPSTREAM,
            inputs,
            &src,
        )
    };

    let mut no_rule = Inputs {
        registered: live.registered.clone(),
        fired_on_examples: live.fired_on_examples.clone(),
        fired_on_fixtures: live.fired_on_fixtures.clone(),
    };
    no_rule.registered.remove("ibr-002");
    assert!(
        go(&no_rule)
            .iter()
            .any(|p| p.starts_with("ibr-002: implemented but no rule"))
    );

    let mut no_fixture = Inputs {
        registered: live.registered.clone(),
        fired_on_examples: live.fired_on_examples.clone(),
        fired_on_fixtures: live.fired_on_fixtures.clone(),
    };
    no_fixture.fired_on_fixtures.remove("ibr-002");
    assert!(
        go(&no_fixture)
            .iter()
            .any(|p| p.starts_with("ibr-002: implemented but no fixture"))
    );

    let mut no_pass = Inputs {
        registered: live.registered.clone(),
        fired_on_examples: live.fired_on_examples.clone(),
        fired_on_fixtures: live.fired_on_fixtures.clone(),
    };
    no_pass.fired_on_examples.insert("ibr-002".into());
    assert!(
        go(&no_pass)
            .iter()
            .any(|p| p.starts_with("ibr-002: fails on an official example"))
    );

    let mut extra = Inputs {
        registered: live.registered.clone(),
        fired_on_examples: live.fired_on_examples.clone(),
        fired_on_fixtures: live.fired_on_fixtures.clone(),
    };
    extra.registered.insert("ibr-001".into()); // a structural row
    assert!(
        go(&extra)
            .iter()
            .any(|p| p.starts_with("ibr-001: registered but its row"))
    );
}

#[test]
fn notes_name_tests_as_module_paths_or_file_paths_and_ignore_prose() {
    assert_eq!(
        named_tests("Proof: rules::header::tests::a_test and export::tests::b_test."),
        [
            ("src/rules/header".to_string(), "a_test".to_string()),
            ("src/export".to_string(), "b_test".to_string())
        ]
    );
    assert_eq!(
        named_tests("test: src/rules/totals.rs::tests::c_test"),
        [("src/rules/totals.rs".to_string(), "c_test".to_string())]
    );
    assert!(named_tests("El::push drops an element; the test counts it").is_empty());
}
