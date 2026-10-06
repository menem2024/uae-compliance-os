//! Dev CLI: validate a canonical-invoice JSON file (or stdin) with the real rule engine.
//!
//!   cargo run --example validate -- path/to/invoice.json [ruleset-version]
//!
//! Prints one line per finding: `<severity> <rule_id> <path> <message>`; exit 0 whether or not there are findings.
use std::io::Read;

fn main() {
    let mut args = std::env::args().skip(1);
    let path = args.next().unwrap_or_else(|| "-".into());
    let version = args
        .next()
        .unwrap_or_else(|| validator_rs::ruleset::default_ruleset().id().to_string());
    let mut text = String::new();
    if path == "-" {
        std::io::stdin()
            .read_to_string(&mut text)
            .expect("read stdin");
    } else {
        text = std::fs::read_to_string(&path).expect("read file");
    }
    let inv = validator_rs::canonical_json::from_canonical_json(&text).expect("canonical JSON");
    let run = validator_rs::ruleset::validate(&version, &inv).expect("known ruleset");
    println!(
        "ruleset {} issues {}",
        run.ruleset_version,
        run.issues.len()
    );
    for i in &run.issues {
        println!(
            "{:?} {} {} [{}] {} | ar: {} | suggest: {}",
            i.severity(),
            i.rule_id,
            i.path,
            i.business_term,
            i.message,
            i.message_ar,
            i.suggested_value
        );
    }
}
