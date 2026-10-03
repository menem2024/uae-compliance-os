//! Differential conformance CLI: exports canonical invoices and mutation fixtures as PINT-AE
//! UBL so the official schematron (`conformance/run_schematron.py`) and the XSD
//! (`conformance/xsd_check.py`) can be run on exactly the XML the platform writes, and
//! `conformance/compare.py` can compare the results.
//!
//! ```text
//! conformance export --in <canonical json> --out <xml>
//! conformance mutations --family <family> --out <dir>
//! ```
//!
//! `export` writes the XML of one canonical invoice without checking validity.
//!
//! `mutations` is step 9 of the rule authoring protocol: it applies every fixture of
//! `mutations/<family>.jsonl` to its base example, checks the RuleSet against the fixture
//! (the multiset of rule ids equals `expect` restricted to the registered rules, see
//! `Mutation::check`; any difference fails the command), and writes `<dir>/<fixture>.xml` plus
//! `<dir>/rust.json` in the shape `compare.py` reads:
//! `{ "<fixture>.xml": { "rules": {rule id: count}, "errors": n, "exported": bool } }`.
//! `rules` is the fixture's complete `expect` multiset: its registered part has just been
//! checked against the Rust RuleSet, so `compare.py` checks every fixture's `expect` list against
//! the official schematron (spec 8: an `expect` list must match Saxon), and once every official
//! rule is registered it is exactly the RuleSet's output. `errors` counts the `expect` entries of
//! `error` severity. A fixture whose XML cannot be written at all (`AE-EXP-005`, a character XML
//! 1.0 forbids) is reported as skipped and left out of both. Existing `*.xml` files and
//! `rust.json` in `<dir>` are replaced; any other file there is an error, so a mistyped
//! directory is never cleaned.
//!
//! `corpus`, `coverage-md` and `snapshot` arrive with Task 15.

use std::collections::{BTreeMap, HashSet};
use std::fs;
use std::path::Path;
use std::process::ExitCode;

use serde_json::json;
use sha2::{Digest, Sha256};
use validator_rs::canonical_json::from_canonical_json;
use validator_rs::catalog::Family;
use validator_rs::conformance::{self, Mutation};
use validator_rs::doc::Doc;
use validator_rs::export;
use validator_rs::pb;
use validator_rs::ruleset::{self, RuleSet};

const USAGE: &str = "usage:\n  conformance export --in <canonical json> --out <xml>\n  conformance mutations --family <family> --out <dir>";

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match run(&args) {
        Ok(summary) => {
            println!("{summary}");
            ExitCode::SUCCESS
        }
        Err(e) => {
            eprintln!("conformance: {e}");
            ExitCode::FAILURE
        }
    }
}

fn run(args: &[String]) -> Result<String, String> {
    match args.first().map(String::as_str) {
        Some("export") => {
            let o = opts(&args[1..], &["--in", "--out"])?;
            export_cmd(Path::new(&o["--in"]), Path::new(&o["--out"]))
        }
        Some("mutations") => {
            let o = opts(&args[1..], &["--family", "--out"])?;
            let family = Family::parse(&o["--family"])
                .ok_or_else(|| format!("unknown family {:?}", o["--family"]))?;
            mutations_cmd(family, Path::new(&o["--out"]), ruleset::default_ruleset())
        }
        _ => Err(USAGE.into()),
    }
}

/// `--key value` pairs; exactly the keys in `keys`, each once.
fn opts(args: &[String], keys: &[&str]) -> Result<BTreeMap<String, String>, String> {
    let mut out = BTreeMap::new();
    let mut it = args.iter();
    while let Some(k) = it.next() {
        if !keys.contains(&k.as_str()) {
            return Err(format!("unexpected argument {k:?}\n{USAGE}"));
        }
        let v = it
            .next()
            .ok_or_else(|| format!("{k} needs a value\n{USAGE}"))?;
        if out.insert(k.clone(), v.clone()).is_some() {
            return Err(format!("{k} given twice"));
        }
    }
    if let Some(missing) = keys.iter().find(|k| !out.contains_key(**k)) {
        return Err(format!("missing {missing}\n{USAGE}"));
    }
    Ok(out)
}

