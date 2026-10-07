//! Loads the differential-test corpus from `rulesets/pint-ae-1.0.4/`: the 30 official examples
//! as canonical JSON (`corpus/examples/<slug>.json`, committed), and assembles the whole
//! differential corpus ([`documents`]): examples, mutation fixtures and seeded fuzz documents.

use std::collections::HashSet;
use std::fs;
use std::path::{Path, PathBuf};

use crate::canonical_json::from_canonical_json;
use crate::catalog::Family;
use crate::pb;
use crate::ruleset::RuleSet;

use super::{file_stem, fuzz, mutations};

/// Root of the RuleSet data directory.
pub fn ruleset_dir() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("rulesets/pint-ae-1.0.4")
}

/// Directory of the committed corpus fixtures.
pub fn examples_dir() -> PathBuf {
    ruleset_dir().join("corpus/examples")
}

/// Lower-kebab slug of an official example's file stem:
/// `Standard.invoice.-.Extensive` becomes `standard-invoice-extensive`.
pub fn slug(file_stem: &str) -> String {
    let mut out = String::new();
    let mut pending_dash = false;
    for ch in file_stem.chars() {
        if ch.is_ascii_alphanumeric() {
            if pending_dash && !out.is_empty() {
                out.push('-');
            }
            pending_dash = false;
            out.push(ch.to_ascii_lowercase());
        } else {
            pending_dash = true;
        }
    }
    out
}

/// The 30 official examples as `(slug, invoice)`, sorted by slug.
///
/// # Panics
/// When a fixture file is missing, unreadable or not valid canonical JSON; the corpus is
/// committed data, so that is a broken checkout, not a runtime condition.
pub fn examples() -> Vec<(String, pb::Invoice)> {
    let dir = examples_dir();
    let mut out: Vec<(String, pb::Invoice)> = fs::read_dir(&dir)
        .unwrap_or_else(|e| panic!("cannot read {}: {e}", dir.display()))
        .map(|entry| entry.expect("directory entry").path())
        .filter(|p| p.extension().is_some_and(|x| x == "json"))
        .map(|p| {
            let slug = p
                .file_stem()
                .and_then(|s| s.to_str())
                .expect("utf-8 file name")
                .to_string();
            let text = fs::read_to_string(&p)
                .unwrap_or_else(|e| panic!("cannot read {}: {e}", p.display()));
            let inv = from_canonical_json(&text).unwrap_or_else(|e| panic!("{}: {e}", p.display()));
            (slug, inv)
        })
        .collect();
    out.sort_by(|a, b| a.0.cmp(&b.0));
    out
}

