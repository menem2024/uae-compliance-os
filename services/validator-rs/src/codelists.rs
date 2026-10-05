//! Code lists inlined in the official XPath (finding F10), loaded from the committed
//! `rulesets/pint-ae-1.0.4/codelists/codelists.tsv` (`list_id`, `code`; `list_id` is the rule id,
//! or `<rule id>#<n>` when a rule has several lists). By CI rule 13 these inline lists, not the
//! `.gc` files, are authoritative. Lists shorter than five codes stay literal in the rule.

use std::collections::{HashMap, HashSet};
use std::sync::LazyLock;

/// The embedded TSV of PINT-AE 1.0.4.
pub const PINT_AE_1_0_4_TSV: &str =
    include_str!("../rulesets/pint-ae-1.0.4/codelists/codelists.tsv");

/// Code-list sets by list id.
#[derive(Debug, Default)]
pub struct CodeLists {
    lists: HashMap<&'static str, HashSet<&'static str>>,
}

impl CodeLists {
    /// Parses a `list_id<TAB>code` TSV with that header line. Codes are matched exactly
    /// (case-sensitive, no trimming), as XPath `contains(' A B ', concat(' ', x, ' '))` does.
    pub fn parse(tsv: &'static str) -> Result<Self, String> {
        let mut lines = tsv.lines();
        if lines.next() != Some("list_id\tcode") {
            return Err("codelists: the header must be `list_id<TAB>code`".into());
        }
        let mut out = CodeLists::default();
        for (n, line) in lines.enumerate() {
            let at = n + 2;
            let (list_id, code) = match line.split('\t').collect::<Vec<_>>()[..] {
                [list_id, code] => (list_id, code),
                _ => return Err(format!("codelists:{at}: expected 2 columns")),
            };
            if list_id.is_empty() || code.is_empty() {
                return Err(format!("codelists:{at}: empty list id or code"));
            }
            if !out.lists.entry(list_id).or_default().insert(code) {
                return Err(format!(
                    "codelists:{at}: duplicate code {code:?} in {list_id}"
                ));
            }
        }
        Ok(out)
    }

    /// The set `list_id`, if the TSV has it.
    pub fn get(&self, list_id: &str) -> Option<&HashSet<&'static str>> {
        self.lists.get(list_id)
    }

    /// Whether `code` is in list `list_id`; false for an unknown list (a typo in a rule makes its
    /// family's pass cases fail, and debug builds stop here).
    pub fn contains(&self, list_id: &str, code: &str) -> bool {
        let list = self.lists.get(list_id);
        debug_assert!(list.is_some(), "unknown code list {list_id:?}");
        list.is_some_and(|set| set.contains(code))
    }

    /// The list ids, in no particular order.
    pub fn ids(&self) -> impl Iterator<Item = &'static str> + '_ {
        self.lists.keys().copied()
    }
}

/// The PINT-AE 1.0.4 code lists, parsed once.
pub fn sets() -> &'static CodeLists {
    static SETS: LazyLock<CodeLists> = LazyLock::new(|| {
        CodeLists::parse(PINT_AE_1_0_4_TSV).expect("embedded codelists.tsv is valid (tested)")
    });
    &SETS
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_embedded_lists_load() {
        let sets = sets();
        let mut ids: Vec<_> = sets.ids().collect();
        ids.sort_unstable();
        assert_eq!(ids.len(), 24);
        assert_eq!(ids.first(), Some(&"ibr-001-ae"));
        let rows = PINT_AE_1_0_4_TSV.lines().count() - 1;
        let total: usize = sets.ids().map(|id| sets.get(id).unwrap().len()).sum();
        assert_eq!(total, rows, "no duplicate rows");
    }

    #[test]
    fn membership_is_exact() {
        let sets = sets();
        let tax = sets.get("ibr-139-ae").unwrap();
        let mut codes: Vec<_> = tax.iter().copied().collect();
        codes.sort_unstable();
        assert_eq!(codes, ["AE", "E", "N", "O", "S", "Z"]);
        // Upstream defect 1: the platform uses ASCII N, never U+039D.
        assert!(sets.contains("ibr-139-ae", "N"));
        assert!(!sets.contains("ibr-139-ae", "\u{39d}"));

        assert!(sets.contains("ibr-cl-04", "AED"));
        assert!(sets.contains("ibr-cl-04", "USD"));
        assert!(!sets.contains("ibr-cl-04", "aed"));
        assert!(!sets.contains("ibr-cl-04", " AED"));
        assert!(!sets.contains("ibr-cl-04", ""));
        assert!(sets.contains("ibr-cl-23", "H87"));
        assert!(sets.contains("ibr-005-ae", "MTH"));
        assert!(sets.contains("ibr-001-ae", "VD"));
        assert_eq!(sets.get("ibr-cl-04").unwrap().len(), 178);
    }

    #[test]
    fn an_unknown_list_has_no_members() {
        assert!(sets().get("ibr-nope").is_none());
    }

    #[test]
    fn parse_rejects_malformed_input() {
        assert!(CodeLists::parse("list_id\tcode\nibr-1\tA\n").is_ok());
        for bad in [
            "",
            "id\tcode\nibr-1\tA\n",
            "list_id\tcode\nibr-1\n",
            "list_id\tcode\nibr-1\tA\tB\n",
            "list_id\tcode\n\tA\n",
            "list_id\tcode\nibr-1\t\n",
            "list_id\tcode\nibr-1\tA\nibr-1\tA\n",
        ] {
            assert!(CodeLists::parse(bad).is_err(), "{bad:?}");
        }
    }
}