fn export_cmd(input: &Path, output: &Path) -> Result<String, String> {
    let json = fs::read_to_string(input).map_err(|e| format!("{}: {e}", input.display()))?;
    let inv: pb::Invoice =
        from_canonical_json(&json).map_err(|e| format!("{}: {e}", input.display()))?;
    let xml = export::to_xml(&Doc::new(&inv)).map_err(|e| e.to_string())?;
    fs::write(output, &xml).map_err(|e| format!("{}: {e}", output.display()))?;
    Ok(format!(
        "wrote {} ({} bytes, sha256 {})",
        output.display(),
        xml.len(),
        hex::encode(Sha256::digest(&xml))
    ))
}

/// A fixture id as a file stem: `AE-FMT-001#1` becomes `AE-FMT-001_1`.
fn file_stem(id: &str) -> String {
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

/// Creates `dir`, or empties it of a previous run's output; refuses a directory holding
/// anything else.
fn prepare_dir(dir: &Path) -> Result<(), String> {
    fs::create_dir_all(dir).map_err(|e| format!("{}: {e}", dir.display()))?;
    let mut stale = Vec::new();
    for entry in fs::read_dir(dir).map_err(|e| format!("{}: {e}", dir.display()))? {
        let path = entry.map_err(|e| e.to_string())?.path();
        let name = path
            .file_name()
            .and_then(|n| n.to_str())
            .unwrap_or_default()
            .to_string();
        let ours = path.is_file()
            && (name.ends_with(".xml")
                || matches!(name.as_str(), "rust.json" | "saxon.json" | "xsd.json"));
        if !ours {
            return Err(format!(
                "{}: holds {name:?}, which this command did not write; use an empty directory",
                dir.display()
            ));
        }
        if name.ends_with(".xml") || name == "rust.json" {
            stale.push(path);
        }
    }
    for path in stale {
        fs::remove_file(&path).map_err(|e| format!("{}: {e}", path.display()))?;
    }
    Ok(())
}

fn errors_in_expect(m: &Mutation, rs: &RuleSet) -> usize {
    m.expect
        .iter()
        .filter(|id| {
            rs.catalog()
                .get(id)
                .is_some_and(|e| e.severity == pb::Severity::Error)
        })
        .count()
}

fn mutations_cmd(family: Family, dir: &Path, rs: &RuleSet) -> Result<String, String> {
    let fixtures = conformance::mutations(family)?;
    let bases = conformance::examples();
    prepare_dir(dir)?;
    let mut rust = BTreeMap::new();
    let mut files = HashSet::new();
    let mut skipped = Vec::new();
    for m in &fixtures {
        let inv = m.apply(&bases)?;
        m.check(&inv, rs)?;
        let file = format!("{}.xml", file_stem(&m.id));
        if !files.insert(file.clone()) {
            return Err(format!("{}: file name {file} is already used", m.id));
        }
        match export::to_xml(&Doc::new(&inv)) {
            Ok(xml) => {
                let path = dir.join(&file);
                fs::write(&path, xml).map_err(|e| format!("{}: {e}", path.display()))?;
                let errors = errors_in_expect(m, rs);
                rust.insert(
                    file,
                    json!({"rules": m.expect_counts(), "errors": errors, "exported": errors == 0}),
                );
            }
            Err(e) => skipped.push(format!("{} ({e})", m.id)),
        }
    }
    let path = dir.join("rust.json");
    let text = serde_json::to_string_pretty(&rust).map_err(|e| e.to_string())? + "\n";
    fs::write(&path, text).map_err(|e| format!("{}: {e}", path.display()))?;
    let mut summary = format!(
        "{}: {} fixtures, {} written to {}",
        family.as_str(),
        fixtures.len(),
        rust.len(),
        dir.display()
    );
    for s in skipped {
        summary.push_str(&format!("\nskipped, not serialisable: {s}"));
    }
    Ok(summary)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    fn s(v: &[&str]) -> Vec<String> {
        v.iter().map(|x| x.to_string()).collect()
    }

    fn scratch(name: &str) -> PathBuf {
        let dir =
            std::env::temp_dir().join(format!("conformance-cli-{name}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        dir
    }

    #[test]
    fn options_are_exact() {
        let o = opts(&s(&["--out", "b", "--in", "a"]), &["--in", "--out"]).unwrap();
        assert_eq!((o["--in"].as_str(), o["--out"].as_str()), ("a", "b"));
        assert!(opts(&s(&["--in", "a"]), &["--in", "--out"]).is_err());
        assert!(opts(&s(&["--in"]), &["--in"]).is_err());
        assert!(opts(&s(&["--in", "a", "--in", "b"]), &["--in"]).is_err());
        assert!(opts(&s(&["--x", "a"]), &["--in"]).is_err());
        assert!(run(&s(&["nope"])).unwrap_err().starts_with("usage"));
        assert!(run(&s(&["mutations", "--family", "nope", "--out", "x"])).is_err());
    }

    #[test]
    fn file_stems_are_safe() {
        assert_eq!(file_stem("AE-FMT-001#1"), "AE-FMT-001_1");
        assert_eq!(file_stem("ibr-co-10#12"), "ibr-co-10_12");
        assert_eq!(file_stem("a/b c"), "a_b_c");
    }

    #[test]
    fn export_writes_the_golden_bytes() {
        let root = Path::new(env!("CARGO_MANIFEST_DIR"));
        let dir = scratch("export");
        fs::create_dir_all(&dir).unwrap();
        for slug in ["standard-tax-invoice", "standard-tax-credit-note"] {
            let out = dir.join(format!("{slug}.xml"));
            let input = root.join(format!(
                "rulesets/pint-ae-1.0.4/corpus/examples/{slug}.json"
            ));
            export_cmd(&input, &out).unwrap();
            let golden = fs::read(root.join(format!("tests/golden/{slug}.xml"))).unwrap();
            assert!(fs::read(&out).unwrap() == golden, "{slug}");
        }
        assert!(export_cmd(&dir.join("missing.json"), &dir.join("x.xml")).is_err());
        fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn mutations_writes_one_xml_per_serialisable_fixture_and_rust_json() {
        let dir = scratch("mutations");
        let rs = ruleset::default_ruleset();
        let summary = mutations_cmd(Family::Platform, &dir, rs).unwrap();
        let fixtures = conformance::mutations(Family::Platform).unwrap();
        let rust: BTreeMap<String, serde_json::Value> =
            serde_json::from_str(&fs::read_to_string(dir.join("rust.json")).unwrap()).unwrap();
        let mut xml: Vec<String> = fs::read_dir(&dir)
            .unwrap()
            .map(|e| e.unwrap().file_name().into_string().unwrap())
            .filter(|n| n.ends_with(".xml"))
            .collect();
        xml.sort();
        assert_eq!(rust.keys().cloned().collect::<Vec<_>>(), xml);
        let skipped = summary.matches("skipped, not serialisable").count();
        assert_eq!(rust.len() + skipped, fixtures.len(), "{summary}");
        for m in &fixtures {
            let file = format!("{}.xml", file_stem(&m.id));
            if let Some(row) = rust.get(&file) {
                let counts: BTreeMap<String, u64> =
                    serde_json::from_value(row["rules"].clone()).unwrap();
                assert_eq!(
                    counts.values().sum::<u64>() as usize,
                    m.expect.len(),
                    "{}",
                    m.id
                );
                assert_eq!(
                    row["errors"].as_u64().unwrap() as usize,
                    errors_in_expect(m, rs)
                );
                assert_eq!(row["exported"], row["errors"] == 0);
            }
        }
        // A second run replaces the output; a foreign file makes it refuse.
        mutations_cmd(Family::Platform, &dir, rs).unwrap();
        fs::write(dir.join("notes.txt"), "x").unwrap();
        let err = mutations_cmd(Family::Platform, &dir, rs).unwrap_err();
        assert!(err.contains("notes.txt"), "{err}");
        fs::remove_dir_all(&dir).unwrap();
    }
}
