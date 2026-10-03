pub mod pb {
    tonic::include_proto!("compliance.v1");
}

/// Encoded `FileDescriptorSet` for `pb`, written by `build.rs` for `prost-reflect`
/// (canonical JSON conversion and path-template checks).
pub const FILE_DESCRIPTOR_SET: &[u8] =
    include_bytes!(concat!(env!("OUT_DIR"), "/compliance_descriptor.bin"));

pub mod canonical_json;
pub mod catalog;
pub mod codelists;
pub mod conformance;
pub mod decimal;
pub mod doc;
pub mod rule;
pub mod rules;
pub mod ruleset;
pub mod service;
pub mod telemetry;
