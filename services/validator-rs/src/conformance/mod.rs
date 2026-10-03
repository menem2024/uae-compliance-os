//! Test support for the differential conformance CLI (`bin/conformance.rs`).
//!
//! * [`ubl_import`]: official UBL XML to canonical `pb::Invoice`;
//! * [`patch`]: applies a mutation fixture's `set` / `remove` paths;
//! * [`corpus`]: the 30 official examples as committed canonical JSON (Task 5); mutation
//!   fixtures and the fuzz generator arrive with Task 15.

pub mod corpus;
pub mod patch;
pub mod ubl_import;

pub use corpus::examples;
pub use patch::{PatchError, apply as apply_patch, path_cmp};
pub use ubl_import::{ImportError, ImportReport, from_xml};
