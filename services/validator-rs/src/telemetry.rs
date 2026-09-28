use std::future::Future;
use std::pin::Pin;
use std::task::{Context as TaskContext, Poll};

use http::{Request, Response};
use opentelemetry::global;
use opentelemetry::trace::TracerProvider as _;
use opentelemetry_http::HeaderExtractor;
use opentelemetry_otlp::{SpanExporter, WithExportConfig};
use opentelemetry_sdk::Resource;
use opentelemetry_sdk::propagation::TraceContextPropagator;
use opentelemetry_sdk::trace::SdkTracerProvider;
use tower::{Layer, Service};
use tracing::Instrument;
use tracing_opentelemetry::OpenTelemetrySpanExt;
use tracing_subscriber::EnvFilter;
use tracing_subscriber::layer::SubscriberExt;
use tracing_subscriber::util::SubscriberInitExt;

/// Installs the global W3C trace-context propagator, wires an OTLP gRPC span
/// exporter to `OTEL_EXPORTER_OTLP_ENDPOINT`, and registers a tracing
/// subscriber that combines JSON logs with an OpenTelemetry span layer.
///
/// Returns the `SdkTracerProvider` so callers can flush/shutdown it on exit.
pub fn init(service_name: &str) -> Result<SdkTracerProvider, Box<dyn std::error::Error>> {
    global::set_text_map_propagator(TraceContextPropagator::new());

    let endpoint = std::env::var("OTEL_EXPORTER_OTLP_ENDPOINT")
        .unwrap_or_else(|_| "http://localhost:4317".to_string());
    let exporter = SpanExporter::builder()
        .with_tonic()
        .with_endpoint(endpoint)
        .build()?;

    let resource = Resource::builder()
        .with_service_name(service_name.to_string())
        .build();

    let provider = SdkTracerProvider::builder()
        .with_batch_exporter(exporter)
        .with_resource(resource)
        .build();

    global::set_tracer_provider(provider.clone());
    let tracer = provider.tracer(service_name.to_string());

    tracing_subscriber::registry()
        .with(EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")))
        .with(tracing_subscriber::fmt::layer().json())
        .with(tracing_opentelemetry::layer().with_tracer(tracer))
        .try_init()?;

    Ok(provider)
}

/// A `tower::Layer` that extracts an incoming W3C `traceparent` header (if
/// present) and makes it the parent of a `grpc.request` span wrapping the
/// inner service call, so the `Validate` span nests under the caller's span.
#[derive(Clone, Default)]
pub struct TraceContextLayer;

impl<S> Layer<S> for TraceContextLayer {
    type Service = TraceContextService<S>;

    fn layer(&self, inner: S) -> Self::Service {
        TraceContextService { inner }
    }
}

#[derive(Clone)]
pub struct TraceContextService<S> {
    inner: S,
}

impl<S, ReqBody, ResBody> Service<Request<ReqBody>> for TraceContextService<S>
where
    S: Service<Request<ReqBody>, Response = Response<ResBody>> + Clone + Send + 'static,
    S::Future: Send + 'static,
    ReqBody: Send + 'static,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = Pin<Box<dyn Future<Output = Result<Self::Response, Self::Error>> + Send>>;

    fn poll_ready(&mut self, cx: &mut TaskContext<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: Request<ReqBody>) -> Self::Future {
        let parent_cx =
            global::get_text_map_propagator(|p| p.extract(&HeaderExtractor(req.headers())));
        let span = tracing::info_span!(
            "grpc.request",
            otel.kind = "server",
            rpc.method = %req.uri().path(),
        );
        if let Err(err) = span.set_parent(parent_cx) {
            tracing::warn!(%err, "failed to set span parent from traceparent header");
        }

        let clone = self.inner.clone();
        let mut inner = std::mem::replace(&mut self.inner, clone);
        Box::pin(async move { inner.call(req).await }.instrument(span))
    }
}
