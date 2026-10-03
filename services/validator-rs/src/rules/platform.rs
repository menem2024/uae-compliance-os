//! Platform rules (`AE-*`, spec 5.2.7): conditions the official schematron does not check but
//! the platform needs, because the canonical model can express them and the exporter cannot
//! write them. Their messages, severity and fix kind are in `coverage/platform.tsv`; their
//! failing fixtures are in `mutations/platform.jsonl`.

use crate::doc::Doc;
use crate::rule::{Rule, Sink, fill};

/// Prefix of the out-of-scope self-billing specification (CI rule 9).
pub const SELFBILLING_SPECIFICATION_PREFIX: &str = "urn:peppol:pint:selfbilling-1@ae-1";

pub static RULES: &[Rule] = &[
    Rule {
        id: "AE-FMT-001",
        check: ae_fmt_001,
    },
    Rule {
        id: "AE-SCOPE-001",
        check: ae_scope_001,
    },
];

/// `AE-FMT-001` (error, fix `llm`): a decimal field breaks the contract rule 10 grammar
/// (spec 5.2.2). One finding per non-empty field of [`Doc::DECIMAL_FIELDS`] that fails
/// `decimal::parse`, at that field's path, with that field's business term as both the issue's
/// term and `message_args.term` (F7). Every other rule sees such a field as absent.
fn ae_fmt_001(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    doc.each_decimal(|field, idx, term, dec| {
        if dec.is_invalid() {
            sink.fail_at(fill(field.path, idx))
                .term(term)
                .arg("term", term);
        }
    });
}

/// `AE-SCOPE-001` (error, fix `none`): `process.specification_identifier`, trimmed and with the
/// exporter's default, starts with `urn:peppol:pint:selfbilling-1@ae-1`. Self-billing is out of
/// scope (CI rule 9) although the official `aligned-ibrp-001-ae` accepts it.
fn ae_scope_001(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if doc
        .specification_identifier()
        .starts_with(SELFBILLING_SPECIFICATION_PREFIX)
    {
        sink.fail(&[]);
    }
}

/// Corpus examples whose imported BTAE-05 is not the decimal string the contract requires, so
/// `AE-FMT-001` fires on `references.contract_value`. Empty: the importer strips the currency
/// prefix of the official text (`AED200000`, `AED 1000000`), so every example passes.
#[cfg(test)]
pub(crate) const KNOWN_INVALID_CONTRACT_VALUE: &[&str] = &[];

#[cfg(test)]
mod tests {
    use super::*;
    use crate::conformance::{apply_patch, examples};
    use crate::pb;
    use crate::rule::Finding;
    use serde_json::{Map, Value};

    fn run(rule_id: &str, inv: &pb::Invoice) -> Vec<Finding> {
        let rule = RULES.iter().find(|r| r.id == rule_id).unwrap();
        let template = crate::catalog::pint_ae_1_0_4()
            .get(rule_id)
            .map(|e| e.path)
            .unwrap();
        let doc = Doc::new(inv);
        let mut sink = Sink::new(template);
        (rule.check)(&doc, &mut sink);
        sink.into_findings()
    }

    fn invoice(set: &[(&str, &str)]) -> pb::Invoice {
        let mut inv = pb::Invoice::default();
        let map: Map<String, Value> = set
            .iter()
            .map(|(k, v)| ((*k).to_string(), Value::String((*v).to_string())))
            .collect();
        apply_patch(&mut inv, &map, &[]).unwrap();
        inv
    }

    #[test]
    fn the_official_examples_pass_every_platform_rule() {
        assert_eq!(KNOWN_INVALID_CONTRACT_VALUE, &[] as &[&str]);
        for (slug, inv) in examples() {
            for rule in RULES {
                let paths: Vec<String> = run(rule.id, &inv).into_iter().map(|f| f.path).collect();
                assert_eq!(paths, Vec::<String>::new(), "{} on {slug}", rule.id);
            }
        }
    }

    /// The two examples that write BTAE-05 with a currency prefix import as bare decimals.
    #[test]
    fn the_examples_contract_values_are_decimal_strings() {
        let ex = examples();
        for (slug, want) in [
            ("continuous-supplies", "1000000"),
            ("standard-invoice-extensive", "200000"),
        ] {
            let (_, inv) = ex.iter().find(|(s, _)| s == slug).unwrap();
            let got = &inv.references.as_ref().unwrap().contract_value;
            assert_eq!(got, want, "{slug}");
        }
    }

