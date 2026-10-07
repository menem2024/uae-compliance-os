//! Informative benchmarks (the gate is `tests/perf_p99.rs`): `validate/<slug>` and `export/<slug>`
//! for the standard official example and for synthetic invoices of 1, 10, 100, 500 and 1,000 lines.
//! Run with `cargo bench --bench validate`.

use criterion::{Criterion, criterion_group, criterion_main};
use std::hint::black_box;
use validator_rs::conformance::corpus;
use validator_rs::export;
use validator_rs::pb;
use validator_rs::ruleset::default_ruleset;

fn cases() -> Vec<(String, pb::Invoice)> {
    let mut out: Vec<(String, pb::Invoice)> = corpus::examples()
        .into_iter()
        .filter(|(slug, _)| slug == "standard-tax-invoice")
        .collect();
    for n in [1usize, 10, 100, 500, 1000] {
        out.push((format!("synthetic-{n}"), corpus::synthetic(n, 20260929)));
    }
    out
}

fn bench_validate(c: &mut Criterion) {
    let rs = default_ruleset();
    let mut group = c.benchmark_group("validate");
    for (slug, inv) in cases() {
        group.bench_function(slug, |b| b.iter(|| black_box(rs.validate(black_box(&inv)))));
    }
    group.finish();
}

fn bench_export(c: &mut Criterion) {
    let rs = default_ruleset();
    let mut group = c.benchmark_group("export");
    for (slug, inv) in cases() {
        group.bench_function(slug, |b| {
            b.iter(|| black_box(export::export(black_box(&inv), rs)))
        });
    }
    group.finish();
}

criterion_group!(benches, bench_validate, bench_export);
criterion_main!(benches);
