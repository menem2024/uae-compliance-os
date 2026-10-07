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
//! ```text
//! conformance corpus --out <dir> --fuzz 2000 --seed 20260929
//! conformance coverage-md [--check]
//! conformance snapshot [--write]
//! ```
//!
//! `corpus` writes the whole differential corpus (the 30 official examples, every mutation
//! fixture, `--fuzz` seeded random mutations, see `conformance::fuzz`) as `<dir>/examples/*.xml`,
//! `<dir>/mutations/<family>/*.xml`, `<dir>/fuzz/*.xml`, plus `<dir>/rust.json` in the shape
//! `compare.py` reads (here `rules` is the RuleSet's own output, never a fixture's `expect`) and
//! `<dir>/manifest.tsv` (document, origin). A document whose XML cannot be written at all is
//! reported as skipped and left out. `<dir>` may hold only a previous run's output.
//!
//! `coverage-md` writes `rulesets/pint-ae-1.0.4/COVERAGE.md`; with `--check` it writes nothing and
//! fails when the committed file is stale.
//!
//! `snapshot` compares the live results on the corpus with the frozen
//! `snapshot/r<N>.jsonl` of the default RuleSet and fails on any difference; `--write` creates the
//! file and refuses to overwrite one (a frozen snapshot is never edited, a behaviour change bumps
//! the revision).

use std::collections::{BTreeMap, HashSet};
use std::fs;
use std::path::Path;
use std::process::ExitCode;

use serde_json::json;
use sha2::{Digest, Sha256};
use validator_rs::canonical_json::from_canonical_json;
use validator_rs::catalog::Family;
use validator_rs::conformance::{self, Mutation, file_stem};
use validator_rs::doc::Doc;
use validator_rs::export;
use validator_rs::pb;
use validator_rs::ruleset::{self, RuleSet};

const USAGE: &str = "usage:\n  conformance export --in <canonical json> --out <xml>\n  conformance mutations --family <family> --out <dir>\n  conformance corpus --out <dir> --fuzz <n> --seed <n>\n  conformance coverage-md [--check]\n  conformance snapshot [--write]";

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
        Some("corpus") => {
            let o = opts(&args[1..], &["--out", "--fuzz", "--seed"])?;
            let fuzz = o["--fuzz"]
                .parse::<usize>()
                .map_err(|e| format!("--fuzz {:?}: {e}", o["--fuzz"]))?;
            let seed = o["--seed"]
                .parse::<u64>()
                .map_err(|e| format!("--seed {:?}: {e}", o["--seed"]))?;
            corpus_cmd(
                Path::new(&o["--out"]),
                fuzz,
                seed,
                ruleset::default_ruleset(),
            )
        }
        Some("coverage-md") => match &args[1..] {
            [] => coverage_md_cmd(false),
            [flag] if flag == "--check" => coverage_md_cmd(true),
            _ => Err(USAGE.into()),
        },
        Some("snapshot") => match &args[1..] {
            [] => snapshot_cmd(false, ruleset::default_ruleset()),
            [flag] if flag == "--write" => snapshot_cmd(true, ruleset::default_ruleset()),
            _ => Err(USAGE.into()),
        },
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

/// What `corpus` writes into its directory.
const CORPUS_DIRS: [&str; 3] = ["examples", "mutations", "fuzz"];
const CORPUS_FILES: [&str; 4] = ["rust.json", "saxon.json", "xsd.json", "manifest.tsv"];

/// Creates `dir`, or empties it of a previous `corpus` run's output; refuses a directory holding
/// anything else.
fn prepare_corpus_dir(dir: &Path) -> Result<(), String> {
    fs::create_dir_all(dir).map_err(|e| format!("{}: {e}", dir.display()))?;
    let mut stale = Vec::new();
    for entry in fs::read_dir(dir).map_err(|e| format!("{}: {e}", dir.display()))? {
        let path = entry.map_err(|e| e.to_string())?.path();
        let name = path
            .file_name()
            .and_then(|n| n.to_str())
            .unwrap_or_default()
            .to_string();
        let ours = (path.is_dir() && CORPUS_DIRS.contains(&name.as_str()))
            || (path.is_file() && CORPUS_FILES.contains(&name.as_str()));
        if !ours {
            return Err(format!(
                "{}: holds {name:?}, which this command did not write; use an empty directory",
                dir.display()
            ));
        }
        stale.push(path);
    }
    for path in stale {
        let r = if path.is_dir() {
            fs::remove_dir_all(&path)
        } else {
            fs::remove_file(&path)
        };
        r.map_err(|e| format!("{}: {e}", path.display()))?;
    }
    Ok(())
}

