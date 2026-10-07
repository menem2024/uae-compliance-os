//! Seeded random mutations of the corpus (spec 5.3.3): the differential test's fuzz documents.
//!
//! A fuzz document is a base example with one to four edits from a fixed vocabulary:
//!
//! * clear a field;
//! * set a code to another listed value, or to an invalid one;
//! * move an amount by +-0.01 or +-1;
//! * drop or duplicate a line;
//! * swap a VAT category code;
//! * change the type code between 380, 480, 381 and 81.
//!
//! Everything is derived from a SplitMix64 stream, so the same `(bases, n, seed)` always yields
//! the same documents (the golden snapshot depends on it). Edits go through the same
//! [`apply_patch`](super::apply_patch) as the mutation fixtures, so a path that does not exist is
//! an error and the edit is skipped, never a silent no-op.

use std::str::FromStr;

use rust_decimal::Decimal;
use serde_json::{Map, Value};

use crate::canonical_json::to_canonical_json;
use crate::codelists;
use crate::doc::Doc;
use crate::pb;

use super::apply_patch;

/// The seed of the committed corpus (the date the phase 2 plan was written).
pub const DEFAULT_SEED: u64 = 20_260_929;

/// The number of fuzz documents of the committed corpus.
pub const DEFAULT_COUNT: usize = 2000;

/// How many edits one document gets, at most.
const MAX_EDITS: usize = 4;

const TYPE_CODES: [&str; 4] = ["380", "480", "381", "81"];
const TAX_CATEGORIES: [&str; 6] = ["S", "Z", "E", "AE", "O", "N"];
const AMOUNT_DELTAS: [&str; 4] = ["0.01", "-0.01", "1", "-1"];

/// One accepted fuzz document.
#[derive(Debug, Clone)]
pub struct FuzzDoc {
    /// `fuzz-0000`, `fuzz-0001`, ... in generation order.
    pub name: String,
    /// The slug of the base example.
    pub base: String,
    /// One label per applied edit, in order.
    pub edits: Vec<String>,
    pub invoice: pb::Invoice,
}

/// SplitMix64: tiny, fast and identical on every platform.
struct Rng(u64);

impl Rng {
    fn next(&mut self) -> u64 {
        self.0 = self.0.wrapping_add(0x9E37_79B9_7F4A_7C15);
        let mut z = self.0;
        z = (z ^ (z >> 30)).wrapping_mul(0xBF58_476D_1CE4_E5B9);
        z = (z ^ (z >> 27)).wrapping_mul(0x94D0_49BB_1331_11EB);
        z ^ (z >> 31)
    }

    /// Uniform in `0..bound` (`bound > 0`); the modulo bias is irrelevant for fuzzing.
    fn below(&mut self, bound: usize) -> usize {
        usize::try_from(self.next() % bound as u64).expect("below a usize")
    }

    fn pick<'a, T>(&mut self, items: &'a [T]) -> Option<&'a T> {
        if items.is_empty() {
            None
        } else {
            Some(&items[self.below(items.len())])
        }
    }
}

/// Every string or bool leaf of the canonical form of `inv`, as `(path, value)`.
fn leaves(inv: &pb::Invoice) -> Vec<(String, Value)> {
    fn walk(prefix: &str, v: &Value, out: &mut Vec<(String, Value)>) {
        match v {
            Value::Object(m) => {
                for (k, child) in m {
                    let p = if prefix.is_empty() {
                        k.clone()
                    } else {
                        format!("{prefix}.{k}")
                    };
                    walk(&p, child, out);
                }
            }
            Value::Array(a) => {
                for (i, child) in a.iter().enumerate() {
                    walk(&format!("{prefix}[{i}]"), child, out);
                }
            }
            Value::String(_) | Value::Bool(_) => out.push((prefix.to_string(), v.clone())),
            _ => {}
        }
    }
    let json: Value = serde_json::from_str(&to_canonical_json(inv)).expect("canonical JSON");
    let mut out = Vec::new();
    walk("", &json, &mut out);
    out
}

/// `lines[3].price.net` becomes `lines[#].price.net` (the template form of a path).
fn template(path: &str) -> String {
    let mut out = String::with_capacity(path.len());
    let mut rest = path;
    while let Some(i) = rest.find('[') {
        out.push_str(&rest[..i]);
        out.push_str("[#]");
        rest = &rest[i..];
        match rest.find(']') {
            Some(j) => rest = &rest[j + 1..],
            None => return out,
        }
    }
    out.push_str(rest);
    out
}

fn line_count(inv: &pb::Invoice) -> usize {
    inv.lines.len()
}

fn apply(inv: &mut pb::Invoice, set: Map<String, Value>, remove: Vec<String>) -> bool {
    let mut next = inv.clone();
    if apply_patch(&mut next, &set, &remove).is_err() {
        return false;
    }
    *inv = next;
    true
}