    #[test]
    fn ae_fmt_001_reports_each_invalid_field_with_its_term() {
        let inv = invoice(&[
            ("total_amount", "1,050.00"),
            ("vat_amount", " 50.00 "),
            ("exchange_rate", "3.6725"),
            ("tax_breakdown[0].tax_amount", "٥٠"),
            ("tax_breakdown[0].taxable_amount", "1000"),
            ("lines[0].net_amount", "1e3"),
            ("lines[0].price.net_price", " "),
            ("lines[0].allowances_charges[0].amount", "AED 5"),
        ]);
        let mut inv = inv;
        inv.lines[0].allowances_charges[0].is_charge = true;
        let got: Vec<_> = run("AE-FMT-001", &inv)
            .into_iter()
            .map(|f| (f.path, f.business_term, f.args))
            .collect();
        let want = |path: &str, term: &'static str| {
            (
                path.to_string(),
                Some(term),
                vec![("term", term.to_string())],
            )
        };
        assert_eq!(
            got,
            [
                want("total_amount", "IBT-112"),
                want("tax_breakdown[0].tax_amount", "IBT-117"),
                want("lines[0].net_amount", "IBT-131"),
                want("lines[0].allowances_charges[0].amount", "IBT-141"),
            ]
        );
    }

    #[test]
    fn ae_fmt_001_covers_every_decimal_field() {
        let mut set = Vec::new();
        for f in Doc::DECIMAL_FIELDS {
            let combos: Vec<Vec<usize>> = match f.path.matches('#').count() {
                0 => vec![vec![]],
                1 => vec![vec![0], vec![1]],
                _ => vec![vec![0, 0], vec![0, 1], vec![1, 0], vec![1, 1]],
            };
            for (n, idx) in combos.iter().enumerate() {
                let bad = if n % 2 == 0 { "1.0.0" } else { "+-1" };
                set.push((fill(f.path, idx), bad.to_string()));
            }
        }
        set.sort();
        let pairs: Vec<(&str, &str)> = set.iter().map(|(k, v)| (k.as_str(), v.as_str())).collect();
        let inv = invoice(&pairs);
        let mut got: Vec<String> = run("AE-FMT-001", &inv)
            .into_iter()
            .map(|f| f.path)
            .collect();
        got.sort();
        let want: Vec<String> = set.into_iter().map(|(k, _)| k).collect();
        assert_eq!(got, want);
    }

    #[test]
    fn ae_fmt_001_accepts_valid_and_absent_values() {
        let inv = invoice(&[
            ("total_amount", " 1050.00 "),
            ("vat_amount", "-0.5"),
            ("exchange_rate", "+3.672500"),
            ("totals.paid_amount", "\t"),
        ]);
        assert_eq!(run("AE-FMT-001", &inv), []);
        assert_eq!(run("AE-FMT-001", &pb::Invoice::default()), []);
    }

    #[test]
    fn ae_scope_001_rejects_self_billing() {
        for spec in [
            "urn:peppol:pint:selfbilling-1@ae-1",
            " urn:peppol:pint:selfbilling-1@ae-1 ",
            "urn:peppol:pint:selfbilling-1@ae-1#conformant#urn:x",
        ] {
            let inv = invoice(&[("process.specification_identifier", spec)]);
            let got = run("AE-SCOPE-001", &inv);
            assert_eq!(got.len(), 1, "{spec:?}");
            assert_eq!(got[0].path, "process.specification_identifier");
            assert_eq!(got[0].business_term, None);
            assert!(got[0].args.is_empty() && got[0].suggested_value.is_none());
        }
        for spec in [
            "",
            "urn:peppol:pint:billing-1@ae-1",
            "URN:PEPPOL:PINT:SELFBILLING-1@AE-1",
            "x urn:peppol:pint:selfbilling-1@ae-1",
            "urn:peppol:pint:selfbilling-1",
        ] {
            let inv = invoice(&[("process.specification_identifier", spec)]);
            assert_eq!(run("AE-SCOPE-001", &inv), [], "{spec:?}");
        }
        assert_eq!(run("AE-SCOPE-001", &pb::Invoice::default()), []);
    }

    /// The failing fixtures of `mutations/platform.jsonl` (spec 5.2.8). `expect` is the complete
    /// multiset the full RuleSet must report; until the generic family harness (Task 9) checks it
    /// exactly, this test checks the platform part, which the official families cannot change.
    #[test]
    fn platform_mutation_fixtures_fail_as_expected() {
        let path = crate::conformance::corpus::ruleset_dir().join("mutations/platform.jsonl");
        let text = std::fs::read_to_string(&path).unwrap();
        let bases = examples();
        let mut fired = std::collections::BTreeSet::new();
        let mut count = 0;
        for line in text.lines() {
            let m: Value = serde_json::from_str(line).unwrap();
            let id = m["id"].as_str().unwrap();
            let base = m["base"].as_str().unwrap();
            let set = m["set"].as_object().unwrap();
            let remove: Vec<String> = m["remove"]
                .as_array()
                .unwrap()
                .iter()
                .map(|v| v.as_str().unwrap().to_string())
                .collect();
            let mut expect: Vec<&str> = m["expect"]
                .as_array()
                .unwrap()
                .iter()
                .map(|v| v.as_str().unwrap())
                .filter(|r| r.starts_with("AE-"))
                .collect();
            assert!(
                m["note"].as_str().is_some_and(|n| !n.is_empty()),
                "{id}: note"
            );
            let (_, inv) = bases
                .iter()
                .find(|(slug, _)| slug == base)
                .unwrap_or_else(|| panic!("{id}: unknown base {base}"));
            let mut inv = inv.clone();
            apply_patch(&mut inv, set, &remove).unwrap();
            let run = crate::ruleset::default_ruleset().validate(&inv);
            let mut got: Vec<&str> = run
                .issues
                .iter()
                .map(|i| i.rule_id.as_str())
                .filter(|r| r.starts_with("AE-"))
                .collect();
            got.sort_unstable();
            expect.sort_unstable();
            assert_eq!(got, expect, "{id}");
            fired.extend(got.iter().map(|r| r.to_string()));
            count += 1;
        }
        assert!(count >= 2, "platform.jsonl has {count} fixtures");
        for rule in RULES {
            assert!(
                fired.contains(rule.id),
                "{} has no failing fixture",
                rule.id
            );
        }
    }
}
