use crate::{pb, ruleset};
use tonic::{Request, Response, Status};

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
}
