use prost::Message;
use std::{env, fs, path::PathBuf};

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let fds = protox::compile(
        [
            "compliance/v1/invoice.proto",
            "compliance/v1/validator.proto",
            "compliance/v1/export.proto",
        ],
        ["../../proto"],
    )?;

    let out_dir = PathBuf::from(env::var("OUT_DIR")?);
    fs::write(
        out_dir.join("compliance_descriptor.bin"),
        fds.encode_to_vec(),
    )?;

    tonic_prost_build::configure()
        .build_client(false)
        .compile_fds(fds)?;
    println!("cargo:rerun-if-changed=../../proto");
    Ok(())
}
