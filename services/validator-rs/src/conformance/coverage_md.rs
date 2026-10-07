//! `COVERAGE.md`: the documented rule coverage list (E1), generated from the coverage TSVs.
//! `conformance coverage-md` writes it, `coverage-md --check` and a unit test fail when the
//! committed file is stale.

use std::fmt::Write as _;
use std::path::PathBuf;

use crate::catalog::{Catalog, Entry, Family, Status};
use crate::pb;

use super::corpus;

/// `rulesets/pint-ae-1.0.4/COVERAGE.md`.
pub fn path() -> PathBuf {
    corpus::ruleset_dir().join("COVERAGE.md")
}

fn status(s: Status) -> &'static str {
    match s {
        Status::Implemented => "implemented",
        Status::Structural => "structural",
        Status::UpstreamNoop => "upstream_noop",
        Status::Pending => "pending",
    }
}

fn fix(e: &Entry) -> &'static str {
    use crate::catalog::Fix;
    match e.fix {
        Fix::None => "none",
        Fix::Value => "value",
        Fix::Llm => "llm",
    }
}

fn severity(e: &Entry) -> &'static str {
    match e.severity {
        pb::Severity::Error => "error",
        pb::Severity::Warning => "warning",
        _ => "unspecified",
    }
}

/// A table cell: the pipe is the only character that breaks a Markdown row, and a TSV cell never
/// holds a newline.
fn cell(s: &str) -> String {
    s.replace('|', "\\|")
}

fn count(entries: &[&Entry], s: Status) -> usize {
    entries.iter().filter(|e| e.status == s).count()
}

/// The generated document for the catalogue of the RuleSet `ruleset_id`.
pub fn render(catalog: &Catalog, ruleset_id: &str) -> String {
    let all = catalog.entries();
    let official: Vec<&Entry> = all.iter().filter(|e| e.official).collect();
    let platform: Vec<&Entry> = all.iter().filter(|e| !e.official).collect();
    let mut out = String::new();
    let w = &mut out;
    let _ = writeln!(w, "# Rule coverage: {ruleset_id}");
    let _ = writeln!(w);
    let _ = writeln!(
        w,
        "Generated from `services/validator-rs/rulesets/pint-ae-1.0.4/coverage/*.tsv` by \
         `cargo run --bin conformance -- coverage-md`. Do not edit; CI fails when it is stale \
         (`coverage-md --check`)."
    );
    let _ = writeln!(w);
    let _ = writeln!(
        w,
        "Every one of the {} official PINT-AE 1.0.4 asserts is listed once, with one of three \
         statuses: `implemented` (a registered Rust rule with a passing and a failing fixture), \
         `structural` (the canonical model plus the exporter make a violation impossible; the \
         note names the test that proves it) or `upstream_noop` (the official test can never \
         fail). Platform rules (`AE-*`) are listed after the official ones.",
        official.len()
    );
    let _ = writeln!(w);
    let _ = writeln!(
        w,
        "Official asserts: {} ({} implemented, {} structural, {} upstream_noop, {} pending). \
         Platform rules: {}.",
        official.len(),
        count(&official, Status::Implemented),
        count(&official, Status::Structural),
        count(&official, Status::UpstreamNoop),
        count(&official, Status::Pending),
        platform.len()
    );
    let _ = writeln!(w);
    let _ = writeln!(
        w,
        "| Family | Rules | Implemented | Structural | Upstream no-op | Pending |"
    );
    let _ = writeln!(w, "|---|---:|---:|---:|---:|---:|");
    let mut total = [0usize; 5];
    for family in Family::ALL {
        let rows: Vec<&Entry> = all.iter().filter(|e| e.family == family).collect();
        let c = [
            rows.len(),
            count(&rows, Status::Implemented),
            count(&rows, Status::Structural),
            count(&rows, Status::UpstreamNoop),
            count(&rows, Status::Pending),
        ];
        for (t, n) in total.iter_mut().zip(c) {
            *t += n;
        }
        let _ = writeln!(
            w,
            "| {} | {} | {} | {} | {} | {} |",
            family.as_str(),
            c[0],
            c[1],
            c[2],
            c[3],
            c[4]
        );
    }
    let _ = writeln!(
        w,
        "| **total** | **{}** | **{}** | **{}** | **{}** | **{}** |",
        total[0], total[1], total[2], total[3], total[4]
    );
    for family in Family::ALL {
        let rows: Vec<&Entry> = all.iter().filter(|e| e.family == family).collect();
        let _ = writeln!(w);
        let _ = writeln!(w, "## {} ({})", family.as_str(), rows.len());
        let _ = writeln!(w);
        let _ = writeln!(
            w,
            "| Rule | Status | Severity | Business term | Fix | Note |"
        );
        let _ = writeln!(w, "|---|---|---|---|---|---|");
        for e in rows {
            let _ = writeln!(
                w,
                "| `{}` | {} | {} | {} | {} | {} |",
                e.rule_id,
                status(e.status),
                severity(e),
                cell(e.business_term),
                fix(e),
                cell(e.note)
            );
        }
    }
    out
}

/// The generated document for the default RuleSet.
pub fn render_default() -> String {
    render(
        crate::catalog::pint_ae_1_0_4(),
        crate::ruleset::default_ruleset().id(),
    )
}

/// `Ok` when the committed file equals the generated one.
pub fn check() -> Result<(), String> {
    let p = path();
    let on_disk = std::fs::read_to_string(&p).map_err(|e| format!("{}: {e}", p.display()))?;
    if on_disk == render_default() {
        Ok(())
    } else {
        Err(format!(
            "{} is stale: run `cargo run --bin conformance -- coverage-md` and commit it",
            p.display()
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_committed_file_is_current() {
        check().unwrap_or_else(|e| panic!("{e}"));
    }

    #[test]
    fn the_document_lists_every_rule_once_and_the_status_counts() {
        let doc = render_default();
        let catalog = crate::catalog::pint_ae_1_0_4();
        for e in catalog.entries() {
            let needle = format!("| `{}` | {} |", e.rule_id, status(e.status));
            assert_eq!(doc.matches(&needle).count(), 1, "{}", e.rule_id);
        }
        assert!(doc.contains("Official asserts: 302 ("), "{}", &doc[..600]);
        assert!(doc.contains("| **total** |"));
    }

    #[test]
    fn pipes_in_a_note_do_not_break_the_table() {
        assert_eq!(cell("a|b"), "a\\|b");
    }
}
