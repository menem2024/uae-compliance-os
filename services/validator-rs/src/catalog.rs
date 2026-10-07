//! Rule metadata (spec 5.2.4) from the coverage TSVs `rulesets/pint-ae-1.0.4/coverage/<family>.tsv`
//! and the official assert texts `rulesets/pint-ae-1.0.4/upstream/rules-{base,ae}.tsv`, both
//! embedded at compile time so the server needs no data files at run time.
//!
//! Loading enforces the list: unique rule ids across files, official ids known upstream, platform
//! ids `^AE-[A-Z]+-[0-9]{3}$` in the `platform` family, valid status, severity, fix kind, business
//! term and path template, an Arabic message on every finished row, and the same `{placeholders}`
//! in both languages. `pending` (finding F6) is transitional: its `message_ar` is empty or the
//! placeholder [`PENDING_MESSAGE_AR`], and Task 15 fails on any remaining `pending` row.

use std::collections::{BTreeSet, HashMap};
use std::sync::LazyLock;

use crate::pb;

/// Placeholder Arabic text of a `pending` row ("translation under review").
pub const PENDING_MESSAGE_AR: &str = "ترجمة قيد المراجعة";

/// Header line of every coverage TSV.
pub const COVERAGE_HEADER: &str =
    "rule_id\tfamily\tstatus\tseverity\tbusiness_term\tpath\tfix\tmessage_en\tmessage_ar\tnote";

/// A rule family: one Rust module, one coverage TSV, one mutation file. Declaration order is the
/// issue sort order.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum Family {
    Header,
    Parties,
    Lines,
    Totals,
    Vat,
    Codelists,
    Platform,
}

impl Family {
    pub const ALL: [Family; 7] = [
        Family::Header,
        Family::Parties,
        Family::Lines,
        Family::Totals,
        Family::Vat,
        Family::Codelists,
        Family::Platform,
    ];

    pub fn as_str(self) -> &'static str {
        match self {
            Family::Header => "header",
            Family::Parties => "parties",
            Family::Lines => "lines",
            Family::Totals => "totals",
            Family::Vat => "vat",
            Family::Codelists => "codelists",
            Family::Platform => "platform",
        }
    }

    pub fn parse(s: &str) -> Option<Family> {
        Family::ALL.into_iter().find(|f| f.as_str() == s)
    }
}

/// Coverage status of a rule.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Status {
    /// A registered Rust rule with fixture tests.
    Implemented,
    /// The canonical model plus the exporter make a violation impossible; `note` names the test.
    Structural,
    /// The official test is constant (`ibr-187-ae` is `true()`).
    UpstreamNoop,
    /// Not yet decided (finding F6); Task 15 fails on any remaining one.
    Pending,
}

impl Status {
    pub fn parse(s: &str) -> Option<Status> {
        Some(match s {
            "implemented" => Status::Implemented,
            "structural" => Status::Structural,
            "upstream_noop" => Status::UpstreamNoop,
            "pending" => Status::Pending,
            _ => return None,
        })
    }
}

/// What the Fix agent may do about an issue of this rule.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Fix {
    None,
    /// The rule computes the one correct value (`suggested_value`).
    Value,
    /// The Fix agent may propose a value.
    Llm,
}

impl Fix {
    pub fn parse(s: &str) -> Option<Fix> {
        Some(match s {
            "none" => Fix::None,
            "value" => Fix::Value,
            "llm" => Fix::Llm,
            _ => return None,
        })
    }
}

/// One coverage row.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Entry {
    pub rule_id: &'static str,
    pub family: Family,
    pub status: Status,
    pub severity: pb::Severity,
    pub business_term: &'static str,
    /// Path template (`#` per index); empty only for rows that never produce an issue.
    pub path: &'static str,
    pub fix: Fix,
    /// The TSV text for a platform row; the official assert text without its `[id]-` prefix for
    /// an official row.
    pub message_en: &'static str,
    pub message_ar: &'static str,
    pub note: &'static str,
    /// An official schematron assert (as opposed to an `AE-` platform rule).
    pub official: bool,
}

/// The coverage list of one RuleSet.
#[derive(Debug)]
pub struct Catalog {
    entries: Vec<Entry>,
    index: HashMap<&'static str, usize>,
    upstream: HashMap<&'static str, &'static str>,
}

