//! Test support for the differential conformance CLI (`bin/conformance.rs`).
//!
//! * [`ubl_import`]: official UBL XML to canonical `pb::Invoice`;
//! * [`patch`]: applies a mutation fixture's `set` / `remove` paths;
//! * [`corpus`]: the 30 official examples as committed canonical JSON (Task 5) and the whole
//!   differential corpus (examples, mutation fixtures, fuzz documents);
//! * [`fuzz`]: the seeded random mutations of the corpus;
//! * [`snapshot`]: the golden snapshot `snapshot/r1.jsonl`;
//! * [`coverage_md`]: the generated `COVERAGE.md`;
//! * [`Mutation`]: one failing fixture of `mutations/<family>.jsonl` (spec 5.2.8), shared by the
//!   generic family harness (`tests/family_fixtures.rs`) and `conformance mutations`.

pub mod corpus;
pub mod coverage_md;
pub mod fuzz;
pub mod patch;
pub mod snapshot;
pub mod ubl_import;

pub use corpus::examples;
pub use patch::{PatchError, apply as apply_patch, path_cmp};
pub use ubl_import::{ImportError, ImportReport, from_xml};

use std::collections::{BTreeMap, HashSet};
use std::path::PathBuf;

use serde::Deserialize;
use serde_json::{Map, Value};

use crate::catalog::{Family, Status};
use crate::pb;
use crate::ruleset::RuleSet;

/// One failing fixture: `base` is a corpus slug, `set` and `remove` are applied with
/// [`apply_patch`], `expect` is the **complete** multiset of rule ids the mutated invoice fails
/// (official ids exactly as the official schematron reports them on the exported XML, plus the
/// platform ids).
#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Mutation {
    pub id: String,
    pub base: String,
    pub set: Map<String, Value>,
    pub remove: Vec<String>,
    pub expect: Vec<String>,
    pub note: String,
}

/// A fixture id as a file stem: `AE-FMT-001#1` becomes `AE-FMT-001_1`.
pub fn file_stem(id: &str) -> String {
    id.chars()
        .map(|c| {
            if c.is_ascii_alphanumeric() || matches!(c, '.' | '-' | '_') {
                c
            } else {
                '_'
            }
        })
        .collect()
}

/// `rulesets/pint-ae-1.0.4/mutations/<family>.jsonl`.
pub fn mutations_path(family: Family) -> PathBuf {
    corpus::ruleset_dir().join(format!("mutations/{}.jsonl", family.as_str()))
}

/// The fixtures of one family, in file order. Every line is one JSON object with exactly the
/// six fields of [`Mutation`]; ids are unique and the note is not empty.
pub fn mutations(family: Family) -> Result<Vec<Mutation>, String> {
    let path = mutations_path(family);
    let text = std::fs::read_to_string(&path).map_err(|e| format!("{}: {e}", path.display()))?;
    let mut out: Vec<Mutation> = Vec::new();
    let mut ids = HashSet::new();
    for (n, line) in text.lines().enumerate() {
        let at = format!("{}:{}", path.display(), n + 1);
        let m: Mutation = serde_json::from_str(line).map_err(|e| format!("{at}: {e}"))?;
        if !ids.insert(m.id.clone()) {
            return Err(format!("{at}: duplicate fixture id {}", m.id));
        }
        if m.note.trim().is_empty() {
            return Err(format!("{at}: {} has no note", m.id));
        }
        if m.expect.is_empty() {
            return Err(format!("{at}: {} expects no rule", m.id));
        }
        out.push(m);
    }
    Ok(out)
}

impl Mutation {
    /// The base example with `set` and `remove` applied.
    pub fn apply(&self, bases: &[(String, pb::Invoice)]) -> Result<pb::Invoice, String> {
        let (_, base) = bases
            .iter()
            .find(|(slug, _)| *slug == self.base)
            .ok_or_else(|| format!("{}: unknown base {:?}", self.id, self.base))?;
        let mut inv = base.clone();
        apply_patch(&mut inv, &self.set, &self.remove).map_err(|e| format!("{}: {e}", self.id))?;
        Ok(inv)
    }

