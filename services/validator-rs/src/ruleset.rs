//! RuleSet registry and run assembly (spec 5.2.6). A RuleSet id is `pint-ae@<spec>+r<N>`;
//! Phase 2 serves exactly one, `pint-ae@1.0.4+r1`, which is also the default.

use std::sync::LazyLock;
use std::time::Instant;

use crate::catalog::{self, Catalog, Entry, Fix, Status};
use crate::doc::Doc;
use crate::pb;
use crate::rule::{Finding, Rule, Sink, path_cmp};
use crate::rules;

/// PINT AE Billing 1.0.4, platform revision 1.
pub const PINT_AE_1_0_4_R1: &str = "pint-ae@1.0.4+r1";

/// The RuleSet used when a request names none.
pub const DEFAULT_RULESET: &str = PINT_AE_1_0_4_R1;

/// A requested `ruleset_version` that is not registered (gRPC `INVALID_ARGUMENT`).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UnknownRuleset(pub String);

impl std::fmt::Display for UnknownRuleset {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "unknown ruleset_version \"{}\"", self.0)
    }
}

impl std::error::Error for UnknownRuleset {}

/// The registered rules of one RuleSet, each paired with its coverage row.
#[derive(Debug)]
pub struct RuleSet {
    id: &'static str,
    catalog: &'static Catalog,
    rules: Vec<(&'static Rule, &'static Entry)>,
}

impl RuleSet {
    /// Pairs `rules` with their rows of `catalog`. Every rule needs an `implemented` row, and
    /// every `implemented` row needs exactly one rule.
    pub fn new(
        id: &'static str,
        catalog: &'static Catalog,
        rules: Vec<&'static Rule>,
    ) -> Result<RuleSet, String> {
        let mut registered = std::collections::HashSet::new();
        let mut paired = Vec::with_capacity(rules.len());
        for rule in rules {
            let entry = catalog
                .get(rule.id)
                .ok_or_else(|| format!("{id}: rule {} has no coverage row", rule.id))?;
            if entry.status != Status::Implemented {
                return Err(format!(
                    "{id}: rule {} is registered but its row is not implemented",
                    rule.id
                ));
            }
            if !registered.insert(rule.id) {
                return Err(format!("{id}: rule {} is registered twice", rule.id));
            }
            paired.push((rule, entry));
        }
        if let Some(e) = catalog
            .entries()
            .iter()
            .find(|e| e.status == Status::Implemented && !registered.contains(e.rule_id))
        {
            return Err(format!(
                "{id}: {} is implemented but not registered",
                e.rule_id
            ));
        }
        Ok(RuleSet {
            id,
            catalog,
            rules: paired,
        })
    }

    pub fn id(&self) -> &'static str {
        self.id
    }

    pub fn catalog(&self) -> &'static Catalog {
        self.catalog
    }

    /// Runs every rule over `inv` and assembles the run: issues rendered through the catalogue
    /// and sorted by (family, rule id, path with numeric indices).
    pub fn validate(&self, inv: &pb::Invoice) -> pb::ValidationRun {
        let start = Instant::now();
        let doc = Doc::new(inv);
        let mut issues: Vec<(&Entry, pb::ValidationIssue)> = Vec::new();
        for &(rule, entry) in &self.rules {
            let mut sink = Sink::new(entry.path);
            (rule.check)(&doc, &mut sink);
            issues.extend(
                sink.into_findings()
                    .into_iter()
                    .map(|f| (entry, issue(entry, f))),
            );
        }
        issues.sort_by(|(ea, a), (eb, b)| {
            ea.family
                .cmp(&eb.family)
                .then_with(|| a.rule_id.cmp(&b.rule_id))
                .then_with(|| path_cmp(&a.path, &b.path))
        });
        pb::ValidationRun {
            ruleset_version: self.id.to_string(),
            issues: issues.into_iter().map(|(_, issue)| issue).collect(),
            duration_us: i64::try_from(start.elapsed().as_micros()).unwrap_or(i64::MAX),
            rules_evaluated: i32::try_from(self.rules.len()).unwrap_or(i32::MAX),
        }
    }
}

