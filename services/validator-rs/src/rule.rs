//! A rule is a pure function over a [`Doc`] that reports findings into a [`Sink`] (spec 5.2.5).
//! The sink turns the coverage TSV's path template into concrete paths; the RuleSet then turns
//! findings into `pb::ValidationIssue`s through the catalogue.

use std::fmt::Display;

use crate::doc::Doc;

/// Orders field paths segment by segment, numeric indices as numbers (`lines[2]` < `lines[10]`).
/// Issue order and mutation patching share this one implementation (finding F13).
pub use crate::conformance::patch::path_cmp;

/// One registered rule. `id` is the official assert id verbatim (`ibr-132-ae`) or a platform id
/// (`AE-FMT-001`); everything else about it (family, severity, path template, fix kind, messages)
/// comes from its coverage TSV row.
pub struct Rule {
    pub id: &'static str,
    pub check: fn(&Doc<'_>, &mut Sink<'_>),
}

impl std::fmt::Debug for Rule {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Rule").field("id", &self.id).finish()
    }
}

/// One failing context node.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Finding {
    /// Concrete path in the CI section 12 grammar.
    pub path: String,
    /// Business term overriding the TSV's, for rules whose context spans several fields (F7).
    pub business_term: Option<&'static str>,
    /// `message_args`: named values for the message placeholders, never free text.
    pub args: Vec<(&'static str, String)>,
    /// The one value for `path` that makes the rule pass (TSV `fix = value` only).
    pub suggested_value: Option<String>,
}

impl Finding {
    fn at(path: String) -> Self {
        Finding {
            path,
            business_term: None,
            args: Vec::new(),
            suggested_value: None,
        }
    }

    /// Overrides the TSV row's business term for this finding.
    pub fn term(&mut self, term: &'static str) -> &mut Self {
        self.business_term = Some(term);
        self
    }

    /// Adds a message argument (`{name}` in the TSV messages).
    pub fn arg(&mut self, name: &'static str, value: impl Display) -> &mut Self {
        self.args.push((name, value.to_string()));
        self
    }

    /// Sets the suggested value.
    pub fn suggest(&mut self, value: impl Display) -> &mut Self {
        self.suggested_value = Some(value.to_string());
        self
    }
}

/// Collects the findings of one rule over one document.
#[derive(Debug)]
pub struct Sink<'t> {
    template: &'t str,
    findings: Vec<Finding>,
}

impl<'t> Sink<'t> {
    /// A sink for a rule whose coverage row has the path template `template`.
    pub fn new(template: &'t str) -> Self {
        Sink {
            template,
            findings: Vec::new(),
        }
    }

    /// Reports a failing node at the TSV path template, its `#`s replaced left to right by `idx`.
    pub fn fail(&mut self, idx: &[usize]) -> &mut Finding {
        let path = fill(self.template, idx);
        self.push(path)
    }

    /// Reports a failing node at a path the rule builds itself, for contexts that map to several
    /// fields (F7: `ibr-132-ae`, `ibr-139-ae`, `ibr-126`, `ibr-128-ae`).
    pub fn fail_at(&mut self, path: impl Into<String>) -> &mut Finding {
        self.push(path.into())
    }

    fn push(&mut self, path: String) -> &mut Finding {
        self.findings.push(Finding::at(path));
        let last = self.findings.len() - 1;
        &mut self.findings[last]
    }

    pub fn findings(&self) -> &[Finding] {
        &self.findings
    }

    pub fn into_findings(self) -> Vec<Finding> {
        self.findings
    }
}

/// Replaces the `#`s of `template` left to right by `idx`. The number of indices must match the
/// number of `#`s (checked in debug builds; a release build keeps unmatched `#`s rather than
/// panicking).
pub fn fill(template: &str, idx: &[usize]) -> String {
    debug_assert_eq!(
        template.matches('#').count(),
        idx.len(),
        "indices {idx:?} do not match template {template:?}"
    );
    use std::fmt::Write;

    let mut out = String::with_capacity(template.len() + 2 * idx.len());
    let mut rest = idx.iter();
    for ch in template.chars() {
        if ch == '#'
            && let Some(i) = rest.next()
        {
            let _ = write!(out, "{i}");
            continue;
        }
        out.push(ch);
    }
    out
}

fn invoice_descriptor() -> &'static prost_reflect::MessageDescriptor {
    static D: std::sync::OnceLock<prost_reflect::MessageDescriptor> = std::sync::OnceLock::new();
    D.get_or_init(|| {
        prost_reflect::DescriptorPool::decode(crate::FILE_DESCRIPTOR_SET)
            .expect("embedded descriptor set decodes")
            .get_message_by_name("compliance.v1.Invoice")
            .expect("compliance.v1.Invoice is in the descriptor set")
    })
}

