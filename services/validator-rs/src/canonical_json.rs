//! Canonical JSON (protojson with proto field names) for `pb::Invoice` (CI rule 11).
//!
//! Writers omit unpopulated fields and use proto names (`UseProtoNames`); readers accept both
//! proto and JSON names and reject unknown fields, so a typo in a stored payload or a fixture
//! is an error instead of a silently dropped value.

use std::sync::OnceLock;

use prost::Message;
use prost_reflect::{
    DescriptorPool, DeserializeOptions, DynamicMessage, MessageDescriptor, SerializeOptions,
};

use crate::{FILE_DESCRIPTOR_SET, pb};

/// Error from reading canonical JSON.
#[derive(Debug)]
pub struct Error(pub String);

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "canonical json: {}", self.0)
    }
}

impl std::error::Error for Error {}

fn descriptor() -> &'static MessageDescriptor {
    static D: OnceLock<MessageDescriptor> = OnceLock::new();
    D.get_or_init(|| {
        DescriptorPool::decode(FILE_DESCRIPTOR_SET)
            .expect("embedded descriptor set decodes")
            .get_message_by_name("compliance.v1.Invoice")
            .expect("compliance.v1.Invoice is in the descriptor set")
    })
}

fn to_dynamic(inv: &pb::Invoice) -> DynamicMessage {
    DynamicMessage::decode(descriptor().clone(), inv.encode_to_vec().as_slice())
        .expect("a prost message re-decodes as its own descriptor")
}

fn options() -> SerializeOptions {
    SerializeOptions::new()
        .use_proto_field_name(true)
        .skip_default_fields(true)
        .stringify_64_bit_integers(true)
}

/// Compact protojson with proto field names, unpopulated fields omitted, fields in
/// field-number order.
pub fn to_canonical_json(inv: &pb::Invoice) -> String {
    let mut out = Vec::new();
    let mut ser = serde_json::Serializer::new(&mut out);
    to_dynamic(inv)
        .serialize_with_options(&mut ser, &options())
        .expect("serialising a dynamic message to a Vec cannot fail");
    String::from_utf8(out).expect("JSON is UTF-8")
}

/// Same content as [`to_canonical_json`], indented with two spaces and a trailing newline;
/// used for the committed corpus files so diffs are readable.
pub fn to_canonical_json_pretty(inv: &pb::Invoice) -> String {
    let mut out = Vec::new();
    let mut ser = serde_json::Serializer::pretty(&mut out);
    to_dynamic(inv)
        .serialize_with_options(&mut ser, &options())
        .expect("serialising a dynamic message to a Vec cannot fail");
    let mut s = String::from_utf8(out).expect("JSON is UTF-8");
    s.push('\n');
    s
}

/// Parses canonical JSON. Accepts proto and JSON field names; rejects unknown fields.
pub fn from_canonical_json(s: &str) -> Result<pb::Invoice, Error> {
    let mut de = serde_json::Deserializer::from_str(s);
    let dm = DynamicMessage::deserialize_with_options(
        descriptor().clone(),
        &mut de,
        &DeserializeOptions::new().deny_unknown_fields(true),
    )
    .map_err(|e| Error(e.to_string()))?;
    de.end().map_err(|e| Error(e.to_string()))?;
    dm.transcode_to::<pb::Invoice>()
        .map_err(|e| Error(e.to_string()))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample() -> pb::Invoice {
        pb::Invoice {
            invoice_number: "INV-1".into(),
            issue_date: "2026-01-02".into(),
            seller_trn: "100000000000003".into(),
            total_amount: "105.00".into(),
            vat_amount: "5.00".into(),
            currency: "AED".into(),
            seller: Some(pb::Party {
                name: "Seller".into(),
                postal_address: Some(pb::PostalAddress {
                    country_code: "AE".into(),
                    ..Default::default()
                }),
                identifiers: vec![pb::Identifier {
                    id: "1".into(),
                    scheme_id: "0235".into(),
                }],
                ..Default::default()
            }),
            lines: vec![
                pb::InvoiceLine {
                    id: "1".into(),
                    net_amount: "100.00".into(),
                    ..Default::default()
                },
                pb::InvoiceLine {
                    id: "2".into(),
                    allowances_charges: vec![pb::AllowanceCharge {
                        is_charge: true,
                        amount: "1".into(),
                        ..Default::default()
                    }],
                    ..Default::default()
                },
            ],
            ..Default::default()
        }
    }

    #[test]
    fn round_trips_a_hand_built_invoice() {
        let inv = sample();
        let json = to_canonical_json(&inv);
        assert_eq!(from_canonical_json(&json).unwrap(), inv);
        let pretty = to_canonical_json_pretty(&inv);
        assert_eq!(from_canonical_json(&pretty).unwrap(), inv);
    }

    #[test]
    fn writes_proto_names_and_omits_unpopulated_fields() {
        let json = to_canonical_json(&sample());
        assert!(json.contains("\"invoice_number\":\"INV-1\""), "{json}");
        assert!(json.contains("\"is_charge\":true"), "{json}");
        assert!(json.contains("\"postal_address\""), "{json}");
        assert!(!json.contains("invoiceNumber"), "{json}");
        assert!(!json.contains("uuid"), "{json}");
        assert!(!json.contains("\"is_charge\":false"), "{json}");
        assert_eq!(to_canonical_json(&pb::Invoice::default()), "{}");
    }

    #[test]
    fn accepts_json_names_too() {
        let inv = from_canonical_json(r#"{"invoiceNumber":"A","seller_trn":"B"}"#).unwrap();
        assert_eq!(inv.invoice_number, "A");
        assert_eq!(inv.seller_trn, "B");
    }

    #[test]
    fn rejects_unknown_fields_at_every_depth() {
        assert!(from_canonical_json(r#"{"invoice_numbr":"A"}"#).is_err());
        assert!(from_canonical_json(r#"{"seller":{"nme":"A"}}"#).is_err());
        assert!(from_canonical_json(r#"{"lines":[{"id":"1","bogus":"x"}]}"#).is_err());
    }

    #[test]
    fn rejects_wrong_types_and_trailing_garbage() {
        assert!(from_canonical_json(r#"{"invoice_number":5}"#).is_err());
        assert!(from_canonical_json(r#"{} {}"#).is_err());
        assert!(from_canonical_json("not json").is_err());
    }
}