/// A zero-issue synthetic invoice of `lines` lines (at least 1), for the performance gate. It starts
/// from the official `standard-tax-invoice` example, drops the document and line allowances and
/// charges, and gives each line a seeded quantity and price in one standard-rated (5%) category,
/// with every total recomputed exactly (`Decimal`, half away from zero, 2 decimals like the rules).
///
/// # Panics
/// When the example fixture is missing or malformed (a broken checkout).
pub fn synthetic(lines: usize, seed: u64) -> pb::Invoice {
    use rust_decimal::Decimal;
    use rust_decimal::RoundingStrategy::MidpointAwayFromZero;
    use serde_json::{Value, json};

    let lines = lines.max(1);
    let path = examples_dir().join("standard-tax-invoice.json");
    let text =
        fs::read_to_string(&path).unwrap_or_else(|e| panic!("cannot read {}: {e}", path.display()));
    let mut doc: Value =
        serde_json::from_str(&text).unwrap_or_else(|e| panic!("{}: {e}", path.display()));
    let two = |d: Decimal| d.round_dp_with_strategy(2, MidpointAwayFromZero);
    let rate = Decimal::new(5, 2);
    let base = doc["lines"][0].clone();
    let mut out_lines = Vec::with_capacity(lines);
    let mut total = Decimal::ZERO;
    for i in 0..lines as u64 {
        let qty = 1 + ((i + seed) % 7);
        let price = Decimal::from(10) + Decimal::new(((i * 3 + seed) % 13) as i64 * 25, 2);
        let net = two(Decimal::from(qty) * price);
        let vat = two(net * rate);
        let mut l = base.clone();
        let obj = l.as_object_mut().expect("line object");
        obj.remove("allowances_charges");
        obj.insert("id".into(), json!((i + 1).to_string()));
        obj.insert("order_line_reference".into(), json!((i + 1).to_string()));
        obj.insert("quantity".into(), json!(qty.to_string()));
        obj.insert("net_amount".into(), json!(net.to_string()));
        obj.insert(
            "price".into(),
            json!({
                "net_price": price.to_string(),
                "gross_price": price.to_string(),
                "discount": "0",
                "base_quantity": "1",
                "base_quantity_unit_code": "H87",
            }),
        );
        obj.insert("amount_aed".into(), json!((net + vat).to_string()));
        obj.insert("vat_amount_aed".into(), json!(vat.to_string()));
        total += net;
        out_lines.push(l);
    }
    let tax = two(total * rate);
    let obj = doc.as_object_mut().expect("invoice object");
    obj.remove("allowances_charges");
    obj.insert("lines".into(), Value::Array(out_lines));
    obj.insert(
        "totals".into(),
        json!({
            "line_extension_amount": total.to_string(),
            "tax_exclusive_amount": total.to_string(),
            "payable_amount": (total + tax).to_string(),
        }),
    );
    obj.insert("total_amount".into(), json!((total + tax).to_string()));
    obj.insert("vat_amount".into(), json!(tax.to_string()));
    doc["tax_breakdown"][0]["taxable_amount"] = json!(total.to_string());
    doc["tax_breakdown"][0]["tax_amount"] = json!(tax.to_string());
    from_canonical_json(&doc.to_string()).unwrap_or_else(|e| panic!("synthetic invoice: {e}"))
}

/// One document of the differential corpus.
#[derive(Debug, Clone)]
pub struct CorpusDoc {
    /// Path relative to the corpus directory, without `.xml`: `examples/<slug>`,
    /// `mutations/<family>/<fixture>` or `fuzz/fuzz-<n>`.
    pub name: String,
    /// Where the document comes from: the fixture's note, or the fuzz edits.
    pub note: String,
    pub invoice: pb::Invoice,
}

impl CorpusDoc {
    /// The file the XML of this document is written to, relative to the corpus directory.
    pub fn xml_name(&self) -> String {
        format!("{}.xml", self.name)
    }
}

/// Whether `inv` has an `AE-FMT-*` or `AE-EXP-*` issue: it cannot be serialised faithfully and
/// is never exported, so the fuzz corpus leaves it out (spec 5.3.3).
fn is_unserialisable(rs: &RuleSet, inv: &pb::Invoice) -> bool {
    rs.validate(inv)
        .issues
        .iter()
        .any(|i| i.rule_id.starts_with("AE-FMT-") || i.rule_id.starts_with("AE-EXP-"))
}