    /// The part of `expect` the RuleSet can report today, sorted: the ids of `implemented`
    /// (registered) rules. Once every official rule is registered (Task 15), this is all of
    /// `expect`.
    pub fn expect_registered(&self, rs: &RuleSet) -> Vec<&str> {
        let mut out: Vec<&str> = self
            .expect
            .iter()
            .map(String::as_str)
            .filter(|id| {
                rs.catalog()
                    .get(id)
                    .is_some_and(|e| e.status == Status::Implemented)
            })
            .collect();
        out.sort_unstable();
        out
    }

    /// Validates the mutated invoice and checks the run against the fixture: every `expect` id
    /// is a catalogue rule, the fixture's own rule (`<rule>#<n>`) is in `expect`, and the
    /// multiset of rule ids of the run equals [`Self::expect_registered`] exactly.
    pub fn check(&self, inv: &pb::Invoice, rs: &RuleSet) -> Result<pb::ValidationRun, String> {
        if let Some(id) = self.expect.iter().find(|id| rs.catalog().get(id).is_none()) {
            return Err(format!("{}: expect names unknown rule {id}", self.id));
        }
        let own = self.id.split('#').next().unwrap_or_default();
        if !self.expect.iter().any(|e| e == own) {
            return Err(format!(
                "{}: the fixture's own rule {own} is not in expect",
                self.id
            ));
        }
        let run = rs.validate(inv);
        let mut got: Vec<&str> = run.issues.iter().map(|i| i.rule_id.as_str()).collect();
        got.sort_unstable();
        let want = self.expect_registered(rs);
        if got != want {
            return Err(format!(
                "{}: rule ids {got:?}, expected {want:?} (expect restricted to registered rules)",
                self.id
            ));
        }
        Ok(run)
    }

    /// `expect` as `{rule id: count}`.
    pub fn expect_counts(&self) -> BTreeMap<&str, u32> {
        let mut out = BTreeMap::new();
        for id in &self.expect {
            *out.entry(id.as_str()).or_insert(0) += 1;
        }
        out
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture(expect: &[&str], id: &str) -> Mutation {
        Mutation {
            id: id.into(),
            base: "standard-tax-invoice".into(),
            set: Map::new(),
            remove: vec![],
            expect: expect.iter().map(|s| s.to_string()).collect(),
            note: "n".into(),
        }
    }

    #[test]
    fn every_family_file_parses() {
        for f in Family::ALL {
            mutations(f).unwrap_or_else(|e| panic!("{e}"));
        }
    }

    #[test]
    fn check_rejects_unknown_rules_a_missing_own_rule_and_a_wrong_multiset() {
        let rs = crate::ruleset::default_ruleset();
        let bases = examples();
        let inv = fixture(&[], "x#1").apply(&bases).unwrap();
        let err = fixture(&["AE-NOPE-001"], "AE-NOPE-001#1").check(&inv, rs);
        assert!(err.unwrap_err().contains("unknown rule"));
        let err = fixture(&["ibr-002"], "AE-FMT-001#9").check(&inv, rs);
        assert!(err.unwrap_err().contains("not in expect"));
        // The unmodified example fails nothing, so expecting a registered rule is wrong.
        let err = fixture(&["AE-FMT-001"], "AE-FMT-001#9").check(&inv, rs);
        assert!(err.unwrap_err().contains("rule ids []"));
        // An official rule that is not registered yet is not required of the RuleSet today.
        if rs.catalog().get("ibr-002").unwrap().status != Status::Implemented {
            assert!(fixture(&["ibr-002"], "ibr-002#9").check(&inv, rs).is_ok());
        }
        let unknown_base = Mutation {
            base: "nope".into(),
            ..fixture(&["ibr-002"], "ibr-002#9")
        };
        assert!(
            unknown_base
                .apply(&bases)
                .unwrap_err()
                .contains("unknown base")
        );
    }

    #[test]
    fn expect_counts_keep_multiplicity() {
        let m = fixture(&["b", "a", "b"], "a#1");
        assert_eq!(
            m.expect_counts().into_iter().collect::<Vec<_>>(),
            [("a", 1), ("b", 2)]
        );
    }
}
