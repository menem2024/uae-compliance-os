//! Applies a mutation fixture's `set` / `remove` paths (CI §12 grammar) to a canonical
//! `pb::Invoice`.
//!
//! * `set`: path -> string or bool. A path to index `len` of a repeated field appends an
//!   element, an index beyond `len` is an error, and `""` clears the field. Paths are applied
//!   in numeric-aware order ([`path_cmp`], finding F13) so that `lines[2]` is applied before
//!   `lines[10]` and appends happen in index order whatever the JSON object order was.
//! * `remove`: `"lines[1]"` deletes that element of a repeated field, highest index first, after
//!   all `set` entries. A path without an index (`"payee"`) clears that field or sub-message.
//!
//! The patch works on the canonical JSON form and converts back through
//! [`from_canonical_json`](crate::canonical_json::from_canonical_json), so an unknown field or a
//! value of the wrong type in a fixture is an error, never a silent no-op.

use std::cmp::Ordering;

use serde_json::{Map, Value};

use crate::canonical_json::{from_canonical_json, to_canonical_json};
use crate::pb;

/// A fixture path or value that cannot be applied.
#[derive(Debug, PartialEq, Eq)]
pub struct PatchError(pub String);

impl std::fmt::Display for PatchError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "patch: {}", self.0)
    }
}

impl std::error::Error for PatchError {}

fn err<T>(msg: impl Into<String>) -> Result<T, PatchError> {
    Err(PatchError(msg.into()))
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
enum Token {
    Name(String),
    Index(u64),
}

fn tokens(path: &str) -> Vec<Token> {
    let mut out = Vec::new();
    for seg in path.split('.') {
        match seg.split_once('[') {
            Some((name, rest)) => {
                out.push(Token::Name(name.to_string()));
                let idx = rest.strip_suffix(']').unwrap_or(rest);
                match idx.parse::<u64>() {
                    Ok(n) => out.push(Token::Index(n)),
                    Err(_) => out.push(Token::Name(format!("[{idx}]"))),
                }
            }
            None => out.push(Token::Name(seg.to_string())),
        }
    }
    out
}

/// Orders field paths segment by segment: names lexically, indices as numbers
/// (`lines[2]` < `lines[10]`), a prefix before its extensions.
pub fn path_cmp(a: &str, b: &str) -> Ordering {
    tokens(a).cmp(&tokens(b))
}

#[derive(Debug)]
struct Segment {
    name: String,
    index: Option<usize>,
}

fn parse(path: &str) -> Result<Vec<Segment>, PatchError> {
    if path.is_empty() {
        return err("empty path");
    }
    path.split('.')
        .map(|seg| {
            let (name, index) = match seg.split_once('[') {
                None => (seg, None),
                Some((name, rest)) => {
                    let Some(num) = rest.strip_suffix(']') else {
                        return err(format!("bad segment {seg:?} in {path:?}"));
                    };
                    if num.is_empty() || !num.bytes().all(|b| b.is_ascii_digit()) {
                        return err(format!("bad index in {seg:?} of {path:?}"));
                    }
                    match num.parse::<usize>() {
                        Ok(n) => (name, Some(n)),
                        Err(_) => return err(format!("bad index in {seg:?} of {path:?}")),
                    }
                }
            };
            if name.is_empty()
                || !name
                    .bytes()
                    .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'_')
            {
                return err(format!("bad field name {name:?} in {path:?}"));
            }
            Ok(Segment {
                name: name.to_string(),
                index,
            })
        })
        .collect()
}

/// Descends through `segs`, creating objects (and appending at index == len) when `create`,
/// and returns the object reached.
fn descend<'a>(
    root: &'a mut Map<String, Value>,
    segs: &[Segment],
    create: bool,
    path: &str,
) -> Result<&'a mut Map<String, Value>, PatchError> {
    let mut cur = root;
    for seg in segs {
        let slot = if create {
            cur.entry(seg.name.clone())
                .or_insert_with(|| match seg.index {
                    Some(_) => Value::Array(Vec::new()),
                    None => Value::Object(Map::new()),
                })
        } else {
            match cur.get_mut(&seg.name) {
                Some(v) => v,
                None => return err(format!("{path:?}: no field {:?}", seg.name)),
            }
        };
        let obj = match seg.index {
            None => slot,
            Some(i) => {
                let Value::Array(arr) = slot else {
                    return err(format!("{path:?}: {:?} is not repeated", seg.name));
                };
                if i == arr.len() && create {
                    arr.push(Value::Object(Map::new()));
                }
                let len = arr.len();
                match arr.get_mut(i) {
                    Some(v) => v,
                    None => {
                        return err(format!(
                            "{path:?}: index {i} out of range for {:?} (len {len})",
                            seg.name
                        ));
                    }
                }
            }
        };
        let Value::Object(next) = obj else {
            return err(format!("{path:?}: {:?} is not a message", seg.name));
        };
        cur = next;
    }
    Ok(cur)
}

