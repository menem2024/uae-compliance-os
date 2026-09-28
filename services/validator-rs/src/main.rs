pub mod pb {
    tonic::include_proto!("compliance.v1");
}
mod rules;
mod service;
mod telemetry;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let provider = telemetry::init("validator-rs")?;

    let (health_reporter, health_service) = tonic_health::server::health_reporter();
    health_reporter
        .set_serving::<pb::validator_service_server::ValidatorServiceServer<service::Validator>>()
        .await;

    let addr = "0.0.0.0:50051".parse()?;
    tracing::info!(%addr, "validator-rs listening");

    tonic::transport::Server::builder()
        .layer(telemetry::TraceContextLayer)
        .add_service(health_service)
        .add_service(pb::validator_service_server::ValidatorServiceServer::new(
            service::Validator,
        ))
        .serve_with_shutdown(addr, async {
            tokio::signal::ctrl_c().await.ok();
        })
        .await?;

    provider.shutdown()?;
    Ok(())
}
