//! Performance benchmark for `RuleSet::validate` (E4, p99 < 5 ms). Stub for Task 1;
//! filled in by Task 16 against the real RuleSet and corpus.

use criterion::{Criterion, criterion_group, criterion_main};

fn bench_stub(c: &mut Criterion) {
    c.bench_function("stub", |b| b.iter(|| 1 + 1));
}

criterion_group!(benches, bench_stub);
criterion_main!(benches);