fn set_one(inv: &mut pb::Invoice, path: &str, value: &str) -> bool {
    let mut set = Map::new();
    set.insert(path.to_string(), Value::String(value.to_string()));
    apply(inv, set, Vec::new())
}

/// The kinds of edit, picked uniformly.
const KINDS: usize = 7;

/// Applies one random edit of kind `kind`; the label when it changed the invoice.
fn edit(inv: &mut pb::Invoice, kind: usize, rng: &mut Rng) -> Option<String> {
    let before = inv.clone();
    let leaves = leaves(inv);
    let label = match kind {
        // Clear a field.
        0 => {
            let (path, _) = rng.pick(&leaves)?;
            let mut set = Map::new();
            set.insert(path.clone(), Value::String(String::new()));
            apply(inv, set, Vec::new()).then(|| format!("clear {path}"))
        }
        // Set a code to another listed value or to an invalid one.
        1 => {
            let sets = codelists::sets();
            let mut ids: Vec<&str> = sets.ids().collect();
            ids.sort_unstable();
            let coded: Vec<(&String, &str, &str)> = leaves
                .iter()
                .filter_map(|(path, v)| {
                    let s = v.as_str()?;
                    let list = ids.iter().find(|id| sets.contains(id, s))?;
                    Some((path, s, *list))
                })
                .collect();
            let (path, value, list) = *rng.pick(&coded)?;
            let new = if rng.below(2) == 0 {
                let mut codes: Vec<&str> = sets.get(list)?.iter().copied().collect();
                codes.sort_unstable();
                codes.retain(|c| *c != value);
                (*rng.pick(&codes)?).to_string()
            } else {
                match rng.below(3) {
                    0 => "ZZ9".to_string(),
                    1 => value.to_lowercase() + "x",
                    _ => format!(" {value}"),
                }
            };
            set_one(inv, path, &new).then(|| format!("code {path} {value}->{new}"))
        }
        // Move an amount by +-0.01 or +-1.
        2 => {
            let amounts: Vec<(&String, Decimal)> = leaves
                .iter()
                .filter(|(path, _)| {
                    let t = template(path);
                    Doc::DECIMAL_FIELDS.iter().any(|f| f.path == t)
                })
                .filter_map(|(path, v)| Some((path, Decimal::from_str(v.as_str()?).ok()?)))
                .collect();
            let (path, value) = rng.pick(&amounts)?;
            let delta = Decimal::from_str(AMOUNT_DELTAS[rng.below(AMOUNT_DELTAS.len())]).ok()?;
            let moved = value.checked_add(delta)?;
            set_one(inv, path, &moved.to_string())
                .then(|| format!("amount {path} {value}->{moved}"))
        }
        // Drop a line.
        3 => {
            let n = line_count(inv);
            if n == 0 {
                return None;
            }
            let i = rng.below(n);
            apply(inv, Map::new(), vec![format!("lines[{i}]")]).then(|| format!("drop lines[{i}]"))
        }
        // Duplicate a line (appended as the last one).
        4 => {
            let n = line_count(inv);
            if n == 0 {
                return None;
            }
            let i = rng.below(n);
            let from = format!("lines[{i}].");
            let to = format!("lines[{n}].");
            let mut set = Map::new();
            for (path, v) in &leaves {
                if let Some(rest) = path.strip_prefix(&from) {
                    set.insert(format!("{to}{rest}"), v.clone());
                }
            }
            apply(inv, set, Vec::new()).then(|| format!("duplicate lines[{i}]"))
        }
        // Swap a VAT category code.
        5 => {
            let cats: Vec<(&String, &str)> = leaves
                .iter()
                .filter_map(|(path, v)| {
                    let s = v.as_str()?;
                    (path.ends_with(".code") && TAX_CATEGORIES.contains(&s)).then_some((path, s))
                })
                .collect();
            let (path, value) = *rng.pick(&cats)?;
            let other: Vec<&str> = TAX_CATEGORIES
                .iter()
                .copied()
                .filter(|c| *c != value)
                .collect();
            let new = *rng.pick(&other)?;
            set_one(inv, path, new).then(|| format!("tax {path} {value}->{new}"))
        }
        // Change the type code between 380, 480, 381 and 81.
        _ => {
            let other: Vec<&str> = TYPE_CODES
                .iter()
                .copied()
                .filter(|c| *c != inv.invoice_type_code)
                .collect();
            let new = *rng.pick(&other)?;
            set_one(inv, "invoice_type_code", new).then(|| format!("type code ->{new}"))
        }
    };
    if label.is_some() && *inv == before {
        return None;
    }
    label
}