/// Checks a path template against `compliance.v1.Invoice`: CI section 12 grammar with `#` (or a
/// fixed number) as index, every segment a field, an index exactly on repeated fields, and the
/// last segment a `string` or `bool` field.
pub fn check_template(template: &str) -> Result<(), String> {
    use prost_reflect::Kind;

    let mut message = invoice_descriptor().clone();
    let segments: Vec<&str> = template.split('.').collect();
    for (n, segment) in segments.iter().enumerate() {
        let (name, indexed) = match segment.split_once('[') {
            None => (*segment, false),
            Some((name, rest)) => {
                let index = rest
                    .strip_suffix(']')
                    .ok_or_else(|| format!("bad segment {segment:?}"))?;
                if index != "#" && (index.is_empty() || !index.bytes().all(|b| b.is_ascii_digit()))
                {
                    return Err(format!("bad index in {segment:?}"));
                }
                (name, true)
            }
        };
        if name.is_empty()
            || !name
                .bytes()
                .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'_')
        {
            return Err(format!("bad field name in {segment:?}"));
        }
        let field = message
            .get_field_by_name(name)
            .ok_or_else(|| format!("{} has no field {name:?}", message.name()))?;
        if field.is_list() != indexed {
            return Err(format!(
                "{name:?}: an index is required exactly on repeated fields"
            ));
        }
        let last = n + 1 == segments.len();
        match (field.kind(), last) {
            (Kind::Message(next), false) => message = next,
            (Kind::String | Kind::Bool, true) => {}
            (_, false) => return Err(format!("{name:?} is not a message")),
            (_, true) => return Err(format!("{name:?} is not a string or bool field")),
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cmp::Ordering;

    #[test]
    fn fill_replaces_hashes_left_to_right() {
        assert_eq!(fill("seller_trn", &[]), "seller_trn");
        assert_eq!(
            fill("lines[#].price.net_price", &[3]),
            "lines[3].price.net_price"
        );
        assert_eq!(
            fill("lines[#].allowances_charges[#].amount", &[2, 0]),
            "lines[2].allowances_charges[0].amount"
        );
        assert_eq!(
            fill("lines[#].item.classifications[#].code", &[10, 12]),
            "lines[10].item.classifications[12].code"
        );
        assert_eq!(
            fill("buyer.identifiers[0].id", &[]),
            "buyer.identifiers[0].id"
        );
    }

    #[test]
    fn fail_fills_the_template() {
        let mut sink = Sink::new("lines[#].allowances_charges[#].amount");
        sink.fail(&[1, 0]);
        sink.fail(&[1, 2]);
        let paths: Vec<_> = sink.findings().iter().map(|f| f.path.as_str()).collect();
        assert_eq!(
            paths,
            [
                "lines[1].allowances_charges[0].amount",
                "lines[1].allowances_charges[2].amount"
            ]
        );
        assert!(sink.findings().iter().all(|f| f.business_term.is_none()
            && f.args.is_empty()
            && f.suggested_value.is_none()));
    }

    #[test]
    fn fail_at_takes_the_concrete_path_and_term_overrides_f7() {
        let mut sink = Sink::new("seller_trn");
        sink.fail(&[]);
        sink.fail_at("buyer_trn").term("IBT-048");
        sink.fail_at(String::from("tax_representative.vat_identifier"))
            .term("IBT-063");
        let got: Vec<_> = sink
            .into_findings()
            .into_iter()
            .map(|f| (f.path, f.business_term))
            .collect();
        assert_eq!(
            got,
            [
                ("seller_trn".to_string(), None),
                ("buyer_trn".to_string(), Some("IBT-048")),
                (
                    "tax_representative.vat_identifier".to_string(),
                    Some("IBT-063")
                ),
            ]
        );
    }

    #[test]
    fn findings_carry_args_and_suggestions() {
        let mut sink = Sink::new("totals.line_extension_amount");
        let value = rust_decimal::Decimal::new(105000, 2);
        sink.fail(&[])
            .arg("expected", value)
            .arg("term", "IBT-106")
            .suggest(value);
        let f = &sink.findings()[0];
        assert_eq!(
            f.args,
            [
                ("expected", "1050.00".to_string()),
                ("term", "IBT-106".to_string())
            ]
        );
        assert_eq!(f.suggested_value.as_deref(), Some("1050.00"));
    }

    #[test]
    fn a_sink_without_failures_is_empty() {
        let sink = Sink::new("seller_trn");
        assert!(sink.findings().is_empty());
        assert!(sink.into_findings().is_empty());
    }

    #[test]
    fn check_template_accepts_paths_ending_on_string_or_bool_fields() {
        for ok in [
            "seller_trn",
            "process.specification_identifier",
            "lines[#].price.net_price",
            "lines[#].allowances_charges[#].amount",
            "lines[#].allowances_charges[#].is_charge",
            "buyer.identifiers[0].id",
            "totals.tax_inclusive_pricing",
            "tax_breakdown[#].category.rate",
        ] {
            assert_eq!(check_template(ok), Ok(()), "{ok}");
        }
    }

    #[test]
    fn check_template_rejects_bad_paths() {
        for bad in [
            "",
            "invoice.seller_trn",
            "seller_trn.",
            ".seller_trn",
            "lines..id",
            "lines.id",
            "seller[#].name",
            "lines[#]",
            "seller",
            "lines[#].price",
            "totals.nope",
            "Lines[#].id",
            "lines[x].id",
            "lines[].id",
            "lines[#]id",
            "lines[#].id[#]",
            "seller_trn[0]",
        ] {
            assert!(check_template(bad).is_err(), "{bad:?}");
        }
    }

    #[test]
    fn path_cmp_is_numeric_aware() {
        assert_eq!(path_cmp("lines[2].id", "lines[10].id"), Ordering::Less);
        assert_eq!(path_cmp("lines[10].id", "lines[2].id"), Ordering::Greater);
        assert_eq!(path_cmp("buyer_trn", "seller_trn"), Ordering::Less);
        assert_eq!(path_cmp("lines[1]", "lines[1].id"), Ordering::Less);
        assert_eq!(path_cmp("lines[0].id", "lines[0].id"), Ordering::Equal);
    }
}
