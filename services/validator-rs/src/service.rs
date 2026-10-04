use crate::doc::Doc;
use crate::{export, pb, ruleset};
use pb::export_service_server::ExportServiceServer;
use pb::validator_service_server::ValidatorServiceServer;
use sha2::{Digest, Sha256};
use tonic::{Request, Response, Status};

/// Decoding and encoding limit of both services (spec 5.3.2): tonic's 4 MiB default is too
/// small for a 5,000-line invoice.
pub const MAX_MESSAGE_SIZE: usize = 16 << 20;

/// `ValidatorService` with the 16 MiB limits, as `main` serves it.
pub fn validator_server() -> ValidatorServiceServer<Validator> {
    ValidatorServiceServer::new(Validator)
        .max_decoding_message_size(MAX_MESSAGE_SIZE)
        .max_encoding_message_size(MAX_MESSAGE_SIZE)
}

/// `ExportService` with the 16 MiB limits, as `main` serves it.
pub fn export_server() -> ExportServiceServer<Exporter> {
    ExportServiceServer::new(Exporter)
        .max_decoding_message_size(MAX_MESSAGE_SIZE)
        .max_encoding_message_size(MAX_MESSAGE_SIZE)
}

#[derive(Default)]
pub struct Exporter;

#[tonic::async_trait]
impl pb::export_service_server::ExportService for Exporter {
    /// Validates, then exports only when the run has no error issue (`export::export`, spec
    /// 5.3.2). A missing invoice or an unknown `ruleset_version` is `INVALID_ARGUMENT`.
    #[tracing::instrument(name = "Export", skip_all)]
    async fn export(
        &self,
        req: Request<pb::ExportRequest>,
    ) -> Result<Response<pb::ExportResponse>, Status> {
        let req = req.into_inner();
        let inv = req
            .invoice
            .ok_or_else(|| Status::invalid_argument("invoice is required"))?;
        let rs = ruleset::get(&req.ruleset_version)
            .map_err(|e| Status::invalid_argument(e.to_string()))?;
        let document_kind = export::document_kind(Doc::new(&inv).kind);
        let outcome = export::export(&inv, rs);
        let (xml, sha256) = match outcome.xml {
            Some(xml) => {
                let digest = hex::encode(Sha256::digest(&xml));
                (xml, digest)
            }
            None => (Vec::new(), String::new()),
        };
        let exported = !sha256.is_empty();
        tracing::info!(
            ruleset = %outcome.run.ruleset_version,
            issues = outcome.run.issues.len(),
            exported,
            document_kind,
            bytes = xml.len(),
            "exported"
        );
        Ok(Response::new(pb::ExportResponse {
            xml,
            sha256,
            format: export::FORMAT.to_string(),
            run: Some(outcome.run),
            exported,
            document_kind: document_kind.to_string(),
        }))
    }
}

#[derive(Default)]
pub struct Validator;