/// A finding as an issue: severity, messages and fix kind from the row; path, arguments, term
/// override and suggestion from the finding.
fn issue(entry: &Entry, finding: Finding) -> pb::ValidationIssue {
    debug_assert!(
        finding.suggested_value.is_none() || entry.fix == Fix::Value,
        "{} suggests a value but its row has fix {:?}",
        entry.rule_id,
        entry.fix
    );
    pb::ValidationIssue {
        rule_id: entry.rule_id.to_string(),
        severity: entry.severity as i32,
        message: catalog::render(entry.message_en, &finding.args),
        message_ar: catalog::render(entry.message_ar, &finding.args),
        business_term: finding
            .business_term
            .unwrap_or(entry.business_term)
            .to_string(),
        message_args: finding
            .args
            .into_iter()
            .map(|(k, v)| (k.to_string(), v))
            .collect(),
        fixable: entry.fix != Fix::None,
        suggested_value: finding.suggested_value.unwrap_or_default(),
        path: finding.path,
    }
}

/// Every served RuleSet. Phase 2 has one entry, `pint-ae@1.0.4+r1`.
pub static REGISTRY: LazyLock<Vec<RuleSet>> = LazyLock::new(|| {
    vec![
        RuleSet::new(PINT_AE_1_0_4_R1, catalog::pint_ae_1_0_4(), rules::all())
            .unwrap_or_else(|e| panic!("{PINT_AE_1_0_4_R1} is inconsistent (tested): {e}")),
    ]
});

/// The RuleSet for a request's `ruleset_version`: empty means the default; anything not
/// registered, including the retired `pint-ae@0.0-skeleton`, is an error.
pub fn get(version: &str) -> Result<&'static RuleSet, UnknownRuleset> {
    let wanted = if version.is_empty() {
        DEFAULT_RULESET
    } else {
        version
    };
    REGISTRY
        .iter()
        .find(|rs| rs.id == wanted)
        .ok_or_else(|| UnknownRuleset(version.to_string()))
}

/// The default RuleSet.
pub fn default_ruleset() -> &'static RuleSet {
    get(DEFAULT_RULESET).expect("the default RuleSet is registered")
}