/// Applies `set` then `remove` to `inv`. On error `inv` is left unchanged.
pub fn apply(
    inv: &mut pb::Invoice,
    set: &Map<String, Value>,
    remove: &[String],
) -> Result<(), PatchError> {
    let Value::Object(mut root) = serde_json::from_str::<Value>(&to_canonical_json(inv))
        .map_err(|e| PatchError(e.to_string()))?
    else {
        return err("invoice did not serialise to an object");
    };

    let mut sets: Vec<(&String, &Value)> = set.iter().collect();
    sets.sort_by(|a, b| path_cmp(a.0, b.0));
    for (path, value) in sets {
        let segs = parse(path)?;
        let (last, parents) = segs.split_last().expect("parse rejects empty paths");
        if last.index.is_some() {
            return err(format!(
                "{path:?}: a set path must end on a string or bool field"
            ));
        }
        if !matches!(value, Value::String(_) | Value::Bool(_)) {
            return err(format!("{path:?}: value must be a string or a bool"));
        }
        let holder = descend(&mut root, parents, true, path)?;
        match value {
            Value::String(s) if s.is_empty() => {
                holder.remove(&last.name);
            }
            v => {
                holder.insert(last.name.clone(), v.clone());
            }
        }
    }

    let mut removes: Vec<&String> = remove.iter().collect();
    removes.sort_by(|a, b| path_cmp(b, a));
    for path in removes {
        let segs = parse(path)?;
        let (last, parents) = segs.split_last().expect("parse rejects empty paths");
        let holder = descend(&mut root, parents, false, path)?;
        match last.index {
            None => {
                if holder.remove(&last.name).is_none() {
                    return err(format!("{path:?}: field is not set"));
                }
            }
            Some(i) => {
                let Some(Value::Array(arr)) = holder.get_mut(&last.name) else {
                    return err(format!("{path:?}: no repeated field {:?}", last.name));
                };
                if i >= arr.len() {
                    return err(format!(
                        "{path:?}: index {i} out of range (len {})",
                        arr.len()
                    ));
                }
                arr.remove(i);
                if arr.is_empty() {
                    holder.remove(&last.name);
                }
            }
        }
    }

    let patched =
        from_canonical_json(&Value::Object(root).to_string()).map_err(|e| PatchError(e.0))?;
    *inv = patched;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn lines(n: usize) -> pb::Invoice {
        pb::Invoice {
            lines: (0..n)
                .map(|i| pb::InvoiceLine {
                    id: format!("L{i}"),
                    ..Default::default()
                })
                .collect(),
            ..Default::default()
        }
    }

    fn set(v: Value) -> Map<String, Value> {
        v.as_object().unwrap().clone()
    }

    #[test]
    fn path_cmp_is_numeric_aware() {
        assert_eq!(path_cmp("lines[2].id", "lines[10].id"), Ordering::Less);
        assert_eq!(path_cmp("lines[10].id", "lines[2].id"), Ordering::Greater);
        assert_eq!(path_cmp("lines[2]", "lines[2]"), Ordering::Equal);
        assert_eq!(path_cmp("lines[1]", "lines[1].id"), Ordering::Less);
        assert_eq!(path_cmp("lines[9]", "lines[10]"), Ordering::Less);
        assert_eq!(path_cmp("a.b", "a.c"), Ordering::Less);
        let mut v = vec!["lines[10].id", "lines[2].id", "lines[1].id", "currency"];
        v.sort_by(|a, b| path_cmp(a, b));
        assert_eq!(
            v,
            ["currency", "lines[1].id", "lines[2].id", "lines[10].id"]
        );
    }

    #[test]
    fn set_applies_in_numeric_order_so_appends_line_up() {
        // 11 existing lines; the JSON map is key-sorted lexically, so a naive loop would see
        // "lines[10]", "lines[11]", "lines[12]" in that order but "lines[2]" after "lines[12]".
        let mut inv = lines(11);
        let s = set(json!({"lines[12].id": "new2", "lines[11].id": "new1", "lines[10].id": "X"}));
        apply(&mut inv, &s, &[]).unwrap();
        assert_eq!(inv.lines.len(), 13);
        assert_eq!(inv.lines[10].id, "X");
        assert_eq!(inv.lines[11].id, "new1");
        assert_eq!(inv.lines[12].id, "new2");
    }

    #[test]
    fn append_after_a_lexically_later_index() {
        // "lines[2]" sorts after "lines[10]" lexically; appending index 2 into 2 lines and
        // index 10 is only valid if index 2.. comes first. Here: 2 lines, set [2] and [3].
        let mut inv = lines(2);
        let s = set(json!({"lines[3].id": "c", "lines[2].id": "b"}));
        apply(&mut inv, &s, &[]).unwrap();
        assert_eq!(inv.lines.len(), 4);
        assert_eq!(inv.lines[2].id, "b");
        assert_eq!(inv.lines[3].id, "c");
    }

    #[test]
    fn set_creates_nested_messages_and_bools() {
        let mut inv = pb::Invoice::default();
        let s = set(json!({
            "seller.postal_address.country_code": "AE",
            "totals.tax_inclusive_pricing": true,
            "lines[0].price.net_price": "1.50",
            "lines[0].allowances_charges[0].is_charge": true,
        }));
        apply(&mut inv, &s, &[]).unwrap();
        assert_eq!(
            inv.seller.unwrap().postal_address.unwrap().country_code,
            "AE"
        );
        assert!(inv.totals.unwrap().tax_inclusive_pricing);
        assert_eq!(inv.lines[0].price.as_ref().unwrap().net_price, "1.50");
        assert!(inv.lines[0].allowances_charges[0].is_charge);
    }

    #[test]
    fn empty_string_clears_a_field() {
        let mut inv = lines(1);
        inv.currency = "AED".into();
        apply(
            &mut inv,
            &set(json!({"currency": "", "lines[0].id": ""})),
            &[],
        )
        .unwrap();
        assert_eq!(inv.currency, "");
        assert_eq!(inv.lines[0].id, "");
        assert_eq!(inv.lines.len(), 1);
    }

    #[test]
    fn set_rejects_bad_paths_and_values() {
        for (path, val) in [
            ("lines[5].id", json!("x")),  // beyond len
            ("lines[0]", json!("x")),     // does not end on a field
            ("nope", json!("x")),         // unknown field
            ("lines[0].id", json!(5)),    // not a string
            ("lines[0].id", json!(true)), // wrong scalar type
            ("lines[a].id", json!("x")),  // bad index
            ("Lines[0].id", json!("x")),  // bad name
            ("", json!("x")),
        ] {
            let mut inv = lines(1);
            let before = inv.clone();
            let mut m = Map::new();
            m.insert(path.to_string(), val);
            assert!(apply(&mut inv, &m, &[]).is_err(), "{path}");
            assert_eq!(inv, before, "invoice unchanged on error: {path}");
        }
    }

    #[test]
    fn remove_deletes_elements_highest_index_first() {
        let mut inv = lines(5);
        let rm = vec![
            "lines[1]".to_string(),
            "lines[3]".to_string(),
            "lines[10]".to_string(),
        ];
        assert!(apply(&mut inv, &Map::new(), &rm).is_err()); // out of range
        assert_eq!(inv.lines.len(), 5, "unchanged on error");
        let rm = vec!["lines[1]".to_string(), "lines[3]".to_string()];
        apply(&mut inv, &Map::new(), &rm).unwrap();
        let ids: Vec<_> = inv.lines.iter().map(|l| l.id.as_str()).collect();
        assert_eq!(ids, ["L0", "L2", "L4"]);
    }

    #[test]
    fn remove_orders_numerically_not_lexically() {
        let mut inv = lines(12);
        // lexical descending would remove "lines[2]" before "lines[10]" and shift it.
        let rm = vec!["lines[2]".to_string(), "lines[10]".to_string()];
        apply(&mut inv, &Map::new(), &rm).unwrap();
        let ids: Vec<_> = inv.lines.iter().map(|l| l.id.clone()).collect();
        assert!(!ids.contains(&"L2".to_string()));
        assert!(!ids.contains(&"L10".to_string()));
        assert_eq!(ids.len(), 10);
    }

    #[test]
    fn remove_runs_after_set_and_handles_unindexed_paths() {
        let mut inv = lines(3);
        inv.payee = Some(pb::Payee {
            name: "P".into(),
            ..Default::default()
        });
        let s = set(json!({"lines[3].id": "L3"})); // append, then remove the first line
        let rm = vec!["lines[0]".to_string(), "payee".to_string()];
        apply(&mut inv, &s, &rm).unwrap();
        let ids: Vec<_> = inv.lines.iter().map(|l| l.id.as_str()).collect();
        assert_eq!(ids, ["L1", "L2", "L3"]);
        assert!(inv.payee.is_none());
        assert!(apply(&mut inv, &Map::new(), &["payee".to_string()]).is_err());
    }
}