#[tonic::async_trait]
impl pb::validator_service_server::ValidatorService for Validator {
    #[tracing::instrument(name = "Validate", skip_all)]
    async fn validate(
        &self,
        req: Request<pb::ValidateRequest>,
    ) -> Result<Response<pb::ValidateResponse>, Status> {
        let req = req.into_inner();
        let inv = req
            .invoice
            .ok_or_else(|| Status::invalid_argument("invoice is required"))?;
        let run = ruleset::validate(&req.ruleset_version, &inv)
            .map_err(|e| Status::invalid_argument(e.to_string()))?;
        tracing::info!(
            ruleset = %run.ruleset_version,
            issues = run.issues.len(),
            duration_us = run.duration_us,
            "validated"
        );
        Ok(Response::new(pb::ValidateResponse { run: Some(run) }))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use pb::validator_service_server::ValidatorService;

    async fn call(
        invoice: Option<pb::Invoice>,
        version: &str,
    ) -> Result<pb::ValidationRun, Status> {
        Validator
            .validate(Request::new(pb::ValidateRequest {
                invoice,
                ruleset_version: version.into(),
            }))
            .await
            .map(|resp| resp.into_inner().run.expect("a run"))
    }

    #[tokio::test]
    async fn missing_invoice_is_invalid_argument() {
        let err = call(None, "").await.unwrap_err();
        assert_eq!(err.code(), tonic::Code::InvalidArgument);
    }

    #[tokio::test]
    async fn unknown_ruleset_version_is_invalid_argument() {
        for v in ["pint-ae@0.0-skeleton", "pint-ae@9.9+r1"] {
            let err = call(Some(pb::Invoice::default()), v).await.unwrap_err();
            assert_eq!(err.code(), tonic::Code::InvalidArgument);
            assert_eq!(err.message(), format!("unknown ruleset_version \"{v}\""));
        }
    }

    #[tokio::test]
    async fn empty_and_explicit_versions_run_the_default_ruleset() {
        for v in ["", "pint-ae@1.0.4+r1"] {
            let run = call(Some(pb::Invoice::default()), v).await.unwrap();
            assert_eq!(run.ruleset_version, "pint-ae@1.0.4+r1");
            assert!(run.rules_evaluated >= 2);
        }
    }

    /// The Phase 0 rule `AE-TRN-001` is retired: a clean official example with one malformed
    /// decimal reports a registered PINT-AE RuleSet rule at a CI section 12 path.
    #[tokio::test]
    async fn returns_run_with_real_rule_issues() {
        let (_, mut inv) = crate::conformance::examples()
            .into_iter()
            .find(|(slug, _)| slug == "standard-tax-invoice")
            .unwrap();
        inv.references.get_or_insert_default().contract_value = "AED200000".into();
        let run = call(Some(inv), "").await.unwrap();
        assert!(run.issues.iter().all(|i| i.rule_id != "AE-TRN-001"));
        let first = &run.issues[0];
        assert_eq!(first.rule_id, "AE-FMT-001");
        assert_eq!(first.severity, pb::Severity::Error as i32);
        assert_eq!(first.path, "references.contract_value");
        assert_eq!(first.business_term, "BTAE-05");
        assert_eq!(
            first.message_args.get("term").map(String::as_str),
            Some("BTAE-05")
        );
        assert!(
            first
                .message
                .starts_with("BTAE-05 must be a decimal number")
        );
        assert!(first.message_ar.contains("BTAE-05"));
        assert!(first.fixable);
    }

    fn example(slug: &str) -> pb::Invoice {
        crate::conformance::examples()
            .into_iter()
            .find(|(s, _)| s == slug)
            .unwrap()
            .1
    }

    async fn export(
        invoice: Option<pb::Invoice>,
        version: &str,
    ) -> Result<pb::ExportResponse, Status> {
        use pb::export_service_server::ExportService;
        Exporter
            .export(Request::new(pb::ExportRequest {
                invoice,
                ruleset_version: version.into(),
            }))
            .await
            .map(Response::into_inner)
    }

    #[tokio::test]
    async fn export_rejects_a_missing_invoice_and_an_unknown_ruleset() {
        let err = export(None, "").await.unwrap_err();
        assert_eq!(err.code(), tonic::Code::InvalidArgument);
        for v in ["pint-ae@0.0-skeleton", "pint-ae@9.9+r1"] {
            let err = export(Some(example("standard-tax-invoice")), v)
                .await
                .unwrap_err();
            assert_eq!(err.code(), tonic::Code::InvalidArgument);
            assert_eq!(err.message(), format!("unknown ruleset_version \"{v}\""));
        }
    }

    /// The golden files are the exported bytes; `sha256` is their lowercase hex digest.
    #[tokio::test]
    async fn a_document_without_errors_is_exported_with_its_digest() {
        use sha2::{Digest, Sha256};
        let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
        for (slug, kind) in [
            ("standard-tax-invoice", "invoice"),
            ("standard-tax-credit-note", "credit_note"),
        ] {
            for version in ["", "pint-ae@1.0.4+r1"] {
                let resp = export(Some(example(slug)), version).await.unwrap();
                let golden = std::fs::read(root.join(format!("tests/golden/{slug}.xml"))).unwrap();
                assert!(resp.exported, "{slug}");
                assert!(resp.xml == golden, "{slug}");
                assert_eq!(resp.sha256, hex::encode(Sha256::digest(&golden)));
                assert!(
                    resp.sha256.len() == 64
                        && resp
                            .sha256
                            .chars()
                            .all(|c| c.is_ascii_digit() || ('a'..='f').contains(&c))
                );
                assert_eq!(resp.format, "pint-ae-billing-1.0.4/ubl-2.1");
                assert_eq!(resp.document_kind, kind);
                let run = resp.run.unwrap();
                assert_eq!(run.ruleset_version, "pint-ae@1.0.4+r1");
                assert!(run.issues.is_empty());
            }
        }
    }

    #[tokio::test]
    async fn a_document_with_an_error_issue_is_not_exported() {
        let mut inv = example("standard-tax-credit-note");
        inv.references.get_or_insert_default().contract_value = "AED200000".into();
        let resp = export(Some(inv), "").await.unwrap();
        assert!(!resp.exported);
        assert!(resp.xml.is_empty() && resp.sha256.is_empty());
        assert_eq!(resp.format, "pint-ae-billing-1.0.4/ubl-2.1");
        assert_eq!(resp.document_kind, "credit_note");
        let run = resp.run.unwrap();
        assert_eq!(run.issues.len(), 1);
        assert_eq!(run.issues[0].rule_id, "AE-FMT-001");
    }

    /// `AE-EXP-001` is a warning: the credit note is exported without its due date.
    #[tokio::test]
    async fn warnings_do_not_block_the_export() {
        let mut inv = example("standard-tax-credit-note");
        inv.payment_due_date = "2025-03-01".into();
        let resp = export(Some(inv), "").await.unwrap();
        let run = resp.run.unwrap();
        assert_eq!(run.issues.len(), 1);
        assert_eq!(run.issues[0].rule_id, "AE-EXP-001");
        assert_eq!(run.issues[0].severity, pb::Severity::Warning as i32);
        assert!(resp.exported && !resp.xml.is_empty());
    }

    /// Both services, served as `main` serves them, accept and return messages above tonic's
    /// 4 MiB default and refuse requests above 16 MiB (spec 5.3.2).
    #[tokio::test(flavor = "multi_thread")]
    async fn both_services_carry_messages_up_to_16_mib() {
        use prost::Message;
        use tonic::codegen::http::uri::PathAndQuery;
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let server = tokio::spawn(
            tonic::transport::Server::builder()
                .add_service(validator_server())
                .add_service(export_server())
                .serve_with_incoming(tonic::transport::server::TcpIncoming::from(listener)),
        );
        let channel = tonic::transport::Endpoint::from_shared(format!("http://{addr}"))
            .unwrap()
            .connect()
            .await
            .unwrap();
        let client = |limit: usize| {
            tonic::client::Grpc::new(channel.clone())
                .max_decoding_message_size(limit)
                .max_encoding_message_size(limit)
        };
        let with_note = |bytes: usize| {
            let mut inv = example("standard-tax-invoice");
            inv.note = "A".repeat(bytes);
            inv
        };
        let big = with_note(6 << 20);
        assert!(big.encoded_len() > 4 << 20 && big.encoded_len() < MAX_MESSAGE_SIZE);

        let mut grpc = client(MAX_MESSAGE_SIZE);
        grpc.ready().await.unwrap();
        let resp: pb::ExportResponse = grpc
            .unary(
                Request::new(pb::ExportRequest {
                    invoice: Some(big.clone()),
                    ruleset_version: String::new(),
                }),
                PathAndQuery::from_static("/compliance.v1.ExportService/Export"),
                tonic_prost::ProstCodec::default(),
            )
            .await
            .unwrap()
            .into_inner();
        assert!(resp.exported && resp.xml.len() > 6 << 20);

        grpc.ready().await.unwrap();
        let resp: pb::ValidateResponse = grpc
            .unary(
                Request::new(pb::ValidateRequest {
                    invoice: Some(big),
                    ruleset_version: String::new(),
                }),
                PathAndQuery::from_static("/compliance.v1.ValidatorService/Validate"),
                tonic_prost::ProstCodec::default(),
            )
            .await
            .unwrap()
            .into_inner();
        assert!(resp.run.unwrap().issues.is_empty());

        let mut unlimited = client(usize::MAX);
        for path in [
            "/compliance.v1.ValidatorService/Validate",
            "/compliance.v1.ExportService/Export",
        ] {
            unlimited.ready().await.unwrap();
            let request = pb::ValidateRequest {
                invoice: Some(with_note(MAX_MESSAGE_SIZE)),
                ruleset_version: String::new(),
            };
            let err = unlimited
                .unary::<_, pb::ValidateResponse, _>(
                    Request::new(request),
                    PathAndQuery::from_static(path),
                    tonic_prost::ProstCodec::default(),
                )
                .await
                .unwrap_err();
            assert_eq!(err.code(), tonic::Code::OutOfRange, "{path}: {err}");
        }
        server.abort();
    }
}