/// `n` fuzz documents over `bases`, seeded with `seed`. `accept` filters a candidate (the
/// differential corpus drops every document with an `AE-FMT-*` or `AE-EXP-*` error: such a
/// document cannot be serialised faithfully and is never exported); a rejected candidate is
/// replaced by the next one, so exactly `n` documents come back unless `bases` is empty or
/// nothing is ever accepted (then the attempt budget of `50 * n` ends the loop early).
pub fn generate(
    bases: &[(String, pb::Invoice)],
    n: usize,
    seed: u64,
    accept: &dyn Fn(&pb::Invoice) -> bool,
) -> Vec<FuzzDoc> {
    let mut rng = Rng(seed);
    let mut out = Vec::with_capacity(n);
    if bases.is_empty() {
        return out;
    }
    let mut attempts = 0usize;
    while out.len() < n && attempts < 50 * n.max(1) {
        attempts += 1;
        let (slug, base) = &bases[rng.below(bases.len())];
        let wanted = 1 + rng.below(MAX_EDITS);
        let mut inv = base.clone();
        let mut edits = Vec::new();
        // Each edit may find nothing to change (no amount, no line); a few extra tries.
        for _ in 0..wanted * 4 {
            if edits.len() == wanted {
                break;
            }
            let kind = rng.below(KINDS);
            if let Some(label) = edit(&mut inv, kind, &mut rng) {
                edits.push(label);
            }
        }
        // Edits can cancel (duplicate a line, then drop it): such a document is just its base.
        if edits.is_empty() || inv == *base || !accept(&inv) {
            continue;
        }
        out.push(FuzzDoc {
            name: format!("fuzz-{:04}", out.len()),
            base: slug.clone(),
            edits,
            invoice: inv,
        });
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::conformance::examples;
    use std::sync::LazyLock;

    // Generating is slow in a debug build (every edit re-serialises the invoice), so the tests
    // share one batch and keep their own batches tiny.
    const BATCH: usize = 120;

    static BASES: LazyLock<Vec<(String, pb::Invoice)>> = LazyLock::new(examples);

    static DOCS: LazyLock<Vec<FuzzDoc>> =
        LazyLock::new(|| generate(&BASES, BATCH, DEFAULT_SEED, &|_| true));

    #[test]
    fn the_same_seed_gives_the_same_documents_and_another_seed_does_not() {
        let again = generate(&BASES, 10, DEFAULT_SEED, &|_| true);
        assert_eq!(again.len(), 10);
        for (x, y) in DOCS.iter().zip(&again) {
            assert_eq!(
                (&x.base, &x.edits, &x.invoice),
                (&y.base, &y.edits, &y.invoice)
            );
        }
        let other = generate(&BASES, 10, DEFAULT_SEED + 1, &|_| true);
        assert!(
            DOCS.iter().zip(&other).any(|(x, y)| x.invoice != y.invoice),
            "another seed must change the corpus"
        );
    }

    #[test]
    fn every_document_differs_from_its_base_and_has_one_to_four_edits() {
        assert_eq!(DOCS.len(), BATCH);
        for d in DOCS.iter() {
            let base = &BASES.iter().find(|(s, _)| *s == d.base).unwrap().1;
            assert_ne!(&d.invoice, base, "{}: {:?}", d.name, d.edits);
            assert!((1..=MAX_EDITS).contains(&d.edits.len()), "{}", d.name);
        }
    }

    #[test]
    fn the_vocabulary_is_fully_used() {
        for prefix in [
            "clear ",
            "code ",
            "amount ",
            "drop ",
            "duplicate ",
            "tax ",
            "type code",
        ] {
            assert!(
                DOCS.iter()
                    .flat_map(|d| &d.edits)
                    .any(|e| e.starts_with(prefix)),
                "no {prefix:?} edit in {BATCH} documents"
            );
        }
    }

    #[test]
    fn rejected_candidates_are_replaced_and_names_are_dense() {
        let only_credit_notes =
            |inv: &pb::Invoice| matches!(inv.invoice_type_code.as_str(), "381" | "81");
        let docs = generate(&BASES, 4, DEFAULT_SEED, &only_credit_notes);
        assert_eq!(docs.len(), 4);
        for (i, d) in docs.iter().enumerate() {
            assert_eq!(d.name, format!("fuzz-{i:04}"));
            assert!(only_credit_notes(&d.invoice));
        }
        // Nothing accepted: the attempt budget (50 per wanted document) ends the loop.
        assert!(generate(&BASES, 1, 1, &|_| false).is_empty());
        assert!(generate(&[], 5, 1, &|_| true).is_empty());
    }

    #[test]
    fn template_replaces_every_index() {
        assert_eq!(
            template("lines[3].allowances_charges[12].amount"),
            "lines[#].allowances_charges[#].amount"
        );
        assert_eq!(template("total_amount"), "total_amount");
    }

    #[test]
    fn an_amount_edit_keeps_the_decimal_grammar() {
        let grammar = regex::Regex::new(r"^[+-]?[0-9]+(\.[0-9]+)?$").unwrap();
        let mut seen = 0;
        for d in DOCS.iter() {
            for e in d.edits.iter().filter(|e| e.starts_with("amount ")) {
                let new = e.rsplit("->").next().unwrap();
                assert!(grammar.is_match(new), "{}: {e}", d.name);
                seen += 1;
            }
        }
        assert!(seen > 5, "{seen}");
    }
}
