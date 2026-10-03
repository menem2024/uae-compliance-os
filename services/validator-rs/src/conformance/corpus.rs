//! Loads the differential-test corpus from `rulesets/pint-ae-1.0.4/`: the 30 official examples
//! as canonical JSON (`corpus/examples/<slug>.json`, committed). Mutation fixtures and the
//! fuzz generator are added by Task 15.

use std::fs;
use std::path::{Path, PathBuf};

use crate::canonical_json::from_canonical_json;
use crate::pb;

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

#[cfg(test)]
mod tests {
    use super::*;
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