fn corpus_cmd(dir: &Path, fuzz: usize, seed: u64, rs: &RuleSet) -> Result<String, String> {
    let docs = conformance::corpus::documents(fuzz, seed, rs)?;
    prepare_corpus_dir(dir)?;
    let mut rust = BTreeMap::new();
    let mut manifest = String::new();
    let mut skipped = Vec::new();
    let (mut exported, mut with_errors) = (0usize, 0usize);
    for d in &docs {
        let xml = match export::to_xml(&Doc::new(&d.invoice)) {
            Ok(xml) => xml,
            Err(e) => {
                skipped.push(format!("{} ({e})", d.name));
                continue;
            }
        };
        let path = dir.join(d.xml_name());
        let parent = path.parent().unwrap_or(dir);
        fs::create_dir_all(parent).map_err(|e| format!("{}: {e}", parent.display()))?;
        fs::write(&path, xml).map_err(|e| format!("{}: {e}", path.display()))?;
        let run = rs.validate(&d.invoice);
        let mut rules: BTreeMap<&str, u32> = BTreeMap::new();
        for issue in &run.issues {
            *rules.entry(issue.rule_id.as_str()).or_insert(0) += 1;
        }
        let errors = run
            .issues
            .iter()
            .filter(|i| i.severity == pb::Severity::Error as i32)
            .count();
        if errors == 0 {
            exported += 1;
        } else {
            with_errors += 1;
        }
        rust.insert(
            d.xml_name(),
            json!({"rules": rules, "errors": errors, "exported": errors == 0}),
        );
        manifest.push_str(&format!(
            "{}\t{}\n",
            d.xml_name(),
            d.note.replace(['\t', '\n'], " ")
        ));
    }
    let path = dir.join("rust.json");
    let text = serde_json::to_string_pretty(&rust).map_err(|e| e.to_string())? + "\n";
    fs::write(&path, text).map_err(|e| format!("{}: {e}", path.display()))?;
    let path = dir.join("manifest.tsv");
    fs::write(&path, manifest).map_err(|e| format!("{}: {e}", path.display()))?;
    let count = |prefix: &str| rust.keys().filter(|k| k.starts_with(prefix)).count();
    let mut summary = format!(
        "{} documents written to {} ({} examples, {} mutation fixtures, {} fuzz; {exported} \
         without errors, {with_errors} with errors)",
        rust.len(),
        dir.display(),
        count("examples/"),
        count("mutations/"),
        count("fuzz/"),
    );
    for s in skipped {
        summary.push_str(&format!("\nskipped, not serialisable: {s}"));
    }
    Ok(summary)
}

fn coverage_md_cmd(check: bool) -> Result<String, String> {
    use validator_rs::conformance::coverage_md;
    if check {
        coverage_md::check()?;
        return Ok(format!("{} is up to date", coverage_md::path().display()));
    }
    let path = coverage_md::path();
    fs::write(&path, coverage_md::render_default())
        .map_err(|e| format!("{}: {e}", path.display()))?;
    Ok(format!("wrote {}", path.display()))
}

fn snapshot_cmd(write: bool, rs: &RuleSet) -> Result<String, String> {
    use validator_rs::conformance::snapshot;
    let path = snapshot::path(rs)?;
    let live = snapshot::render_default(rs)?;
    if write {
        if path.exists() {
            return Err(format!(
                "{} exists: a frozen snapshot is never edited; a behaviour change bumps the \
                 revision and writes the next r<N>.jsonl",
                path.display()
            ));
        }
        let parent = path.parent().unwrap_or(Path::new("."));
        fs::create_dir_all(parent).map_err(|e| format!("{}: {e}", parent.display()))?;
        fs::write(&path, &live).map_err(|e| format!("{}: {e}", path.display()))?;
        return Ok(format!(
            "wrote {} ({} documents)",
            path.display(),
            live.lines().count()
        ));
    }
    let frozen = fs::read_to_string(&path).map_err(|e| format!("{}: {e}", path.display()))?;
    snapshot::compare(&frozen, &live)?;
    Ok(format!(
        "{} matches the live results ({} documents)",
        path.display(),
        live.lines().count()
    ))
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
    fn corpus_writes_every_group_and_replaces_its_own_output() {
        let dir = scratch("corpus");
        let rs = ruleset::default_ruleset();
        let summary = corpus_cmd(&dir, 12, 7, rs).unwrap();
        let rust: BTreeMap<String, serde_json::Value> =
            serde_json::from_str(&fs::read_to_string(dir.join("rust.json")).unwrap()).unwrap();
        let skipped = summary.matches("skipped, not serialisable").count();
        let fixtures: usize = Family::ALL
            .iter()
            .map(|f| conformance::mutations(*f).unwrap().len())
            .sum();
        assert_eq!(rust.len() + skipped, 30 + fixtures + 12, "{summary}");
        let in_group = |p: &str| rust.keys().filter(|k| k.starts_with(p)).count();
        assert_eq!(in_group("examples/"), 30);
        assert_eq!(in_group("fuzz/"), 12);
        for (name, row) in &rust {
            assert!(dir.join(name).is_file(), "{name}");
            assert_eq!(row["exported"], row["errors"] == 0, "{name}");
            let rules = row["rules"].as_object().unwrap();
            // The fuzz corpus never holds a document with an AE-FMT / AE-EXP issue.
            if name.starts_with("fuzz/") {
                assert!(
                    rules
                        .keys()
                        .all(|k| !k.starts_with("AE-FMT-") && !k.starts_with("AE-EXP-")),
                    "{name}"
                );
            }
            if name.starts_with("examples/") {
                assert_eq!(row["errors"], 0, "{name}");
            }
        }
        let manifest = fs::read_to_string(dir.join("manifest.tsv")).unwrap();
        assert_eq!(manifest.lines().count(), rust.len());
        // A second run replaces the output; a foreign file makes it refuse.
        corpus_cmd(&dir, 12, 7, rs).unwrap();
        fs::write(dir.join("notes.txt"), "x").unwrap();
        let err = corpus_cmd(&dir, 12, 7, rs).unwrap_err();
        assert!(err.contains("notes.txt"), "{err}");
        fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn corpus_snapshot_and_coverage_arguments_are_checked() {
        assert!(
            run(&s(&[
                "corpus", "--out", "x", "--fuzz", "many", "--seed", "1"
            ]))
            .is_err()
        );
        assert!(run(&s(&["corpus", "--out", "x", "--fuzz", "1"])).is_err());
        assert!(run(&s(&["coverage-md", "--nope"])).is_err());
        assert!(run(&s(&["snapshot", "--nope"])).is_err());
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
