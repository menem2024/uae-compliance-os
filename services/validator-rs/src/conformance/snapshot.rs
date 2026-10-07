//! The golden snapshot of a RuleSet revision (spec 5.2.4): `snapshot/r<N>.jsonl` stores, for
//! every document of the differential corpus, the sorted `(rule_id, path, severity)` list the
//! RuleSet reports. A test compares the live results with the frozen file; a behaviour change
//! fails it, and the fix is to bump the revision and write `r<N+1>.jsonl`, never to edit the
//! frozen one (`rulesets/pint-ae-1.0.4/README.md`, "Revision policy").
//!
//! One line per document, in corpus order:
//! `{"doc":"examples/standard-tax-invoice","issues":[["ibr-002","seller_trn","error"],...]}`.

use std::path::PathBuf;

use serde_json::json;

use crate::pb;
use crate::ruleset::RuleSet;

use super::corpus::{self, CorpusDoc};
use super::fuzz;

/// `snapshot/r<N>.jsonl` of the RuleSet `rs` (`pint-ae@1.0.4+r1` is `r1`).
pub fn path(rs: &RuleSet) -> Result<PathBuf, String> {
    let (_, rev) = rs
        .id()
        .rsplit_once("+r")
        .ok_or_else(|| format!("RuleSet id {:?} has no +r<N> revision", rs.id()))?;
    if rev.is_empty() || !rev.bytes().all(|b| b.is_ascii_digit()) {
        return Err(format!("RuleSet id {:?} has no +r<N> revision", rs.id()));
    }
    Ok(corpus::ruleset_dir().join(format!("snapshot/r{rev}.jsonl")))
}

fn severity(s: i32) -> &'static str {
    match pb::Severity::try_from(s) {
        Ok(pb::Severity::Error) => "error",
        Ok(pb::Severity::Warning) => "warning",
        _ => "unspecified",
    }
}

/// The snapshot line of one document's run (without the newline).
pub fn line(name: &str, run: &pb::ValidationRun) -> String {
    let mut issues: Vec<(&str, &str, &str)> = run
        .issues
        .iter()
        .map(|i| (i.rule_id.as_str(), i.path.as_str(), severity(i.severity)))
        .collect();
    issues.sort_unstable();
    json!({"doc": name, "issues": issues}).to_string()
}

/// The snapshot of `docs` under `rs`: one line per document, each ended by a newline.
pub fn render(docs: &[CorpusDoc], rs: &RuleSet) -> String {
    let mut out = String::new();
    for d in docs {
        out.push_str(&line(&d.name, &rs.validate(&d.invoice)));
        out.push('\n');
    }
    out
}

/// The snapshot of the committed corpus (examples, every mutation fixture, 2,000 fuzz documents
/// of the default seed) under `rs`.
pub fn render_default(rs: &RuleSet) -> Result<String, String> {
    let docs = corpus::documents(fuzz::DEFAULT_COUNT, fuzz::DEFAULT_SEED, rs)?;
    Ok(render(&docs, rs))
}

/// `Ok` when `live` equals `frozen`; otherwise the first differing documents, at most ten.
pub fn compare(frozen: &str, live: &str) -> Result<(), String> {
    if frozen == live {
        return Ok(());
    }
    let f: Vec<&str> = frozen.lines().collect();
    let l: Vec<&str> = live.lines().collect();
    let mut msg = vec![format!(
        "snapshot differs: {} frozen documents, {} live",
        f.len(),
        l.len()
    )];
    let mut shown = 0;
    for i in 0..f.len().max(l.len()) {
        let (a, b) = (f.get(i), l.get(i));
        if a != b {
            msg.push(format!(
                "line {}:\n  frozen {}\n  live   {}",
                i + 1,
                a.copied().unwrap_or("(none)"),
                b.copied().unwrap_or("(none)")
            ));
            shown += 1;
            if shown == 10 {
                msg.push("...".into());
                break;
            }
        }
    }
    Err(msg.join("\n"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ruleset::default_ruleset;

    #[test]
    fn the_path_follows_the_revision() {
        let p = path(default_ruleset()).unwrap();
        assert!(p.ends_with("snapshot/r1.jsonl"), "{}", p.display());
    }

    #[test]
    fn a_line_sorts_its_issues_and_names_severity_in_words() {
        let issue = |rule: &str, path: &str, sev: pb::Severity| pb::ValidationIssue {
            rule_id: rule.into(),
            path: path.into(),
            severity: sev as i32,
            ..Default::default()
        };
        let run = pb::ValidationRun {
            issues: vec![
                issue("b", "x", pb::Severity::Warning),
                issue("a", "y", pb::Severity::Error),
                issue("a", "x", pb::Severity::Error),
            ],
            ..Default::default()
        };
        assert_eq!(
            line("d", &run),
            r#"{"doc":"d","issues":[["a","x","error"],["a","y","error"],["b","x","warning"]]}"#
        );
    }

    #[test]
    fn compare_reports_the_differing_documents() {
        assert!(compare("a\nb\n", "a\nb\n").is_ok());
        let e = compare("a\nb\n", "a\nc\n").unwrap_err();
        assert!(
            e.contains("line 2") && e.contains("frozen b") && e.contains("live   c"),
            "{e}"
        );
        let e = compare("a\n", "a\nb\n").unwrap_err();
        assert!(e.contains("1 frozen documents, 2 live"), "{e}");
    }
}