/// The whole differential corpus, in a fixed order: the 30 examples (by slug), every mutation
/// fixture of every family (family order, file order), then `fuzz` documents of `seed`
/// ([`fuzz::generate`], seeds the fixed SplitMix64 stream) over the 30 examples.
///
/// A mutation fixture is kept even when it carries an `AE-FMT-*` or `AE-EXP-*` expectation: the
/// family authors checked each one against the official schematron, and the CLI skips the few
/// that cannot be serialised at all.
pub fn documents(fuzz_count: usize, seed: u64, rs: &RuleSet) -> Result<Vec<CorpusDoc>, String> {
    let bases = examples();
    let mut out: Vec<CorpusDoc> = bases
        .iter()
        .map(|(slug, inv)| CorpusDoc {
            name: format!("examples/{slug}"),
            note: "official example".into(),
            invoice: inv.clone(),
        })
        .collect();
    for family in Family::ALL {
        let mut stems = HashSet::new();
        for m in mutations(family)? {
            let stem = file_stem(&m.id);
            if !stems.insert(stem.clone()) {
                return Err(format!("{}: file name {stem} is already used", m.id));
            }
            out.push(CorpusDoc {
                name: format!("mutations/{}/{stem}", family.as_str()),
                note: format!("{} on {}: {}", m.id, m.base, m.note),
                invoice: m.apply(&bases)?,
            });
        }
    }
    for doc in fuzz::generate(&bases, fuzz_count, seed, &|inv| !is_unserialisable(rs, inv)) {
        out.push(CorpusDoc {
            name: format!("fuzz/{}", doc.name),
            note: format!("{}: {}", doc.base, doc.edits.join("; ")),
            invoice: doc.invoice,
        });
    }
    if out.iter().filter(|d| d.name.starts_with("fuzz/")).count() != fuzz_count {
        return Err(format!(
            "the fuzz generator did not produce {fuzz_count} documents"
        ));
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;
    use prost::Message;

    #[test]
    fn synthetic_invoices_have_no_issues_at_every_size() {
        let rs = crate::ruleset::default_ruleset();
        for n in [1, 10, 100, 500, 1000] {
            let inv = synthetic(n, 20260929);
            assert_eq!(inv.lines.len(), n);
            let run = rs.validate(&inv);
            assert!(
                run.issues.is_empty(),
                "{n} lines: {:?}",
                run.issues.iter().map(|i| &i.rule_id).collect::<Vec<_>>()
            );
        }
        assert_eq!(
            synthetic(10, 7).encode_to_vec(),
            synthetic(10, 7).encode_to_vec()
        );
    }

    use crate::canonical_json::to_canonical_json_pretty;
    use crate::conformance::ubl_import::from_xml;

    #[test]
    fn slug_is_lower_kebab() {
        assert_eq!(
            slug("Standard.invoice.-.Extensive"),
            "standard-invoice-extensive"
        );
        assert_eq!(slug("Standard tax invoice"), "standard-tax-invoice");
        assert_eq!(
            slug("Volume-discount-credit-note"),
            "volume-discount-credit-note"
        );
        assert_eq!(
            slug("Doc-level-allowance-O-category"),
            "doc-level-allowance-o-category"
        );
        assert_eq!(slug("  --A..b  "), "a-b");
    }

    /// `(slug, xml path)` of the upstream examples; they are fetched, not committed
    /// (`scripts/fetch-upstream.sh`).
    fn upstream_examples() -> Vec<(String, PathBuf)> {
        let base = ruleset_dir().join("upstream/examples");
        let mut out = Vec::new();
        for kind in ["trn-invoice", "trn-creditnote"] {
            let dir = base.join(kind);
            let rd = fs::read_dir(&dir).unwrap_or_else(|e| {
                panic!(
                    "{}: {e}; run services/validator-rs/scripts/fetch-upstream.sh first",
                    dir.display()
                )
            });
            for e in rd {
                let p = e.unwrap().path();
                if p.extension().is_some_and(|x| x == "xml") {
                    let stem = p.file_stem().unwrap().to_str().unwrap();
                    out.push((slug(stem), p));
                }
            }
        }
        out.sort();
        out
    }

    #[test]
    fn upstream_has_30_examples_with_unique_slugs() {
        let ex = upstream_examples();
        assert_eq!(ex.len(), 30);
        let mut slugs: Vec<_> = ex.iter().map(|e| e.0.clone()).collect();
        slugs.dedup();
        assert_eq!(slugs.len(), 30, "slugs must be unique");
    }

    /// Exact, documented list of what the importer cannot represent in the canonical model.
    /// Everything else in the 30 examples is consumed. Every entry is content that no
    /// PINT-AE assert reads, or that the canonical model has no field for (CI v0.2.1).
    fn known_ignored(slug: &str) -> &'static [&'static str] {
        match slug {
            // Upstream defect 3 (line-level reason) plus the example's second, redundant
            // document-level DiscrepancyResponse (BTAE-03 is a single field; the first one,
            // `VD`, is kept, which is what ibr-055-ae evaluates to the same result as the
            // official schematron) and the free-text Description of the first.
            "volume-discount-credit-note" => &[
                "CreditNote/cac:DiscrepancyResponse/cbc:Description",
                "CreditNote/cac:DiscrepancyResponse",
                "CreditNote/cac:CreditNoteLine/cac:DiscrepancyResponse",
            ],
            // The example writes the billing frequency in InvoicePeriod/cbc:Description
            // instead of DescriptionCode (BTAE-06 binds to DescriptionCode: ibr-005-ae,
            // ibr-160-ae); and a UBL line-level TaxTotal that has no business term.
            "continuous-supplies" => &[
                "Invoice/cac:InvoicePeriod/cbc:Description",
                "Invoice/cac:InvoiceLine/cac:TaxTotal",
            ],
            // UBL line-level TaxTotal (BTAE-08 lives in ItemPriceExtension/TaxTotal).
            "exports" | "exports-predefined-endpoint" => &["Invoice/cac:InvoiceLine/cac:TaxTotal"],
            // ClassifiedTaxCategory/PerUnitAmount: no business term in the model.
            "margin-scheme" | "supply-through-e-commerce" => {
                &["Invoice/cac:InvoiceLine/cac:Item/cac:ClassifiedTaxCategory/cbc:PerUnitAmount"]
            }
            _ => &[],
        }
    }

    #[test]
    fn import_of_every_example_ignores_only_the_documented_content() {
        let mut mismatches = Vec::new();
        for (slug, path) in upstream_examples() {
            let xml = fs::read_to_string(&path).unwrap();
            let (_, report) = from_xml(&xml).unwrap_or_else(|e| panic!("{slug}: {e}"));
            let mut got = report.ignored.clone();
            let mut want: Vec<String> =
                known_ignored(&slug).iter().map(|s| s.to_string()).collect();
            got.sort();
            want.sort();
            if got != want {
                mismatches.push(format!("{slug}: got {got:?}, want {want:?}"));
            }
        }
        assert!(mismatches.is_empty(), "{}", mismatches.join("\n"));
    }

    /// The committed corpus is exactly what the importer produces from the fetched examples.
    /// `UPDATE_CORPUS=1 cargo test conformance::corpus` rewrites the files.
    #[test]
    fn committed_corpus_matches_the_importer() {
        let update = std::env::var("UPDATE_CORPUS").is_ok_and(|v| v == "1");
        let dir = examples_dir();
        if update {
            fs::create_dir_all(&dir).unwrap();
        }
        let mut expected_files = Vec::new();
        for (slug, path) in upstream_examples() {
            let xml = fs::read_to_string(&path).unwrap();
            let (inv, _) = from_xml(&xml).unwrap();
            let json = to_canonical_json_pretty(&inv);
            let target = dir.join(format!("{slug}.json"));
            if update {
                fs::write(&target, &json).unwrap();
            } else {
                let on_disk = fs::read_to_string(&target)
                    .unwrap_or_else(|e| panic!("{}: {e}", target.display()));
                assert_eq!(on_disk, json, "{slug}: corpus file is stale");
            }
            expected_files.push(format!("{slug}.json"));
        }
        expected_files.sort();
        let mut on_disk: Vec<String> = fs::read_dir(&dir)
            .unwrap()
            .map(|e| e.unwrap().file_name().into_string().unwrap())
            .collect();
        on_disk.sort();
        assert_eq!(on_disk, expected_files, "no stray or missing corpus files");
    }

    #[test]
    fn examples_loads_all_30_fixtures() {
        let ex = examples();
        assert_eq!(ex.len(), 30);
        for (slug, inv) in &ex {
            assert!(!inv.invoice_number.is_empty(), "{slug}");
            assert!(!inv.lines.is_empty(), "{slug}");
        }
        assert!(ex.iter().any(|(s, _)| s == "standard-tax-invoice"));
        assert!(ex.iter().any(|(s, _)| s == "volume-discount-credit-note"));
        let credit_notes = ex
            .iter()
            .filter(|(_, i)| matches!(i.invoice_type_code.as_str(), "381" | "81"))
            .count();
        assert_eq!(credit_notes, 3);
    }
}