impl Catalog {
    /// Parses the coverage TSVs (one per family) against the upstream assert TSVs (`id, flag,
    /// context, test, message`, no header).
    pub fn parse(
        coverage: &[(Family, &'static str)],
        upstream: &[&'static str],
    ) -> Result<Catalog, String> {
        let mut official = HashMap::new();
        for (k, file) in upstream.iter().enumerate() {
            for (n, line) in file.lines().enumerate() {
                let at = format!("upstream[{k}]:{}", n + 1);
                let [id, _flag, _context, _test, message] =
                    line.split('\t').collect::<Vec<_>>()[..]
                else {
                    return Err(format!("{at}: expected 5 columns"));
                };
                let text = strip_id_prefix(id, message)
                    .ok_or_else(|| format!("{at}: message does not start with [{id}]-"))?;
                if official.insert(id, text).is_some() {
                    return Err(format!("{at}: duplicate assert id {id}"));
                }
            }
        }

        let checks = RowChecks {
            upstream: &official,
            platform_id: regex::Regex::new(r"^AE-[A-Z]+-[0-9]{3}$").expect("valid regex"),
            term: regex::Regex::new(r"^(IBT|IBG|BTAE)-[0-9]{2,3}(-[0-9]+)?$").expect("valid regex"),
        };
        let mut entries: Vec<Entry> = Vec::new();
        let mut index = HashMap::new();
        for &(family, file) in coverage {
            let name = format!("coverage/{}.tsv", family.as_str());
            let mut lines = file.lines();
            if lines.next() != Some(COVERAGE_HEADER) {
                return Err(format!(
                    "{name}: the first line must be the 10-column header"
                ));
            }
            for (n, line) in lines.enumerate() {
                let at = format!("{name}:{}", n + 2);
                let entry = checks.row(family, line).map_err(|e| format!("{at}: {e}"))?;
                if let Some(&first) = index.get(entry.rule_id) {
                    let other: &Entry = &entries[first];
                    return Err(format!(
                        "{at}: {} is already listed in coverage/{}.tsv",
                        entry.rule_id,
                        other.family.as_str()
                    ));
                }
                index.insert(entry.rule_id, entries.len());
                entries.push(entry);
            }
        }
        Ok(Catalog {
            entries,
            index,
            upstream: official,
        })
    }

    pub fn get(&self, rule_id: &str) -> Option<&Entry> {
        self.index.get(rule_id).map(|&i| &self.entries[i])
    }

    /// Every row, family by family in file order.
    pub fn entries(&self) -> &[Entry] {
        &self.entries
    }

    /// The official assert ids.
    pub fn upstream_ids(&self) -> impl Iterator<Item = &'static str> + '_ {
        self.upstream.keys().copied()
    }
}

/// Replaces each `{name}` of `template` with its argument; a placeholder without an argument is
/// kept as written.
pub fn render(template: &str, args: &[(&'static str, String)]) -> String {
    let mut out = String::with_capacity(template.len());
    let mut rest = template;
    while let Some(open) = rest.find('{') {
        out.push_str(&rest[..open]);
        let after = &rest[open + 1..];
        let value = after.find('}').and_then(|close| {
            let name = &after[..close];
            args.iter()
                .find(|(k, _)| *k == name)
                .map(|(_, v)| (v, close))
        });
        match value {
            Some((v, close)) => {
                out.push_str(v);
                rest = &after[close + 1..];
            }
            None => {
                out.push('{');
                rest = after;
            }
        }
    }
    out.push_str(rest);
    out
}

/// The `{name}` placeholders of a message (`name` = `[a-z_][a-z0-9_]*`).
fn placeholders(message: &str) -> BTreeSet<&str> {
    let mut out = BTreeSet::new();
    let mut rest = message;
    while let Some(open) = rest.find('{') {
        let after = &rest[open + 1..];
        let Some(close) = after.find('}') else { break };
        let name = &after[..close];
        let mut chars = name.chars();
        let is_name = chars
            .next()
            .is_some_and(|c| c.is_ascii_lowercase() || c == '_')
            && chars.all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '_');
        if is_name {
            out.insert(name);
            rest = &after[close + 1..];
        } else {
            rest = after;
        }
    }
    out
}

/// `[id]-Text` or `[id] - Text` to `Text`.
fn strip_id_prefix(id: &str, message: &'static str) -> Option<&'static str> {
    let text = message
        .strip_prefix('[')?
        .strip_prefix(id)?
        .strip_prefix(']')?
        .trim_start()
        .strip_prefix('-')?
        .trim_start();
    (!text.is_empty()).then_some(text)
}

struct RowChecks<'u> {
    upstream: &'u HashMap<&'static str, &'static str>,
    platform_id: regex::Regex,
    term: regex::Regex,
}

impl RowChecks<'_> {
    fn row(&self, family: Family, line: &'static str) -> Result<Entry, String> {
        let [
            rule_id,
            family_col,
            status,
            severity,
            business_term,
            path,
            fix,
            message_en,
            message_ar,
            note,
        ] = line.split('\t').collect::<Vec<_>>()[..]
        else {
            return Err("expected 10 tab-separated columns".into());
        };
        if family_col != family.as_str() {
            return Err(format!(
                "{rule_id}: family {family_col:?} in the {} file",
                family.as_str()
            ));
        }
        let upstream_text = self.upstream.get(rule_id).copied();
        let official = upstream_text.is_some();
        if official == (family == Family::Platform) {
            return Err(format!(
                "{rule_id}: official asserts belong to the six official families, AE- rules to platform"
            ));
        }
        if !official && !self.platform_id.is_match(rule_id) {
            return Err(format!(
                "{rule_id}: neither an official assert id nor ^AE-[A-Z]+-[0-9]{{3}}$"
            ));
        }
        let status =
            Status::parse(status).ok_or_else(|| format!("{rule_id}: bad status {status:?}"))?;
        let severity = match severity {
            "error" => pb::Severity::Error,
            "warning" if !official => pb::Severity::Warning,
            _ => return Err(format!("{rule_id}: bad severity {severity:?}")),
        };
        let fix = Fix::parse(fix).ok_or_else(|| format!("{rule_id}: bad fix {fix:?}"))?;
        if !business_term.is_empty() && !self.term.is_match(business_term) {
            return Err(format!("{rule_id}: bad business term {business_term:?}"));
        }
        if path.is_empty() {
            if status == Status::Implemented {
                return Err(format!(
                    "{rule_id}: an implemented rule needs a path template"
                ));
            }
        } else {
            crate::rule::check_template(path)
                .map_err(|e| format!("{rule_id}: path {path:?}: {e}"))?;
        }
        let message_en = match upstream_text {
            Some(text) if message_en.is_empty() => text,
            Some(_) => {
                return Err(format!(
                    "{rule_id}: an official row takes its English text from upstream"
                ));
            }
            None if message_en.is_empty() => {
                return Err(format!("{rule_id}: a platform row needs message_en"));
            }
            None => message_en,
        };
        if status == Status::Pending {
            if !message_ar.is_empty() && message_ar != PENDING_MESSAGE_AR {
                return Err(format!("{rule_id}: a pending row has no Arabic text yet"));
            }
        } else {
            if message_ar.is_empty() {
                return Err(format!("{rule_id}: message_ar is required"));
            }
            if placeholders(message_en) != placeholders(message_ar) {
                return Err(format!(
                    "{rule_id}: message_en and message_ar use different placeholders"
                ));
            }
        }
        if matches!(status, Status::Structural | Status::UpstreamNoop) && note.is_empty() {
            return Err(format!(
                "{rule_id}: a structural or upstream_noop row explains itself in note"
            ));
        }
        Ok(Entry {
            rule_id,
            family,
            status,
            severity,
            business_term,
            path,
            fix,
            message_en,
            message_ar,
            note,
            official,
        })
    }
}

/// The coverage TSVs of PINT-AE 1.0.4, embedded.
pub const PINT_AE_1_0_4_COVERAGE: [(Family, &str); 7] = [
    (
        Family::Header,
        include_str!("../rulesets/pint-ae-1.0.4/coverage/header.tsv"),
    ),
    (
        Family::Parties,
        include_str!("../rulesets/pint-ae-1.0.4/coverage/parties.tsv"),
    ),
    (
        Family::Lines,
        include_str!("../rulesets/pint-ae-1.0.4/coverage/lines.tsv"),
    ),
    (
        Family::Totals,
        include_str!("../rulesets/pint-ae-1.0.4/coverage/totals.tsv"),
    ),
    (
        Family::Vat,
        include_str!("../rulesets/pint-ae-1.0.4/coverage/vat.tsv"),
    ),
    (
        Family::Codelists,
        include_str!("../rulesets/pint-ae-1.0.4/coverage/codelists.tsv"),
    ),
    (
        Family::Platform,
        include_str!("../rulesets/pint-ae-1.0.4/coverage/platform.tsv"),
    ),
];

/// The official asserts of PINT-AE 1.0.4, embedded.
pub const PINT_AE_1_0_4_UPSTREAM: [&str; 2] = [
    include_str!("../rulesets/pint-ae-1.0.4/upstream/rules-base.tsv"),
    include_str!("../rulesets/pint-ae-1.0.4/upstream/rules-ae.tsv"),
];

/// The PINT-AE 1.0.4 catalogue, parsed once.
pub fn pint_ae_1_0_4() -> &'static Catalog {
    static CATALOG: LazyLock<Catalog> = LazyLock::new(|| {
        Catalog::parse(&PINT_AE_1_0_4_COVERAGE, &PINT_AE_1_0_4_UPSTREAM)
            .unwrap_or_else(|e| panic!("embedded coverage TSVs are invalid (tested): {e}"))
    });
    &CATALOG
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashSet;

    /// One coverage TSV row, column by column.
    type Row = [&'static str; 10];

    const UPSTREAM: &str = "ibr-001\tfatal\t/ubl:Invoice\tcbc:CustomizationID\t[ibr-001]-An Invoice shall have a Specification identifier (IBT-024).\n\
        ibr-sr-63\tfatal\t/ubl:Invoice\tnot(x)\t[ibr-sr-63] - A Specification identifier must not contain a '*'\n";

    const AR: &str = "رسالة";

    fn tsv(rows: &[[&str; 10]]) -> &'static str {
        let mut out = format!("{COVERAGE_HEADER}\n");
        for row in rows {
            out.push_str(&row.join("\t"));
            out.push('\n');
        }
        out.leak()
    }

    fn official(id: &str, status: &str) -> Row {
        let id: &'static str = id.to_string().leak();
        let status: &'static str = status.to_string().leak();
        [
            id,
            "header",
            status,
            "error",
            "IBT-024",
            "process.specification_identifier",
            "none",
            "",
            AR,
            "n",
        ]
    }

    fn platform(id: &str) -> Row {
        let id: &'static str = id.to_string().leak();
        [
            id,
            "platform",
            "implemented",
            "warning",
            "",
            "seller_trn",
            "llm",
            "Check {term}.",
            "تحقق من {term}.",
            "",
        ]
    }

    fn parse(rows: &[(Family, Row)]) -> Result<Catalog, String> {
        let mut by_family: Vec<(Family, Vec<[&str; 10]>)> =
            Family::ALL.iter().map(|&f| (f, Vec::new())).collect();
        for (family, row) in rows {
            by_family
                .iter_mut()
                .find(|(f, _)| f == family)
                .unwrap()
                .1
                .push(*row);
        }
        let coverage: Vec<(Family, &'static str)> =
            by_family.iter().map(|(f, rows)| (*f, tsv(rows))).collect();
        Catalog::parse(&coverage, &[UPSTREAM])
    }

    fn with(mut row: Row, col: usize, value: &'static str) -> Row {
        row[col] = value;
        row
    }

    #[test]
    fn a_valid_catalogue_parses() {
        let catalog = parse(&[
            (
                Family::Header,
                with(official("ibr-001", "pending"), 8, PENDING_MESSAGE_AR),
            ),
            (Family::Header, official("ibr-sr-63", "structural")),
            (Family::Platform, platform("AE-FMT-001")),
        ])
        .unwrap();
        assert_eq!(catalog.entries().len(), 3);
        let e = catalog.get("ibr-sr-63").unwrap();
        assert!(e.official);
        assert_eq!(e.family, Family::Header);
        assert_eq!(e.status, Status::Structural);
        assert_eq!(e.severity, pb::Severity::Error);
        assert_eq!(
            e.message_en,
            "A Specification identifier must not contain a '*'"
        );
        assert_eq!(
            catalog.get("ibr-001").unwrap().message_en,
            "An Invoice shall have a Specification identifier (IBT-024)."
        );
        let p = catalog.get("AE-FMT-001").unwrap();
        assert!(!p.official);
        assert_eq!(p.severity, pb::Severity::Warning);
        assert_eq!(p.fix, Fix::Llm);
        assert_eq!(p.message_en, "Check {term}.");
        let mut ids: Vec<_> = catalog.upstream_ids().collect();
        ids.sort_unstable();
        assert_eq!(ids, ["ibr-001", "ibr-sr-63"]);
    }

    #[test]
    fn pending_rows_take_an_empty_or_placeholder_arabic_message_only() {
        assert!(
            parse(&[(
                Family::Header,
                with(official("ibr-001", "pending"), 8, PENDING_MESSAGE_AR)
            )])
            .is_ok()
        );
        assert!(parse(&[(Family::Header, with(official("ibr-001", "pending"), 8, ""))]).is_ok());
        assert!(parse(&[(Family::Header, with(official("ibr-001", "pending"), 8, AR))]).is_err());
        assert!(
            parse(&[(
                Family::Header,
                with(official("ibr-001", "structural"), 8, "")
            )])
            .is_err()
        );
        assert!(parse(&[(Family::Platform, with(platform("AE-FMT-001"), 8, ""))]).is_err());
    }

    #[test]
    fn invalid_rows_are_rejected() {
        let ok = official("ibr-001", "structural");
        let cases: Vec<(&str, Vec<(Family, Row)>)> = vec![
            (
                "duplicate id",
                vec![
                    (Family::Header, ok),
                    (Family::Parties, with(ok, 1, "parties")),
                ],
            ),
            (
                "family column differs from file",
                vec![(Family::Header, with(ok, 1, "vat"))],
            ),
            (
                "unknown official id",
                vec![(Family::Header, official("ibr-999", "structural"))],
            ),
            (
                "bad platform id",
                vec![(Family::Platform, platform("AE-fmt-001"))],
            ),
            (
                "bad platform id digits",
                vec![(Family::Platform, platform("AE-FMT-01"))],
            ),
            (
                "platform id outside platform",
                vec![(Family::Header, with(platform("AE-FMT-001"), 1, "header"))],
            ),
            (
                "official id in platform",
                vec![(Family::Platform, with(ok, 1, "platform"))],
            ),
            (
                "official message_en",
                vec![(Family::Header, with(ok, 7, "text"))],
            ),
            (
                "platform without message_en",
                vec![(Family::Platform, with(platform("AE-FMT-001"), 7, ""))],
            ),
            ("bad status", vec![(Family::Header, with(ok, 2, "done"))]),
            ("bad severity", vec![(Family::Header, with(ok, 3, "fatal"))]),
            (
                "official warning",
                vec![(Family::Header, with(ok, 3, "warning"))],
            ),
            ("bad fix", vec![(Family::Header, with(ok, 6, "auto"))]),
            ("bad term", vec![(Family::Header, with(ok, 4, "ibt-024"))]),
            (
                "bad term number",
                vec![(Family::Header, with(ok, 4, "IBT-1"))],
            ),
            (
                "bad path",
                vec![(Family::Header, with(ok, 5, "invoice.seller_trn"))],
            ),
            (
                "path to a message",
                vec![(Family::Header, with(ok, 5, "seller"))],
            ),
            (
                "implemented without path",
                vec![(Family::Platform, with(platform("AE-FMT-001"), 5, ""))],
            ),
            (
                "structural without note",
                vec![(Family::Header, with(ok, 9, ""))],
            ),
            (
                "noop without note",
                vec![(Family::Header, with(with(ok, 2, "upstream_noop"), 9, ""))],
            ),
            (
                "placeholder only in arabic",
                vec![(Family::Header, with(ok, 8, "{expected}"))],
            ),
            (
                "placeholder differs",
                vec![(Family::Platform, with(platform("AE-FMT-001"), 8, "{value}"))],
            ),
        ];
        for (what, rows) in cases {
            assert!(parse(&rows).is_err(), "{what} was accepted");
        }
    }

    #[test]
    fn malformed_files_are_rejected() {
        let row = official("ibr-001", "structural").join("\t");
        let bad_files = [
            String::new(),
            format!("rule_id\tfamily\n{row}\n"),
            format!("{COVERAGE_HEADER}\n{}\n", &row[..row.rfind('\t').unwrap()]),
            format!("{COVERAGE_HEADER}\n{row}\textra\n"),
            format!("{COVERAGE_HEADER}\n\n{row}\n"),
        ];
        for file in bad_files {
            let file: &'static str = file.leak();
            let coverage = [(Family::Header, file)];
            assert!(Catalog::parse(&coverage, &[UPSTREAM]).is_err(), "{file:?}");
        }
        let header_only = [(Family::Header, COVERAGE_HEADER)];
        assert!(Catalog::parse(&header_only, &[UPSTREAM]).is_ok());
        assert!(Catalog::parse(&header_only, &["ibr-001\tfatal\tctx\n"]).is_err());
        assert!(Catalog::parse(&header_only, &[UPSTREAM, UPSTREAM]).is_err());
    }

    #[test]
    fn render_substitutes_named_arguments() {
        let args = [
            ("term", "IBT-112".to_string()),
            ("expected", "1050.00".to_string()),
        ];
        assert_eq!(
            render("{term} must be {expected}.", &args),
            "IBT-112 must be 1050.00."
        );
        assert_eq!(render("يجب أن تكون {term}", &args), "يجب أن تكون IBT-112");
        assert_eq!(render("no args", &[]), "no args");
        assert_eq!(render("{missing} stays", &args), "{missing} stays");
        assert_eq!(render("{term}{term}", &args), "IBT-112IBT-112");
        assert_eq!(render("open { brace", &args), "open { brace");
    }

    #[test]
    fn the_embedded_catalogue_loads() {
        let catalog = pint_ae_1_0_4();
        assert_eq!(catalog.upstream_ids().count(), 302);
        let official = catalog.entries().iter().filter(|e| e.official).count();
        assert!(official <= 302);
        for e in catalog.entries().iter().filter(|e| e.official) {
            assert!(
                !e.message_en.is_empty() && !e.message_en.starts_with('['),
                "{}",
                e.rule_id
            );
        }
        assert_eq!(
            catalog
                .get("ibr-sr-63")
                .map(|e| e.message_en.starts_with("A Specification identifier")),
            Some(true)
        );
        assert!(catalog.get("AE-FMT-001").is_some());
        assert!(catalog.get("AE-SCOPE-001").is_some());
    }

    /// Spec 5.2.4: every `implemented` row has exactly one registered rule, and every registered
    /// rule has an `implemented` row of its own family.
    #[test]
    fn implemented_rows_and_registered_rules_are_a_bijection() {
        let catalog = pint_ae_1_0_4();
        let mut registered = HashSet::new();
        for family in Family::ALL {
            for rule in crate::rules::family(family) {
                assert!(registered.insert(rule.id), "{} registered twice", rule.id);
                let entry = catalog
                    .get(rule.id)
                    .unwrap_or_else(|| panic!("{} has no coverage row", rule.id));
                assert_eq!(entry.status, Status::Implemented, "{}", rule.id);
                assert_eq!(
                    entry.family, family,
                    "{} is registered in another family",
                    rule.id
                );
            }
        }
        for e in catalog.entries() {
            assert_eq!(
                e.status == Status::Implemented,
                registered.contains(e.rule_id),
                "{}: implemented row without rule, or rule without implemented row",
                e.rule_id
            );
        }
        assert_eq!(registered.len(), crate::rules::all().len());
    }

    #[test]
    fn placeholders_are_identifiers_in_braces() {
        assert_eq!(placeholders("{a} {b_2} {a}"), BTreeSet::from(["a", "b_2"]));
        assert_eq!(placeholders("{} {A} { a } x"), BTreeSet::new());
    }
}
