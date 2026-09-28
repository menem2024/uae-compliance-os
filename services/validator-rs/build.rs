fn main() -> Result<(), Box<dyn std::error::Error>> {
    let fds = protox::compile(
        [
            "compliance/v1/invoice.proto",
            "compliance/v1/validator.proto",
        ],
        ["../../proto"],
    )?;
    tonic_prost_build::configure()
        .build_client(false)
        .compile_fds(fds)?;
    println!("cargo:rerun-if-changed=../../proto");
    Ok(())
}