/// Validates `inv` with the RuleSet `version` (empty = default).
pub fn validate(version: &str, inv: &pb::Invoice) -> Result<pb::ValidationRun, UnknownRuleset> {
    Ok(get(version)?.validate(inv))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::catalog::{COVERAGE_HEADER, Family};

    #[test]
    fn the_registry_has_one_default_entry() {
        assert_eq!(REGISTRY.len(), 1);
        assert_eq!(REGISTRY[0].id(), "pint-ae@1.0.4+r1");
        assert_eq!(get("").unwrap().id(), "pint-ae@1.0.4+r1");
        assert_eq!(get("pint-ae@1.0.4+r1").unwrap().id(), "pint-ae@1.0.4+r1");
        assert_eq!(default_ruleset().id(), DEFAULT_RULESET);
    }

    #[test]
    fn unknown_versions_are_rejected_with_the_exact_message() {
        for (v, msg) in [
            (
                "pint-ae@0.0-skeleton",
                r#"unknown ruleset_version "pint-ae@0.0-skeleton""#,
            ),
            (
                "pint-ae@1.0.4+r2",
                r#"unknown ruleset_version "pint-ae@1.0.4+r2""#,
            ),
            (
                "pint-ae@1.0.4",
                r#"unknown ruleset_version "pint-ae@1.0.4""#,
            ),
            (" ", r#"unknown ruleset_version " ""#),
        ] {
            let err = get(v).unwrap_err();
            assert_eq!(err, UnknownRuleset(v.to_string()));
            assert_eq!(err.to_string(), msg);
            assert_eq!(
                validate(v, &pb::Invoice::default())
                    .unwrap_err()
                    .to_string(),
                msg
            );
        }
    }

    #[test]
    fn a_run_names_its_ruleset_and_counts_the_rules_it_ran() {
        let run = validate("", &pb::Invoice::default()).unwrap();
        assert_eq!(run.ruleset_version, "pint-ae@1.0.4+r1");
        assert_eq!(run.rules_evaluated as usize, rules::all().len());
        assert!(run.rules_evaluated >= 2);
        assert!(run.duration_us >= 0);
    }

    #[test]
    fn the_official_examples_have_no_errors() {
        use crate::rules::platform::KNOWN_INVALID_CONTRACT_VALUE;
        for (slug, inv) in crate::conformance::examples() {
            let run = default_ruleset().validate(&inv);
            let errors: Vec<_> = run
                .issues
                .iter()
                .filter(|i| i.severity == pb::Severity::Error as i32)
                .map(|i| (i.rule_id.as_str(), i.path.as_str()))
                .collect();
            let want: &[(&str, &str)] = if KNOWN_INVALID_CONTRACT_VALUE.contains(&slug.as_str()) {
                &[("AE-FMT-001", "references.contract_value")]
            } else {
                &[]
            };
            assert_eq!(errors, want, "{slug}");
        }
    }

    // A synthetic RuleSet pins assembly independently of the real families.

    const UPSTREAM: &str = "ibr-001\tfatal\t/ubl:Invoice\tx\t[ibr-001]-An Invoice shall have a Specification identifier (IBT-024).\n";

    fn coverage(family: Family, rows: &[&str]) -> (Family, &'static str) {
        let mut out = format!("{COVERAGE_HEADER}\n");
        for row in rows {
            out.push_str(row);
            out.push('\n');
        }
        (family, out.leak())
    }

    fn test_catalog() -> &'static Catalog {
        static C: LazyLock<Catalog> = LazyLock::new(|| {
            let files: Vec<(Family, &'static str)> = Family::ALL
                .into_iter()
                .map(|f| match f {
                    Family::Header => coverage(f, &[
                        "ibr-001\theader\timplemented\terror\tIBT-024\tprocess.specification_identifier\tnone\t\tيجب\tn",
                    ]),
                    Family::Platform => coverage(f, &[
                        "AE-TST-002\tplatform\timplemented\twarning\tIBT-106\ttotals.line_extension_amount\tvalue\tExpected {expected}.\tالمتوقع {expected}.\t",
                        "AE-TST-001\tplatform\timplemented\terror\tIBT-126\tlines[#].id\tnone\tLine id.\tمعرّف السطر.\t",
                        "AE-TST-003\tplatform\tstructural\terror\t\t\tnone\tNever.\tأبداً.\tproved by a test",
                    ]),
                    _ => coverage(f, &[]),
                })
                .collect();
            Catalog::parse(&files, &[UPSTREAM]).unwrap()
        });
        &C
    }

    fn official(_: &Doc<'_>, sink: &mut Sink<'_>) {
        sink.fail(&[]);
    }

    fn lines(doc: &Doc<'_>, sink: &mut Sink<'_>) {
        for i in (0..doc.lines.len()).rev() {
            sink.fail(&[i]).term("IBT-127");
        }
    }

    fn totals(_: &Doc<'_>, sink: &mut Sink<'_>) {
        sink.fail(&[]).arg("expected", "1050.00").suggest("1050.00");
    }

    static TEST_RULES: &[Rule] = &[
        Rule {
            id: "AE-TST-002",
            check: totals,
        },
        Rule {
            id: "AE-TST-001",
            check: lines,
        },
        Rule {
            id: "ibr-001",
            check: official,
        },
    ];

    fn test_ruleset() -> RuleSet {
        RuleSet::new("test@1+r1", test_catalog(), TEST_RULES.iter().collect()).unwrap()
    }

    #[test]
    fn issues_are_rendered_through_the_catalogue_and_sorted() {
        let inv = pb::Invoice {
            lines: (0..12).map(|_| pb::InvoiceLine::default()).collect(),
            ..Default::default()
        };
        let run = test_ruleset().validate(&inv);
        assert_eq!(run.ruleset_version, "test@1+r1");
        assert_eq!(run.rules_evaluated, 3);

        let order: Vec<_> = run
            .issues
            .iter()
            .map(|i| (i.rule_id.as_str(), i.path.as_str()))
            .collect();
        let mut want = vec![("ibr-001", "process.specification_identifier".to_string())];
        want.extend((0..12).map(|i| ("AE-TST-001", format!("lines[{i}].id"))));
        want.push(("AE-TST-002", "totals.line_extension_amount".to_string()));
        let want: Vec<_> = want.iter().map(|(r, p)| (*r, p.as_str())).collect();
        assert_eq!(order, want);

        let official = &run.issues[0];
        assert_eq!(official.severity, pb::Severity::Error as i32);
        assert_eq!(official.business_term, "IBT-024");
        assert_eq!(
            official.message,
            "An Invoice shall have a Specification identifier (IBT-024)."
        );
        assert_eq!(official.message_ar, "يجب");
        assert!(
            !official.fixable
                && official.message_args.is_empty()
                && official.suggested_value.is_empty()
        );

        let line = &run.issues[3];
        assert_eq!(line.path, "lines[2].id");
        assert_eq!(
            line.business_term, "IBT-127",
            "Finding::term overrides the row"
        );
        assert_eq!(line.message, "Line id.");

        let totals = run.issues.last().unwrap();
        assert_eq!(totals.severity, pb::Severity::Warning as i32);
        assert_eq!(totals.business_term, "IBT-106");
        assert_eq!(totals.message, "Expected 1050.00.");
        assert_eq!(totals.message_ar, "المتوقع 1050.00.");
        assert_eq!(
            totals.message_args.get("expected").map(String::as_str),
            Some("1050.00")
        );
        assert_eq!(totals.message_args.len(), 1);
        assert!(totals.fixable);
        assert_eq!(totals.suggested_value, "1050.00");
    }

    #[test]
    fn a_clean_document_has_no_issues() {
        static ONLY_LINES: &[Rule] = &[Rule {
            id: "AE-TST-001",
            check: lines,
        }];
        let mut catalog_rules: Vec<&'static Rule> = ONLY_LINES.iter().collect();
        catalog_rules.extend(TEST_RULES.iter().filter(|r| r.id != "AE-TST-001"));
        let rs = RuleSet::new("t", test_catalog(), catalog_rules).unwrap();
        let run = rs.validate(&pb::Invoice::default());
        assert_eq!(
            run.issues.len(),
            2,
            "only the two document-level rules fire"
        );
    }

    #[test]
    fn new_rejects_an_inconsistent_registration() {
        fn noop(_: &Doc<'_>, _: &mut Sink<'_>) {}
        static UNKNOWN: Rule = Rule {
            id: "AE-TST-009",
            check: noop,
        };
        static STRUCTURAL: Rule = Rule {
            id: "AE-TST-003",
            check: noop,
        };
        let all: Vec<&'static Rule> = TEST_RULES.iter().collect();

        let mut extra = all.clone();
        extra.push(&UNKNOWN);
        assert!(
            RuleSet::new("t", test_catalog(), extra).is_err(),
            "rule without a row"
        );

        let mut not_implemented = all.clone();
        not_implemented.push(&STRUCTURAL);
        assert!(
            RuleSet::new("t", test_catalog(), not_implemented).is_err(),
            "row not implemented"
        );

        let mut twice = all.clone();
        twice.push(&TEST_RULES[0]);
        assert!(
            RuleSet::new("t", test_catalog(), twice).is_err(),
            "registered twice"
        );

        assert!(
            RuleSet::new("t", test_catalog(), all[1..].to_vec()).is_err(),
            "row without rule"
        );
    }
}
