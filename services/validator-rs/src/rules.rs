use crate::pb;

pub const RULESET_VERSION: &str = "pint-ae@0.0-skeleton";

pub fn validate(inv: &pb::Invoice) -> pb::ValidationRun {
    let mut issues = Vec::new();
    let trn = inv.seller_trn.as_str();
    if !(trn.len() == 15 && trn.bytes().all(|b| b.is_ascii_digit())) {
        issues.push(pb::ValidationIssue {
            rule_id: "AE-TRN-001".into(),
            severity: pb::Severity::Error as i32,
            path: "invoice.seller_trn".into(),
            message: "Seller TRN must be exactly 15 digits".into(),
        });
    }
    pb::ValidationRun {
        ruleset_version: RULESET_VERSION.into(),
        issues,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn inv(trn: &str) -> pb::Invoice {
        pb::Invoice {
            seller_trn: trn.to_string(),
            ..Default::default()
        }
    }

    #[test]
    fn fifteen_digits_passes() {
        let run = validate(&inv("100000000000003"));
        assert_eq!(run.ruleset_version, RULESET_VERSION);
        assert!(run.issues.is_empty());
    }

    #[test]
    fn bad_trns_fail_with_ae_trn_001() {
        for trn in [
            "",
            "123",
            "10000000000000",
            "1000000000000030",
            "10000000000000A",
            "١٠٠٠٠٠٠٠٠٠٠٠٠٠٣",
        ] {
            let run = validate(&inv(trn));
            assert_eq!(run.issues.len(), 1, "trn={trn:?}");
            let i = &run.issues[0];
            assert_eq!(i.rule_id, "AE-TRN-001");
            assert_eq!(i.severity, pb::Severity::Error as i32);
            assert_eq!(i.path, "invoice.seller_trn");
        }
    }
}
