pub mod pb {
    tonic::include_proto!("compliance.v1");
}
mod rules;
mod service;
mod telemetry;

use std::time::Duration;

/// A shutdown timeout for flushing the OTel tracer provider once the server
/// has stopped accepting new connections. Bounded so an unreachable OTLP
/// endpoint cannot stall process exit past the container runtime's grace
/// period.
const TELEMETRY_SHUTDOWN_TIMEOUT: Duration = Duration::from_secs(3);

/// Resolves on the first SIGINT or SIGTERM, whichever arrives first, so
/// `docker stop` (which sends SIGTERM) triggers the same graceful shutdown
/// path as Ctrl-C during local development.
async fn shutdown_signal() {
    let ctrl_c = async {
        tokio::signal::ctrl_c()
            .await
            .expect("failed to install SIGINT handler");
    };

    let terminate = async {
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
            .expect("failed to install SIGTERM handler")
            .recv()
            .await;
    };

    tokio::select! {
        _ = ctrl_c => tracing::info!("received SIGINT, shutting down"),
        _ = terminate => tracing::info!("received SIGTERM, shutting down"),
    }
}

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
        .serve_with_shutdown(addr, shutdown_signal())
        .await?;

    // `SdkTracerProvider::shutdown` is a blocking call that flushes
    // outstanding spans to the OTLP endpoint. Run it on a blocking thread
    // and bound the wait: if the endpoint is unreachable, export can stall,
    // and we still need to exit promptly under the runtime's SIGTERM grace
    // period.
    let flush = tokio::task::spawn_blocking(move || provider.shutdown());
    match tokio::time::timeout(TELEMETRY_SHUTDOWN_TIMEOUT, flush).await {
        Ok(Ok(Ok(()))) => tracing::info!("telemetry provider shut down cleanly"),
        Ok(Ok(Err(err))) => tracing::warn!(%err, "telemetry provider shutdown reported an error"),
        Ok(Err(err)) => tracing::warn!(%err, "telemetry provider shutdown task panicked"),
        Err(_) => tracing::warn!(
            timeout_secs = TELEMETRY_SHUTDOWN_TIMEOUT.as_secs(),
            "telemetry provider shutdown timed out; exiting anyway"
        ),
    }

    Ok(())
}
