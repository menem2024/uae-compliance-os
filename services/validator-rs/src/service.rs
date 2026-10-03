use crate::{pb, rules::skeleton};
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
        let inv = req
            .into_inner()
            .invoice
            .ok_or_else(|| Status::invalid_argument("invoice is required"))?;
        let run = skeleton::validate(&inv);
        tracing::info!(issues = run.issues.len(), "validated");
        Ok(Response::new(pb::ValidateResponse { run: Some(run) }))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use pb::validator_service_server::ValidatorService;

    #[tokio::test]
    async fn missing_invoice_is_invalid_argument() {
        let err = Validator
            .validate(Request::new(pb::ValidateRequest {
                invoice: None,
                ..Default::default()
            }))
            .await
            .unwrap_err();
        assert_eq!(err.code(), tonic::Code::InvalidArgument);
    }

    #[tokio::test]
    async fn returns_run() {
        let inv = pb::Invoice {
            seller_trn: "123".into(),
            ..Default::default()
        };
        let resp = Validator
            .validate(Request::new(pb::ValidateRequest {
                invoice: Some(inv),
                ..Default::default()
            }))
            .await
            .unwrap();
        assert_eq!(
            resp.into_inner().run.unwrap().issues[0].rule_id,
            "AE-TRN-001"
        );
    }
}
