//! `parties` family rules (spec 5.2.4): the seller, buyer, payee and tax representative of
//! PINT-AE Billing 1.0.4, and the scheme-specific identifier formats that apply to every
//! endpoint, party identifier and legal registration identifier.
//!
//! The coverage list is `rulesets/pint-ae-1.0.4/coverage/parties.tsv` (71 rows: 50 `implemented`,
//! 18 `structural`, 3 `upstream_noop`); the failing fixtures are
//! `rulesets/pint-ae-1.0.4/mutations/parties.jsonl`, whose `expect` lists come from the official
//! schematron, not from this code.
//!
//! Each rule reads the party as the exporter writes it ([`PartyXml`]): an empty or blank field is
//! an element that is not written, so "the element exists" is "the field has text", and a
//! `cac:Party` exists only when at least one of its children is written. That is why a rule whose
//! context is a party does not fire on a seller with no content at all. Three behaviours of the
//! compiled official XSLT are reproduced on purpose and recorded in the coverage notes:
//!
//! * a node matched by several templates of one mode is handled by the highest-priority one only,
//!   so `ibr-128-ae` (context `cac:PostalAddress`) never sees the seller and buyer addresses
//!   (`ibr-143-ae` and `ibr-144-ae` take them) but does see the tax representative's;
//! * a template whose context is an attribute is never applied (the XSLT visits elements only), so
//!   `ibr-011-ae` and `ibr-013-ae` can never fire (`upstream_noop`);
//! * `a != b` and `a = b` are existential over sequences, so an empty side is `false`
//!   (`ibr-176-ae`, `ibr-017`, `ibr-149-ae`).

use std::sync::LazyLock;

use regex::Regex;

use crate::codelists::sets;
use crate::doc::{Doc, text};
use crate::pb;
use crate::rule::{Rule, Sink};

pub static RULES: &[Rule] = &[
    Rule {
        id: "ibr-006",
        check: ibr_006,
    },
    Rule {
        id: "ibr-007",
        check: ibr_007,
    },
    Rule {
        id: "ibr-007-ae",
        check: ibr_007_ae,
    },
    Rule {
        id: "ibr-008",
        check: ibr_008,
    },
    Rule {
        id: "ibr-009",
        check: ibr_009,
    },
    Rule {
        id: "ibr-010",
        check: ibr_010,
    },
    Rule {
        id: "ibr-010-ae",
        check: ibr_010_ae,
    },
    Rule {
        id: "ibr-011",
        check: ibr_011,
    },
    Rule {
        id: "ibr-012-ae",
        check: ibr_012_ae,
    },
    Rule {
        id: "ibr-017",
        check: ibr_017,
    },
    Rule {
        id: "ibr-018",
        check: ibr_018,
    },
    Rule {
        id: "ibr-019",
        check: ibr_019,
    },
    Rule {
        id: "ibr-020",
        check: ibr_020,
    },
    Rule {
        id: "ibr-056",
        check: ibr_056,
    },
    Rule {
        id: "ibr-062",
        check: ibr_062,
    },
    Rule {
        id: "ibr-063",
        check: ibr_063,
    },
    Rule {
        id: "ibr-068",
        check: ibr_068,
    },
    Rule {
        id: "ibr-069",
        check: ibr_069,
    },
    Rule {
        id: "ibr-070",
        check: ibr_070,
    },
    Rule {
        id: "ibr-080",
        check: ibr_080,
    },
    Rule {
        id: "ibr-081",
        check: ibr_081,
    },
    Rule {
        id: "ibr-101-ae",
        check: ibr_101_ae,
    },
    Rule {
        id: "ibr-104",
        check: ibr_104,
    },
    Rule {
        id: "ibr-113",
        check: ibr_113,
    },
    Rule {
        id: "ibr-114",
        check: ibr_114,
    },
    Rule {
        id: "ibr-115",
        check: ibr_115,
    },
    Rule {
        id: "ibr-116",
        check: ibr_116,
    },
    Rule {
        id: "ibr-120",
        check: ibr_120,
    },
    Rule {
        id: "ibr-127",
        check: ibr_127,
    },
    Rule {
        id: "ibr-128-ae",
        check: ibr_128_ae,
    },
    Rule {
        id: "ibr-132-ae",
        check: ibr_132_ae,
    },
    Rule {
        id: "ibr-134-ae",
        check: ibr_134_ae,
    },
    Rule {
        id: "ibr-135-ae",
        check: ibr_135_ae,
    },
    Rule {
        id: "ibr-136-ae",
        check: ibr_136_ae,
    },
    Rule {
        id: "ibr-137-ae",
        check: ibr_137_ae,
    },
    Rule {
        id: "ibr-143-ae",
        check: ibr_143_ae,
    },
    Rule {
        id: "ibr-144-ae",
        check: ibr_144_ae,
    },
    Rule {
        id: "ibr-148-ae",
        check: ibr_148_ae,
    },
    Rule {
        id: "ibr-149-ae",
        check: ibr_149_ae,
    },
    Rule {
        id: "ibr-150-ae",
        check: ibr_150_ae,
    },
    Rule {
        id: "ibr-172-ae",
        check: ibr_172_ae,
    },
    Rule {
        id: "ibr-173-ae",
        check: ibr_173_ae,
    },
    Rule {
        id: "ibr-176-ae",
        check: ibr_176_ae,
    },
    Rule {
        id: "ibr-177-ae",
        check: ibr_177_ae,
    },
    Rule {
        id: "ibr-179-ae",
        check: ibr_179_ae,
    },
    Rule {
        id: "ibr-180-ae",
        check: ibr_180_ae,
    },
    Rule {
        id: "ibr-181-ae",
        check: ibr_181_ae,
    },
    Rule {
        id: "ibr-183-ae",
        check: ibr_183_ae,
    },
    Rule {
        id: "ibr-co-26",
        check: ibr_co_26,
    },
    Rule {
        id: "ibr-sr-16",
        check: ibr_sr_16,
    },
];

/// The seven emirates `ibr-128-ae` accepts.
const EMIRATES: [&str; 7] = ["AUH", "DXB", "SHJ", "UAQ", "FUJ", "AJM", "RAK"];

/// The values `ibr-173-ae` accepts as `schemeAgencyID` of the seller's `CompanyID`.
const SELLER_REGISTRATION_TYPES: [&str; 4] = ["TL", "EID", "PAS", "CD"];

/// The values `ibr-183-ae` accepts as `schemeAgencyID` of the buyer's `CompanyID` (it adds `CL`).
const BUYER_REGISTRATION_TYPES: [&str; 5] = ["TL", "CL", "EID", "PAS", "CD"];

/// The electronic address scheme of the UAE (`ibr-135-ae`, `ibr-149-ae`, `ibr-150-ae`, ...).
const UAE_SCHEME: &str = "0235";

// ------------------------------------------------------------------------------------ the XML

/// An identifier as the exporter writes it, `<cbc:ID schemeID="..">id</cbc:ID>`: it exists when
/// `id` has text.
#[derive(Debug, Clone, Copy)]
struct Ident<'a> {
    id: &'a str,
    scheme: Option<&'a str>,
}

fn ident(i: Option<&pb::Identifier>) -> Option<Ident<'_>> {
    let i = i?;
    Some(Ident {
        id: text(&i.id)?,
        scheme: text(&i.scheme_id),
    })
}

/// `cac:PartyLegalEntity/cbc:CompanyID` with its three attributes (`export::B::party`):
/// `schemeAgencyID` is the registration type and `schemeAgencyName` is the passport issuing
/// country when the type is exactly `PAS`, the authority name otherwise.
#[derive(Debug, Clone, Copy)]
struct Company<'a> {
    id: &'a str,
    scheme: Option<&'a str>,
    agency_id: Option<&'a str>,
    agency_name: Option<&'a str>,
}

fn company(lr: Option<&pb::LegalRegistration>) -> Option<Company<'_>> {
    let lr = lr?;
    let agency_id = text(&lr.r#type);
    let name = if agency_id == Some("PAS") {
        &lr.passport_issuing_country
    } else {
        &lr.authority_name
    };
    Some(Company {
        id: text(&lr.id)?,
        scheme: text(&lr.scheme_id),
        agency_id,
        agency_name: text(name),
    })
}

/// A `cac:PostalAddress`: written when any of the seven fields has text.
#[derive(Debug, Clone, Copy)]
struct Address<'a> {
    line1: Option<&'a str>,
    city: Option<&'a str>,
    subdivision: Option<&'a str>,
    country: Option<&'a str>,
}

fn address(a: Option<&pb::PostalAddress>) -> Option<Address<'_>> {
    let a = a?;
    let any = [
        &a.line1,
        &a.line2,
        &a.line3,
        &a.city,
        &a.post_code,
        &a.country_subdivision,
        &a.country_code,
    ]
    .into_iter()
    .any(|f| text(f).is_some());
    any.then(|| Address {
        line1: text(&a.line1),
        city: text(&a.city),
        subdivision: text(&a.country_subdivision),
        country: text(&a.country_code),
    })
}

/// The `cac:Party` of the seller or the buyer (`export::B::party`).
#[derive(Debug)]
struct PartyXml<'a> {
    endpoint: Option<Ident<'a>>,
    /// `(index in the canonical array, identifier)` of every written `PartyIdentification`.
    identifiers: Vec<(usize, Ident<'a>)>,
    trading_name: Option<&'a str>,
    address: Option<Address<'a>>,
    /// `PartyTaxScheme` with scheme `VAT`: IBT-031 / IBT-048.
    vat: Option<&'a str>,
    /// `PartyTaxScheme` with scheme `TIN`: IBT-032 (the exporter writes it for a buyer too).
    tin: Option<&'a str>,
    /// `PartyLegalEntity/RegistrationName`.
    name: Option<&'a str>,
    company: Option<Company<'a>>,
    /// `CompanyLegalForm` or `Contact` is written: nothing a rule reads, but the party exists.
    has_other: bool,
}

impl<'a> PartyXml<'a> {
    fn new(p: Option<&'a pb::Party>, trn: &'a str) -> Self {
        let mut out = PartyXml {
            endpoint: None,
            identifiers: Vec::new(),
            trading_name: None,
            address: None,
            vat: text(trn),
            tin: None,
            name: None,
            company: None,
            has_other: false,
        };
        if let Some(p) = p {
            out.endpoint = ident(p.electronic_address.as_ref());
            out.identifiers = p
                .identifiers
                .iter()
                .enumerate()
                .filter_map(|(i, id)| Some((i, ident(Some(id))?)))
                .collect();
            out.trading_name = text(&p.trading_name);
            out.address = address(p.postal_address.as_ref());
            out.tin = text(&p.tax_registration_identifier);
            out.name = text(&p.name);
            out.company = company(p.legal_registration.as_ref());
            out.has_other = text(&p.additional_legal_information).is_some()
                || p.contact.as_ref().is_some_and(|c| {
                    [&c.name, &c.telephone, &c.email]
                        .into_iter()
                        .any(|f| text(f).is_some())
                });
        }
        out
    }

    /// The `cac:Party` element is written.
    fn exists(&self) -> bool {
        self.endpoint.is_some()
            || !self.identifiers.is_empty()
            || self.trading_name.is_some()
            || self.address.is_some()
            || self.vat.is_some()
            || self.tin.is_some()
            || self.name.is_some()
            || self.company.is_some()
            || self.has_other
    }

    /// `exists(cac:PartyTaxScheme/cbc:CompanyID)`.
    fn has_tax_scheme(&self) -> bool {
        self.vat.is_some() || self.tin.is_some()
    }

    fn endpoint_scheme(&self) -> Option<&'a str> {
        self.endpoint.and_then(|e| e.scheme)
    }

    /// `cbc:EndpointID/@schemeID = "0235"`.
    fn has_uae_endpoint(&self) -> bool {
        self.endpoint_scheme() == Some(UAE_SCHEME)
    }

    /// `matches(normalize-space(cbc:EndpointID), "^[19]")`.
    fn endpoint_starts_with_1_or_9(&self) -> bool {
        self.endpoint.is_some_and(|e| e.id.starts_with(['1', '9']))
    }
}

fn seller<'a>(doc: &Doc<'a>) -> PartyXml<'a> {
    PartyXml::new(doc.inv.seller.as_ref(), &doc.inv.seller_trn)
}

fn buyer<'a>(doc: &Doc<'a>) -> PartyXml<'a> {
    PartyXml::new(doc.inv.buyer.as_ref(), &doc.inv.buyer_trn)
}

/// The `cac:PayeeParty`.
struct Payee<'a> {
    name: Option<&'a str>,
    identifier: Option<Ident<'a>>,
    legal: Option<Ident<'a>>,
}

/// `Some` when the `cac:PayeeParty` is written.
fn payee<'a>(doc: &Doc<'a>) -> Option<Payee<'a>> {
    let p = doc.inv.payee.as_ref()?;
    let out = Payee {
        name: text(&p.name),
        identifier: ident(p.identifier.as_ref()),
        legal: ident(p.legal_registration.as_ref()),
    };
    (out.name.is_some() || out.identifier.is_some() || out.legal.is_some()).then_some(out)
}

/// The `cac:TaxRepresentativeParty`.
struct Representative<'a> {
    name: Option<&'a str>,
    address: Option<Address<'a>>,
    vat: Option<&'a str>,
}

/// `Some` when the `cac:TaxRepresentativeParty` is written.
fn representative<'a>(doc: &Doc<'a>) -> Option<Representative<'a>> {
    let r = doc.inv.tax_representative.as_ref()?;
    let out = Representative {
        name: text(&r.name),
        address: address(r.postal_address.as_ref()),
        vat: text(&r.vat_identifier),
    };
    (out.name.is_some() || out.address.is_some() || out.vat.is_some()).then_some(out)
}

/// IBT-003 as written to `cbc:InvoiceTypeCode` / `cbc:CreditNoteTypeCode`.
fn type_code<'a>(doc: &Doc<'a>) -> Option<&'a str> {
    text(&doc.inv.invoice_type_code)
}

/// `(cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "480" or ... = "81"`.
fn is_out_of_scope_or_credit_note_type(doc: &Doc<'_>) -> bool {
    matches!(type_code(doc), Some("480" | "81"))
}

/// The eight 0/1 positions of BTAE-02 (`cbc:ProfileExecutionID`) when the written text is exactly
/// eight of them, which is what every `matches(cbc:ProfileExecutionID, "^...$")` of this family
/// requires.
fn flags(code: Option<&str>) -> Option<[bool; 8]> {
    let b = code?.as_bytes();
    if b.len() != 8 || !b.iter().all(|&c| c == b'0' || c == b'1') {
        return None;
    }
    let mut out = [false; 8];
    for (flag, &c) in out.iter_mut().zip(b) {
        *flag = c == b'1';
    }
    Some(out)
}

fn transaction_flag(doc: &Doc<'_>, position: usize) -> bool {
    flags(text(&doc.inv.transaction_type_code)).is_some_and(|f| f[position])
}

/// `matches(cbc:ProfileExecutionID, "^1[01]{7}$")`: free trade zone.
fn is_free_trade_zone(doc: &Doc<'_>) -> bool {
    transaction_flag(doc, 0)
}

/// `matches(cbc:ProfileExecutionID, "^[01]{5}1[01]{2}$")`: disclosed agent billing.
fn is_disclosed_agent_billing(doc: &Doc<'_>) -> bool {
    transaction_flag(doc, 5)
}

/// `matches(cbc:ProfileExecutionID, "^[01]{7}1$")`: exports.
fn is_export(doc: &Doc<'_>) -> bool {
    transaction_flag(doc, 7)
}

/// `^1\d{9}$` (`\d` is any Unicode decimal digit in XPath, as in this crate's regex).
fn is_ten_digit_1xxxxxxxxx(s: &str) -> bool {
    static RE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^1\d{9}$").expect("valid regex"));
    RE.is_match(s)
}

// -------------------------------------------------------------------- identifier check helpers

/// XPath `normalize-space()`: leading and trailing XML whitespace removed, inner runs collapsed.
fn normalize_space(s: &str) -> String {
    s.split([' ', '\t', '\n', '\r'])
        .filter(|part| !part.is_empty())
        .collect::<Vec<_>>()
        .join(" ")
}

/// `matches(x, '^[0-9]+$')`.
fn is_digits(s: &str) -> bool {
    !s.is_empty() && s.bytes().all(|b| b.is_ascii_digit())
}

fn digit_values(s: &str) -> Vec<u32> {
    s.chars().map(|c| c.to_digit(10).unwrap_or(0)).collect()
}

/// `u:gln`: the GS1 check digit of a string of digits.
fn gln(v: &str) -> bool {
    let d = digit_values(v);
    let Some((&check, body)) = d.split_last() else {
        return false;
    };
    let sum: u32 = body
        .iter()
        .rev()
        .enumerate()
        .map(|(i, &x)| x * if i % 2 == 0 { 3 } else { 1 })
        .sum();
    (10 - sum % 10) % 10 == check
}

/// `u:mod11`: `number($val) > 0` and the mod 11 check digit of a string of digits.
fn mod11(v: &str) -> bool {
    let d = digit_values(v);
    let Some((&check, body)) = d.split_last() else {
        return false;
    };
    let sum: u32 = body
        .iter()
        .rev()
        .enumerate()
        .map(|(i, &x)| x * (u32::try_from(i % 6).unwrap_or(0) + 2))
        .sum();
    d.iter().any(|&x| x != 0) && (11 - sum % 11) % 11 == check
}

/// `u:mod97-0208` for a string of ten digits: `97 - (first eight digits mod 97)` is the number
/// the last two digits spell.
fn mod97_0208(v: &str) -> bool {
    if v.len() != 10 || !is_digits(v) {
        return false;
    }
    let first: u64 = v[..8].parse().unwrap_or(0);
    let check: u64 = v[8..].parse().unwrap_or(0);
    97 - first % 97 == check
}

/// `u:abn` for a string of eleven digits.
fn abn(v: &str) -> bool {
    const WEIGHTS: [i64; 11] = [10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19];
    let d = digit_values(v);
    if d.len() != 11 {
        return false;
    }
    let sum: i64 = d
        .iter()
        .zip(WEIGHTS)
        .enumerate()
        .map(|(i, (&x, w))| (i64::from(x) - i64::from(i == 0)) * w)
        .sum();
    sum % 89 == 0
}

/// `u:checkCodiceIPA`: six ASCII letters or digits.
fn check_codice_ipa(s: &str) -> bool {
    s.chars().count() == 6 && s.chars().all(|c| c.is_ascii_alphanumeric())
}

/// `$s castable as xs:integer`: an optional sign and at least one ASCII digit.
fn castable_as_integer(s: &str) -> bool {
    is_digits(s.strip_prefix(['+', '-']).unwrap_or(s))
}

/// `u:checkCF`: 16 characters shaped like a Codice Fiscale, or 11 that read as an integer.
fn check_cf(s: &str) -> bool {
    let c: Vec<char> = s.chars().collect();
    let part = |from: usize, to: usize| c[from..to].iter().collect::<String>();
    let letter = |i: usize| c[i].is_ascii_alphabetic();
    match c.len() {
        11 => castable_as_integer(s),
        16 => {
            (0..6).all(letter)
                && castable_as_integer(&part(6, 8))
                && letter(8)
                && castable_as_integer(&part(9, 11))
                && castable_as_integer(&part(14, 15))
                && letter(15)
        }
        _ => false,
    }
}

/// `u:checkPIVAseIT`: only values that start with `IT` or `it` are checked: eleven digits whose
/// alternating sum (every second digit mapped through `0246813579`) is a multiple of ten. A value
/// that does not read as an integer fails; one with a sign is a dynamic error in the official
/// XSLT and is reported here.
fn check_piva_se_it(s: &str) -> bool {
    const DOUBLED: [u32; 10] = [0, 2, 4, 6, 8, 1, 3, 5, 7, 9];
    let mut chars = s.chars();
    let prefix: String = chars.by_ref().take(2).collect();
    if prefix != "IT" && prefix != "it" {
        return true;
    }
    let code: String = chars.collect();
    if code.chars().count() != 11 || !is_digits(&code) {
        return false;
    }
    let sum: u32 = digit_values(&code)
        .iter()
        .enumerate()
        .map(|(i, &d)| {
            if i % 2 == 0 {
                d
            } else {
                DOUBLED[usize::try_from(d).unwrap_or(0)]
            }
        })
        .sum();
    sum.is_multiple_of(10)
}

/// `string(number(x)) != 'NaN'` for a string without whitespace: the lexical form of `xs:double`
/// (a number with an optional fraction and exponent; `INF` and `NaN` are shorter than the ten
/// characters `ibr-127` asks for).
fn is_xpath_double(s: &str) -> bool {
    let unsigned = s.strip_prefix(['+', '-']).unwrap_or(s);
    let (mantissa, exponent) = match unsigned.split_once(['e', 'E']) {
        Some((m, e)) => (m, Some(e)),
        None => (unsigned, None),
    };
    let mantissa_ok = match mantissa.split_once('.') {
        None => is_digits(mantissa),
        Some((int, frac)) => {
            (is_digits(int) || int.is_empty())
                && (frac.is_empty() || is_digits(frac))
                && !(int.is_empty() && frac.is_empty())
        }
    };
    mantissa_ok && exponent.is_none_or(|e| is_digits(e.strip_prefix(['+', '-']).unwrap_or(e)))
}

/// IBT-031, IBT-048, IBT-063, BTAE-14 of an AE party (`ibr-132-ae`): `string-length(.) = 15 and
/// starts-with(., "1") and ends-with(., "03") and matches(., "^[0-9]+$")`.
fn is_trn(v: &str) -> bool {
    v.chars().count() == 15 && v.starts_with('1') && v.ends_with("03") && is_digits(v)
}

// ----------------------------------------------------------------------- scheme-specific formats

/// Where a node of the format rules sits in the canonical invoice.
#[derive(Clone, Copy)]
enum At {
    Field(&'static str),
    Seller(usize),
    Buyer(usize),
}

impl At {
    fn path(self) -> String {
        match self {
            At::Field(path) => path.to_string(),
            At::Seller(i) => format!("seller.identifiers[{i}].id"),
            At::Buyer(i) => format!("buyer.identifiers[{i}].id"),
        }
    }
}

/// A context node of `cbc:EndpointID[@schemeID = X] | cac:PartyIdentification/cbc:ID[@schemeID =
/// X] | cbc:CompanyID[@schemeID = X]`: every written endpoint, party identifier and legal
/// registration identifier that carries a scheme.
struct SchemeNode<'a> {
    at: At,
    term: &'static str,
    scheme: &'a str,
    value: &'a str,
}

fn scheme_nodes<'a>(doc: &Doc<'a>) -> Vec<SchemeNode<'a>> {
    let (s, b) = (seller(doc), buyer(doc));
    let mut out = Vec::new();
    let mut push = |at: At, term: &'static str, id: Option<Ident<'a>>| {
        if let Some(Ident {
            id: value,
            scheme: Some(scheme),
        }) = id
        {
            out.push(SchemeNode {
                at,
                term,
                scheme,
                value,
            });
        }
    };
    push(
        At::Field("seller.electronic_address.id"),
        "IBT-034",
        s.endpoint,
    );
    push(
        At::Field("buyer.electronic_address.id"),
        "IBT-049",
        b.endpoint,
    );
    for &(i, id) in &s.identifiers {
        push(At::Seller(i), "IBT-029", Some(id));
    }
    for &(i, id) in &b.identifiers {
        push(At::Buyer(i), "IBT-046", Some(id));
    }
    let payee = doc.inv.payee.as_ref();
    push(
        At::Field("payee.identifier.id"),
        "IBT-060",
        ident(payee.and_then(|p| p.identifier.as_ref())),
    );
    let legal = |c: Option<Company<'a>>| {
        c.map(|c| Ident {
            id: c.id,
            scheme: c.scheme,
        })
    };
    push(
        At::Field("seller.legal_registration.id"),
        "IBT-030",
        legal(s.company),
    );
    push(
        At::Field("buyer.legal_registration.id"),
        "IBT-047",
        legal(b.company),
    );
    push(
        At::Field("payee.legal_registration.id"),
        "IBT-061",
        ident(payee.and_then(|p| p.legal_registration.as_ref())),
    );
    out
}

/// One finding per context node of one of `schemes` whose value fails `valid`, at that node's own
/// path with its own business term (F7).
fn check_scheme(
    doc: &Doc<'_>,
    sink: &mut Sink<'_>,
    schemes: &[&str],
    valid: impl Fn(&str) -> bool,
) {
    for node in scheme_nodes(doc) {
        if schemes.contains(&node.scheme) && !valid(node.value) {
            sink.fail_at(node.at.path()).term(node.term);
        }
    }
}

// ------------------------------------------------------------------------------------- rules

/// `ibr-006`, context `/ubl:Invoice | /cn:CreditNote`: `normalize-space(cac:AccountingSupplierParty/cac:Party/cac:PartyLegalEntity/cbc:RegistrationName) !=''`.
/// Fails when `seller.name` is absent.
fn ibr_006(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if seller(doc).name.is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-007`, root context: `normalize-space(cac:AccountingCustomerParty/cac:Party/cac:PartyLegalEntity/cbc:RegistrationName) !=''`.
fn ibr_007(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if buyer(doc).name.is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-007-ae`, root context: `not(matches(cbc:ProfileExecutionID, "^1[01]{7}$")) or
/// cac:BuyerCustomerParty/cac:Party/cac:PartyIdentification/cbc:ID`.
fn ibr_007_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if is_free_trade_zone(doc) && text(&doc.inv.beneficiary_id).is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-008`, root context: `exists(cac:AccountingSupplierParty/cac:Party/cac:PostalAddress)`.
fn ibr_008(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if seller(doc).address.is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-009`, context `cac:AccountingSupplierParty/cac:Party/cac:PostalAddress`:
/// `normalize-space(cac:Country/cbc:IdentificationCode) != ''`.
fn ibr_009(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if seller(doc).address.is_some_and(|a| a.country.is_none()) {
        sink.fail(&[]);
    }
}

/// `ibr-010`, root context: `exists(cac:AccountingCustomerParty/cac:Party/cac:PostalAddress)`.
fn ibr_010(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if buyer(doc).address.is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-010-ae`, context `cac:AccountingCustomerParty/cac:Party`: `not(cac:PartyLegalEntity/cbc:CompanyID/@schemeAgencyID) or not(... = "PAS") or (... = "PAS" and cac:PartyLegalEntity/cbc:CompanyID/@schemeAgencyName and matches(cac:PartyLegalEntity/cbc:CompanyID/@schemeAgencyName, "^(AF|AX|...|ZW)$"))`
/// (the alternation is code list `ibr-010-ae`).
fn ibr_010_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(c) = buyer(doc).company
        && c.agency_id == Some("PAS")
        && !c
            .agency_name
            .is_some_and(|n| sets().contains("ibr-010-ae", n))
    {
        sink.fail(&[]);
    }
}

/// `ibr-011`, context `cac:AccountingCustomerParty/cac:Party/cac:PostalAddress`:
/// `normalize-space(cac:Country/cbc:IdentificationCode) != ''`.
fn ibr_011(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if buyer(doc).address.is_some_and(|a| a.country.is_none()) {
        sink.fail(&[]);
    }
}

/// `ibr-012-ae`, context `cac:AccountingSupplierParty/cac:Party`: as `ibr-010-ae` for the seller
/// (code list `ibr-012-ae`).
fn ibr_012_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(c) = seller(doc).company
        && c.agency_id == Some("PAS")
        && !c
            .agency_name
            .is_some_and(|n| sets().contains("ibr-012-ae", n))
    {
        sink.fail(&[]);
    }
}

/// `ibr-017`, context `cac:PayeeParty`: `exists(cac:PartyName/cbc:Name) and (not(cac:PartyName/cbc:Name = ../cac:AccountingSupplierParty/cac:Party/cac:PartyName/cbc:Name) and not(cac:PartyIdentification/cbc:ID = ../cac:AccountingSupplierParty/cac:Party/cac:PartyIdentification/cbc:ID) )`.
/// The seller side is the trading name, not the registration name, and the identifiers' schemes
/// are ignored.
fn ibr_017(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let Some(p) = payee(doc) else { return };
    let s = seller(doc);
    let same_name = p.name.is_some() && p.name == s.trading_name;
    let same_id = p
        .identifier
        .is_some_and(|i| s.identifiers.iter().any(|(_, other)| other.id == i.id));
    if p.name.is_none() || same_name || same_id {
        sink.fail(&[]);
    }
}

/// `ibr-018`, context `cac:TaxRepresentativeParty`: `normalize-space(cac:PartyName/cbc:Name) != ''`.
fn ibr_018(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if representative(doc).is_some_and(|r| r.name.is_none()) {
        sink.fail(&[]);
    }
}

/// `ibr-019`, context `cac:TaxRepresentativeParty`: `exists(cac:PostalAddress)`.
fn ibr_019(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if representative(doc).is_some_and(|r| r.address.is_none()) {
        sink.fail(&[]);
    }
}

/// `ibr-020`, context `cac:TaxRepresentativeParty/cac:PostalAddress`:
/// `normalize-space(cac:Country/cbc:IdentificationCode) != ''`.
fn ibr_020(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if representative(doc)
        .and_then(|r| r.address)
        .is_some_and(|a| a.country.is_none())
    {
        sink.fail(&[]);
    }
}

/// `ibr-056`, context `cac:TaxRepresentativeParty`: `exists(cac:PartyTaxScheme/cbc:CompanyID)`.
fn ibr_056(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if representative(doc).is_some_and(|r| r.vat.is_none()) {
        sink.fail(&[]);
    }
}

/// `ibr-062`, context `cac:AccountingSupplierParty/cac:Party/cbc:EndpointID`: `exists(@schemeID)`.
fn ibr_062(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if seller(doc).endpoint.is_some_and(|e| e.scheme.is_none()) {
        sink.fail(&[]);
    }
}

/// `ibr-063`, context `cac:AccountingCustomerParty/cac:Party/cbc:EndpointID`: `exists(@schemeID)`.
fn ibr_063(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if buyer(doc).endpoint.is_some_and(|e| e.scheme.is_none()) {
        sink.fail(&[]);
    }
}

/// `ibr-068`, scheme `0088`: `matches(normalize-space(), '^[0-9]+$') and u:gln(normalize-space())`.
fn ibr_068(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    check_scheme(doc, sink, &["0088"], |v| {
        let n = normalize_space(v);
        is_digits(&n) && gln(&n)
    });
}

/// `ibr-069`, scheme `0192`: `matches(normalize-space(), '^[0-9]{9}$') and u:mod11(normalize-space())`.
fn ibr_069(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    check_scheme(doc, sink, &["0192"], |v| {
        let n = normalize_space(v);
        n.len() == 9 && is_digits(&n) && mod11(&n)
    });
}

/// `ibr-070`, scheme `0184`: `(string-length(text()) = 10) and (substring(text(), 1, 2) = 'DK') and
/// (string-length(translate(substring(text(), 3, 8), '1234567890', '')) = 0)`; `text()` is the
/// written value itself.
fn ibr_070(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    check_scheme(doc, sink, &["0184"], |v| {
        v.chars().count() == 10 && v.starts_with("DK") && v[2..].bytes().all(|b| b.is_ascii_digit())
    });
}

/// `ibr-080`, context `cac:AccountingCustomerParty/cac:Party`: `cbc:EndpointID`.
fn ibr_080(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let b = buyer(doc);
    if b.exists() && b.endpoint.is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-081`, context `cac:AccountingSupplierParty/cac:Party`: `cbc:EndpointID`.
fn ibr_081(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let s = seller(doc);
    if s.exists() && s.endpoint.is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-101-ae`, context `cac:AccountingCustomerParty/cac:Party`: `(cac:PartyLegalEntity/cbc:CompanyID/@schemeAgencyID = "TL" and cac:PartyLegalEntity/cbc:CompanyID/@schemeAgencyName) or not(cac:PartyLegalEntity/cbc:CompanyID/@schemeAgencyID = "TL")`.
fn ibr_101_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if buyer(doc)
        .company
        .is_some_and(|c| c.agency_id == Some("TL") && c.agency_name.is_none())
    {
        sink.fail(&[]);
    }
}

/// `ibr-104`, root context: `(count(cac:AccountingCustomerParty/cac:Party/cac:PartyTaxScheme/cbc:CompanyID) <= 1)`.
/// The exporter writes the buyer's VAT identifier and TIN as two `PartyTaxScheme`.
fn ibr_104(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let b = buyer(doc);
    if b.vat.is_some() && b.tin.is_some() {
        sink.fail(&[]);
    }
}

/// `ibr-113`, scheme `0208`: `matches(normalize-space(), '^[0-9]{10}$') and u:mod97-0208(normalize-space())`.
fn ibr_113(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    check_scheme(doc, sink, &["0208"], |v| mod97_0208(&normalize_space(v)));
}

/// `ibr-114`, scheme `0201`: `u:checkCodiceIPA(normalize-space())`.
fn ibr_114(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    check_scheme(doc, sink, &["0201"], |v| {
        check_codice_ipa(&normalize_space(v))
    });
}

/// `ibr-115`, schemes `0210` and `9907`: `u:checkCF(normalize-space())`.
fn ibr_115(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    check_scheme(doc, sink, &["0210", "9907"], |v| {
        check_cf(&normalize_space(v))
    });
}

/// `ibr-116`, schemes `0211` and `9906`: `u:checkPIVAseIT(normalize-space())`.
fn ibr_116(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    check_scheme(doc, sink, &["0211", "9906"], |v| {
        check_piva_se_it(&normalize_space(v))
    });
}

/// `ibr-120`, scheme `0151`: `matches(normalize-space(), '^[0-9]{11}$') and u:abn(normalize-space())`.
fn ibr_120(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    check_scheme(doc, sink, &["0151"], |v| {
        let n = normalize_space(v);
        n.len() == 11 && is_digits(&n) && abn(&n)
    });
}

/// `ibr-127`, scheme `0007`: `string-length(normalize-space()) = 10 and string(number(normalize-space())) != 'NaN'`.
fn ibr_127(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    check_scheme(doc, sink, &["0007"], |v| {
        let n = normalize_space(v);
        n.chars().count() == 10 && is_xpath_double(&n)
    });
}

/// `ibr-128-ae`, context `cac:PostalAddress`: `(cac:Country/cbc:IdentificationCode="AE" and cbc:CountrySubentity = ("AUH", "DXB", "SHJ", "UAQ", "FUJ", "AJM", "RAK")) or not(cac:Country/cbc:IdentificationCode="AE")`.
/// In the compiled XSLT the seller and buyer `cac:PostalAddress` are taken by `ibr-143-ae` and
/// `ibr-144-ae` (same mode, higher priority), so only the tax representative's address is a
/// context of this rule.
fn ibr_128_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(a) = representative(doc).and_then(|r| r.address)
        && a.country == Some("AE")
        && !a.subdivision.is_some_and(|s| EMIRATES.contains(&s))
    {
        sink.fail(&[]);
    }
}

/// `ibr-132-ae`, context `cac:Party[cac:PostalAddress/cac:Country/cbc:IdentificationCode = 'AE']/cac:PartyTaxScheme[cac:TaxScheme/normalize-space(upper-case(cbc:ID)) = 'VAT']/cbc:CompanyID`:
/// `string-length(.) = 15 and starts-with(., "1") and ends-with(., "03") and matches(., "^[0-9]+$")`.
/// The parties with a `cac:Party` element are the seller (`seller_trn`) and the buyer
/// (`buyer_trn`); the tax representative's VAT identifier has no `cac:Party` parent.
fn ibr_132_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    for (party, path, term) in [
        (seller(doc), "seller_trn", "IBT-031"),
        (buyer(doc), "buyer_trn", "IBT-048"),
    ] {
        if party.address.is_some_and(|a| a.country == Some("AE"))
            && party.vat.is_some_and(|v| !is_trn(v))
        {
            sink.fail_at(path).term(term);
        }
    }
}

/// `ibr-134-ae`, root context: `(not((cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "480" or (cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "81") and cac:AccountingSupplierParty/cac:Party/cac:PartyTaxScheme[cac:TaxScheme/cbc:ID = "VAT"]/cbc:CompanyID) or ((cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "480" or (cbc:InvoiceTypeCode | cbc:CreditNoteTypeCode) = "81")`.
fn ibr_134_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if !is_out_of_scope_or_credit_note_type(doc) && seller(doc).vat.is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-135-ae`, context `cac:AccountingCustomerParty/cac:Party`: `((cac:PartyIdentification/cbc:ID or cac:PartyTaxScheme/cbc:CompanyID) and (not(matches(../../cbc:ProfileExecutionID, "^[01]{7}1$")) and cbc:EndpointID/@schemeID = "0235" and not(matches(cbc:EndpointID, "^1\d{9}$")))) or not(not(matches(../../cbc:ProfileExecutionID, "^[01]{7}1$")) and cbc:EndpointID/@schemeID = "0235" and not(matches(cbc:EndpointID, "^1\d{9}$")))`.
fn ibr_135_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let b = buyer(doc);
    if !b.exists() {
        return;
    }
    let applies = !is_export(doc)
        && b.has_uae_endpoint()
        && !b.endpoint.is_some_and(|e| is_ten_digit_1xxxxxxxxx(e.id));
    if applies && b.identifiers.is_empty() && !b.has_tax_scheme() {
        sink.fail(&[]);
    }
}

/// `ibr-136-ae`, root context: `not(cbc:InvoiceTypeCode = "480" or cbc:CreditNoteTypeCode = "81") or exists(cac:AccountingCustomerParty/cac:Party/cac:PartyLegalEntity/cbc:CompanyID)`.
/// Type `480` is always written as `InvoiceTypeCode` and `81` as `CreditNoteTypeCode`.
fn ibr_136_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if is_out_of_scope_or_credit_note_type(doc) && buyer(doc).company.is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-137-ae`, root context: `(matches(cbc:ProfileExecutionID, "^[01]{5}1[01]{2}$") and cac:SellerSupplierParty/cac:Party/cac:PartyIdentification/cbc:ID) or not(matches(cbc:ProfileExecutionID, "^[01]{5}1[01]{2}$"))`.
fn ibr_137_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if is_disclosed_agent_billing(doc) && text(&doc.inv.principal_id).is_none() {
        sink.fail(&[]);
    }
}

/// The first of the three mandatory address fields (`ibr-143-ae`, `ibr-144-ae`) that is absent.
fn first_missing(
    a: &Address<'_>,
    terms: [&'static str; 3],
) -> Option<(&'static str, &'static str)> {
    if a.line1.is_none() {
        Some(("line1", terms[0]))
    } else if a.city.is_none() {
        Some(("city", terms[1]))
    } else if a.subdivision.is_none() {
        Some(("country_subdivision", terms[2]))
    } else {
        None
    }
}

/// `ibr-143-ae`, context `cac:AccountingSupplierParty/cac:Party/cac:PostalAddress`:
/// `(cbc:StreetName) and (cbc:CityName) and (cbc:CountrySubentity)`. One finding per address, at
/// the first absent field.
fn ibr_143_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(a) = seller(doc).address
        && let Some((field, term)) = first_missing(&a, ["IBT-035", "IBT-037", "IBT-039"])
    {
        sink.fail_at(format!("seller.postal_address.{field}"))
            .term(term);
    }
}

/// `ibr-144-ae`, context `cac:AccountingCustomerParty/cac:Party/cac:PostalAddress`:
/// `(cbc:StreetName) and (cbc:CityName) and (cbc:CountrySubentity)`.
fn ibr_144_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(a) = buyer(doc).address
        && let Some((field, term)) = first_missing(&a, ["IBT-050", "IBT-052", "IBT-054"])
    {
        sink.fail_at(format!("buyer.postal_address.{field}"))
            .term(term);
    }
}

/// `ibr-148-ae`, context `cac:AccountingSupplierParty/cac:Party/cac:PartyTaxScheme[cac:TaxScheme/normalize-space(upper-case(cbc:ID)) != 'VAT']/cbc:CompanyID`:
/// `matches(., "^1[0-9]{9}$")`.
fn ibr_148_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if seller(doc)
        .tin
        .is_some_and(|t| !(t.len() == 10 && t.starts_with('1') && is_digits(t)))
    {
        sink.fail(&[]);
    }
}

/// `ibr-149-ae`, context `cac:AccountingCustomerParty/cac:Party`: `cbc:EndpointID/@schemeID != "0235" or matches(normalize-space(cbc:EndpointID), "^[19]") or exists(cac:PartyTaxScheme/cbc:CompanyID)`.
/// An absent endpoint or scheme makes the first two false.
fn ibr_149_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let b = buyer(doc);
    if !b.exists() {
        return;
    }
    let other_scheme = b.endpoint_scheme().is_some_and(|s| s != UAE_SCHEME);
    if !(other_scheme || b.endpoint_starts_with_1_or_9() || b.has_tax_scheme()) {
        sink.fail(&[]);
    }
}

/// `ibr-150-ae`, context `cac:AccountingSupplierParty/cac:Party`: `not(cbc:EndpointID/@schemeID = "0235" and not(cac:PartyLegalEntity/cbc:CompanyID))`.
fn ibr_150_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let s = seller(doc);
    if s.has_uae_endpoint() && s.company.is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-172-ae`, context `cac:AccountingSupplierParty/cac:Party`: `(cac:PartyLegalEntity/cbc:CompanyID/@schemeAgencyID = "TL" and (cac:PartyLegalEntity/cbc:CompanyID/@schemeAgencyName)) or not(cac:PartyLegalEntity/cbc:CompanyID/@schemeAgencyID = "TL")`.
fn ibr_172_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if seller(doc)
        .company
        .is_some_and(|c| c.agency_id == Some("TL") && c.agency_name.is_none())
    {
        sink.fail(&[]);
    }
}

/// `ibr-173-ae`, context `cac:AccountingSupplierParty/cac:Party`: `not(cac:PartyLegalEntity/cbc:CompanyID and  cbc:EndpointID/@schemeID = "0235" and cac:PostalAddress/cac:Country/cbc:IdentificationCode = "AE" and  not(cac:PartyLegalEntity/cbc:CompanyID/@schemeAgencyID = ("TL", "EID", "PAS", "CD")))`.
fn ibr_173_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let s = seller(doc);
    if let Some(c) = s.company
        && s.has_uae_endpoint()
        && s.address.is_some_and(|a| a.country == Some("AE"))
        && !c
            .agency_id
            .is_some_and(|t| SELLER_REGISTRATION_TYPES.contains(&t))
    {
        sink.fail(&[]);
    }
}

/// `ibr-176-ae`, root context: `(matches(cbc:ProfileExecutionID, "^[01]{5}1[01]{2}$") and cac:AccountingSupplierParty/cac:Party/cac:PartyTaxScheme/cbc:CompanyID !=  cac:SellerSupplierParty/cac:Party/cac:PartyIdentification/cbc:ID) or not(matches(cbc:ProfileExecutionID, "^[01]{5}1[01]{2}$"))`.
/// `!=` is existential: some supplier tax identifier must differ from the principal id; with no
/// principal id, or no supplier tax identifier, the comparison is false.
fn ibr_176_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if !is_disclosed_agent_billing(doc) {
        return;
    }
    let s = seller(doc);
    let principal = text(&doc.inv.principal_id);
    let differs = principal.is_some_and(|p| [s.vat, s.tin].into_iter().flatten().any(|id| id != p));
    if !differs {
        sink.fail(&[]);
    }
}

/// `ibr-177-ae`, context `cac:AccountingSupplierParty/cac:Party`: `not(matches((ancestor::*[local-name()='Invoice' or local-name()='CreditNote'][1]/cbc:ProfileExecutionID)[1], '^[01]{5}1[01]{2}$')) or exists(cac:PartyTaxScheme/cbc:CompanyID)`.
fn ibr_177_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let s = seller(doc);
    if is_disclosed_agent_billing(doc) && s.exists() && !s.has_tax_scheme() {
        sink.fail(&[]);
    }
}

/// `ibr-179-ae`, context `cac:AccountingCustomerParty/cac:Party`: `count(cac:PartyTaxScheme/cbc:CompanyID) <=1`
/// (the same condition as `ibr-104`).
fn ibr_179_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let b = buyer(doc);
    if b.vat.is_some() && b.tin.is_some() {
        sink.fail(&[]);
    }
}

/// `ibr-180-ae`, context `cac:AccountingCustomerParty/cac:Party`: `not(cbc:EndpointID[@schemeID = "0235"] and cac:PartyLegalEntity/cbc:CompanyID) or cac:PartyLegalEntity/cbc:CompanyID/@schemeAgencyID`.
fn ibr_180_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let b = buyer(doc);
    if b.has_uae_endpoint() && b.company.is_some_and(|c| c.agency_id.is_none()) {
        sink.fail(&[]);
    }
}

/// `ibr-181-ae`, context `cac:AccountingSupplierParty/cac:Party`: `not(cbc:EndpointID[@schemeID = "0235"]) or not(cac:PartyLegalEntity/cbc:CompanyID) or  cac:PartyLegalEntity/cbc:CompanyID/@schemeAgencyID`.
fn ibr_181_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let s = seller(doc);
    if s.has_uae_endpoint() && s.company.is_some_and(|c| c.agency_id.is_none()) {
        sink.fail(&[]);
    }
}

/// `ibr-183-ae`, context `cac:AccountingCustomerParty/cac:Party`: `not(cac:PartyLegalEntity/cbc:CompanyID) or not(cbc:EndpointID[@schemeID = "0235"]) or  matches(normalize-space(cbc:EndpointID), "^[19]") or cac:PartyLegalEntity/cbc:CompanyID/@schemeAgencyID = ("TL","CL", "EID", "PAS", "CD")`.
fn ibr_183_ae(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let b = buyer(doc);
    if let Some(c) = b.company
        && b.has_uae_endpoint()
        && !b.endpoint_starts_with_1_or_9()
        && !c
            .agency_id
            .is_some_and(|t| BUYER_REGISTRATION_TYPES.contains(&t))
    {
        sink.fail(&[]);
    }
}

/// `ibr-co-26`, context `cac:AccountingSupplierParty/cac:Party`: `exists(cac:PartyTaxScheme/cbc:CompanyID) or exists(cac:PartyIdentification/cbc:ID) or exists(cac:PartyLegalEntity/cbc:CompanyID)`.
fn ibr_co_26(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    let s = seller(doc);
    if s.exists() && !s.has_tax_scheme() && s.identifiers.is_empty() && s.company.is_none() {
        sink.fail(&[]);
    }
}

/// `ibr-sr-16`, root context: `(count(cac:AccountingCustomerParty/cac:Party/cac:PartyIdentification/cbc:ID) <= 1)`.
/// One finding, at the second identifier the exporter writes.
fn ibr_sr_16(doc: &Doc<'_>, sink: &mut Sink<'_>) {
    if let Some(&(index, _)) = buyer(doc).identifiers.get(1) {
        sink.fail(&[index]);
    }
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeSet;

    use roxmltree::{Document, Node};
    use serde_json::Value;

    use super::*;
    use crate::catalog::{Family, Status};
    use crate::conformance::{apply_patch, examples, mutations};
    use crate::export::testing::{maximal, xsd};
    use crate::ruleset::default_ruleset;

    /// What the official schematron reported (Saxon, `conformance/run_schematron.py`) on the
    /// exported XML of `base` with `set` and `remove` applied, restricted to the `parties` rules.
    /// These are the cases the failing fixtures do not show: values that must pass, and the exact
    /// rule set of a situation.
    /// `(name, base, set as JSON, remove, parties rule ids)`.
    #[allow(clippy::type_complexity)]
    const PROBES: &[(&str, &str, &str, &[&str], &[&str])] = &[
        (
            "co26-noparty",
            "standard-tax-invoice",
            r#"{"seller_trn": ""}"#,
            &["seller"],
            &["ibr-006", "ibr-008", "ibr-134-ae"],
        ),
        (
            "136-cn81",
            "standard-tax-credit-note",
            r#"{"invoice_type_code": "81", "buyer.legal_registration.id": ""}"#,
            &[],
            &["ibr-136-ae"],
        ),
        (
            "134-81",
            "standard-tax-credit-note",
            r#"{"invoice_type_code": "81", "seller_trn": ""}"#,
            &[],
            &[],
        ),
        (
            "134-480",
            "commercial-invoice",
            r#"{"seller_trn": ""}"#,
            &[],
            &[],
        ),
        (
            "176-tin",
            "disclosed-agent-billing",
            r#"{"principal_id": "124680135701003", "seller.tax_registration_identifier": "1234567890"}"#,
            &[],
            &[],
        ),
        (
            "176-tin-equal",
            "disclosed-agent-billing",
            r#"{"principal_id": "124680135701003", "seller_trn": "", "seller.tax_registration_identifier": "124680135701003"}"#,
            &[],
            &["ibr-134-ae", "ibr-148-ae", "ibr-176-ae"],
        ),
        (
            "127-dbl1",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "1234.5e+10", "seller.identifiers[0].scheme_id": "0007"}"#,
            &[],
            &[],
        ),
        (
            "127-plus",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "+123456789", "seller.identifiers[0].scheme_id": "0007"}"#,
            &[],
            &[],
        ),
        (
            "127-dot",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "12345678.9", "seller.identifiers[0].scheme_id": "0007"}"#,
            &[],
            &[],
        ),
        (
            "127-hex",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "0x12345678", "seller.identifiers[0].scheme_id": "0007"}"#,
            &[],
            &["ibr-127"],
        ),
        (
            "127-inf",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "1234567.e1", "seller.identifiers[0].scheme_id": "0007"}"#,
            &[],
            &[],
        ),
        (
            "127-dotstart",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": ".123456789", "seller.identifiers[0].scheme_id": "0007"}"#,
            &[],
            &[],
        ),
        (
            "127-minus",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "-123456789", "seller.identifiers[0].scheme_id": "0007"}"#,
            &[],
            &[],
        ),
        (
            "127-spaces",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "12345  678", "seller.identifiers[0].scheme_id": "0007"}"#,
            &[],
            &["ibr-127"],
        ),
        (
            "127-unicode",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "\u0661\u0662\u0663\u0664\u0665\u0666\u0667\u0668\u0669\u0660", "seller.identifiers[0].scheme_id": "0007"}"#,
            &[],
            &["ibr-127"],
        ),
        (
            "132-ae-lower",
            "standard-tax-invoice",
            r#"{"seller_trn": "123", "seller.postal_address.country_code": "ae"}"#,
            &[],
            &[],
        ),
        (
            "132-sa",
            "standard-tax-invoice",
            r#"{"seller_trn": "123", "seller.postal_address.country_code": "SA", "seller.postal_address.country_subdivision": "X"}"#,
            &[],
            &[],
        ),
        (
            "132-nocountry",
            "standard-tax-invoice",
            r#"{"seller_trn": "123"}"#,
            &["seller.postal_address.country_code"],
            &["ibr-009"],
        ),
        (
            "132-rep",
            "standard-invoice-extensive",
            r#"{"tax_representative.vat_identifier": "123"}"#,
            &[],
            &[],
        ),
        (
            "132-buyer-tin",
            "standard-tax-invoice",
            r#"{"buyer.tax_registration_identifier": "123"}"#,
            &[],
            &["ibr-104", "ibr-179-ae"],
        ),
        (
            "135-export",
            "standard-tax-invoice",
            r#"{"buyer_trn": "", "buyer.electronic_address.id": "2345678901", "transaction_type_code": "00000001"}"#,
            &[],
            &["ibr-149-ae"],
        ),
        (
            "135-scheme-other",
            "standard-tax-invoice",
            r#"{"buyer_trn": "", "buyer.electronic_address.id": "2345678901", "buyer.electronic_address.scheme_id": "0060"}"#,
            &[],
            &[],
        ),
        (
            "135-uni",
            "standard-tax-invoice",
            r#"{"buyer_trn": "", "buyer.electronic_address.id": "1\u0662\u0663\u0664\u0665\u0666\u0667\u0668\u0669\u0660"}"#,
            &[],
            &[],
        ),
        (
            "149-9",
            "standard-tax-invoice",
            r#"{"buyer_trn": "", "buyer.electronic_address.id": "9345678901"}"#,
            &[],
            &["ibr-135-ae"],
        ),
        (
            "149-noscheme",
            "standard-tax-invoice",
            r#"{"buyer_trn": "", "buyer.electronic_address.scheme_id": "", "buyer.electronic_address.id": "2345678901"}"#,
            &[],
            &["ibr-063", "ibr-149-ae"],
        ),
        (
            "149-noendpoint-vat",
            "standard-tax-invoice",
            r#"{}"#,
            &["buyer.electronic_address"],
            &["ibr-080"],
        ),
        (
            "150-other-scheme",
            "standard-tax-invoice",
            r#"{"seller.legal_registration.id": "", "seller.electronic_address.scheme_id": "0060"}"#,
            &[],
            &[],
        ),
        (
            "173-notae",
            "standard-tax-invoice",
            r#"{"seller.legal_registration.type": "XX", "seller.postal_address.country_code": "SA", "seller.postal_address.country_subdivision": "X"}"#,
            &[],
            &[],
        ),
        (
            "183-CL",
            "standard-tax-invoice",
            r#"{"buyer.electronic_address.id": "2345678901", "buyer.legal_registration.type": "CL"}"#,
            &[],
            &[],
        ),
        (
            "183-notype",
            "standard-tax-invoice",
            r#"{"buyer.electronic_address.id": "2345678901", "buyer.legal_registration.type": ""}"#,
            &[],
            &["ibr-180-ae", "ibr-183-ae"],
        ),
        (
            "sr16-three",
            "standard-tax-invoice",
            r#"{"buyer.identifiers[0].id": "B-1", "buyer.identifiers[1].id": "", "buyer.identifiers[2].id": "B-3"}"#,
            &[],
            &["ibr-sr-16"],
        ),
        (
            "sr16-empty",
            "standard-tax-invoice",
            r#"{"buyer.identifiers[0].id": "B-1", "buyer.identifiers[1].id": ""}"#,
            &[],
            &[],
        ),
        (
            "017-nopayee-name-in-id",
            "standard-tax-invoice",
            r#"{"payee.name": "x"}"#,
            &["payee.legal_registration"],
            &[],
        ),
        (
            "017-seller-regname",
            "standard-tax-invoice",
            r#"{"payee.name": "Supplier Legal Name"}"#,
            &[],
            &[],
        ),
        (
            "017-payee-id-schemes",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "ID-77", "seller.identifiers[0].scheme_id": "0060", "payee.identifier.id": "ID-77", "payee.identifier.scheme_id": "0088"}"#,
            &[],
            &["ibr-017", "ibr-068"],
        ),
        (
            "payee-empty",
            "standard-tax-invoice",
            r#"{"payee.name": ""}"#,
            &["payee.legal_registration"],
            &[],
        ),
        (
            "rep-onlyvat",
            "standard-tax-invoice",
            r#"{"tax_representative.vat_identifier": "123349792700003"}"#,
            &[],
            &["ibr-018", "ibr-019"],
        ),
        (
            "rep-onlyaddr",
            "standard-tax-invoice",
            r#"{"tax_representative.postal_address.country_code": "AE"}"#,
            &[],
            &["ibr-018", "ibr-056", "ibr-128-ae"],
        ),
        (
            "ben-id",
            "standard-tax-invoice",
            r#"{"transaction_type_code": "10000000", "beneficiary_id": "  X "}"#,
            &[],
            &[],
        ),
        (
            "ben-nopattern",
            "standard-tax-invoice",
            r#"{"transaction_type_code": "1000000"}"#,
            &[],
            &[],
        ),
        (
            "ben-pat2",
            "standard-tax-invoice",
            r#"{"transaction_type_code": "11111111"}"#,
            &[],
            &["ibr-007-ae", "ibr-137-ae", "ibr-176-ae"],
        ),
        (
            "addr-spaces",
            "standard-tax-invoice",
            r#"{"seller.postal_address.line1": "   ", "seller.postal_address.city": "x"}"#,
            &[],
            &["ibr-143-ae"],
        ),
        (
            "seller-only-contact",
            "standard-tax-invoice",
            r#"{"seller_trn": ""}"#,
            &[
                "seller.name",
                "seller.legal_registration",
                "seller.postal_address",
                "seller.electronic_address",
                "seller.trading_name",
                "seller.additional_legal_information",
            ],
            &["ibr-006", "ibr-008", "ibr-081", "ibr-134-ae", "ibr-co-26"],
        ),
        (
            "pas-seller-type",
            "standard-tax-invoice",
            r#"{"seller.legal_registration.type": "PAS", "seller.legal_registration.authority_name": "", "seller.legal_registration.passport_issuing_country": "AE"}"#,
            &[],
            &[],
        ),
        (
            "pas-authority-only",
            "standard-tax-invoice",
            r#"{"seller.legal_registration.type": "PAS", "seller.legal_registration.passport_issuing_country": ""}"#,
            &[],
            &["ibr-012-ae"],
        ),
        (
            "pas-lower",
            "standard-tax-invoice",
            r#"{"seller.legal_registration.type": "pas", "seller.legal_registration.passport_issuing_country": "ZZ"}"#,
            &[],
            &["ibr-173-ae"],
        ),
        (
            "tl-nameless-noid",
            "standard-tax-invoice",
            r#"{"seller.legal_registration.id": "", "seller.legal_registration.authority_name": ""}"#,
            &[],
            &["ibr-150-ae"],
        ),
        (
            "0088-valid",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "4012345000009", "seller.identifiers[0].scheme_id": "0088"}"#,
            &[],
            &[],
        ),
        (
            "0088-single",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "0", "seller.identifiers[0].scheme_id": "0088"}"#,
            &[],
            &[],
        ),
        (
            "0088-single5",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "5", "seller.identifiers[0].scheme_id": "0088"}"#,
            &[],
            &["ibr-068"],
        ),
        (
            "0192-ok",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "974760673", "seller.identifiers[0].scheme_id": "0192"}"#,
            &[],
            &[],
        ),
        (
            "0208-ok",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "0403170701", "seller.identifiers[0].scheme_id": "0208"}"#,
            &[],
            &[],
        ),
        (
            "0151-ok",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "51824753556", "seller.identifiers[0].scheme_id": "0151"}"#,
            &[],
            &[],
        ),
        (
            "0211-ok",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "IT01234567897", "seller.identifiers[0].scheme_id": "0211"}"#,
            &[],
            &[],
        ),
        (
            "0211-lower",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "it12345678901", "seller.identifiers[0].scheme_id": "0211"}"#,
            &[],
            &["ibr-116"],
        ),
        (
            "0211-DE",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "DE12", "seller.identifiers[0].scheme_id": "0211"}"#,
            &[],
            &[],
        ),
        (
            "0210-cf11-plus",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "+1234567890", "seller.identifiers[0].scheme_id": "0210"}"#,
            &[],
            &[],
        ),
        (
            "0210-cf11-ok",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "12345678901", "seller.identifiers[0].scheme_id": "0210"}"#,
            &[],
            &[],
        ),
        (
            "0210-cf16-ok",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "ABCDEF12G34H567Z", "seller.identifiers[0].scheme_id": "0210"}"#,
            &[],
            &[],
        ),
        (
            "0210-cf16-bad",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "1BCDEF12G34H567Z", "seller.identifiers[0].scheme_id": "0210"}"#,
            &[],
            &["ibr-115"],
        ),
        (
            "0210-cf16-sign",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "ABCDEF+2G34H567Z", "seller.identifiers[0].scheme_id": "0210"}"#,
            &[],
            &[],
        ),
        (
            "0184-ok",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "DK12345678", "seller.identifiers[0].scheme_id": "0184"}"#,
            &[],
            &[],
        ),
        (
            "0184-lower",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "dk12345678", "seller.identifiers[0].scheme_id": "0184"}"#,
            &[],
            &["ibr-070"],
        ),
        (
            "0201-ok",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "aB3dE9", "seller.identifiers[0].scheme_id": "0201"}"#,
            &[],
            &[],
        ),
        (
            "0201-uni",
            "standard-tax-invoice",
            r#"{"seller.identifiers[0].id": "aB3dE\u00e9", "seller.identifiers[0].scheme_id": "0201"}"#,
            &[],
            &["ibr-114"],
        ),
        (
            "seller-ep-0088",
            "standard-tax-invoice",
            r#"{"seller.electronic_address.id": "5", "seller.electronic_address.scheme_id": "0088"}"#,
            &[],
            &["ibr-068"],
        ),
        (
            "legal-0007",
            "standard-tax-invoice",
            r#"{"seller.legal_registration.id": "12345", "seller.legal_registration.scheme_id": "0007"}"#,
            &[],
            &["ibr-127"],
        ),
        (
            "payee-id-0007",
            "standard-tax-invoice",
            r#"{"payee.identifier.id": "12345", "payee.identifier.scheme_id": "0007"}"#,
            &[],
            &["ibr-127"],
        ),
        (
            "buyer-legal-0192",
            "standard-tax-invoice",
            r#"{"buyer.legal_registration.id": "12345", "buyer.legal_registration.scheme_id": "0192"}"#,
            &[],
            &["ibr-069"],
        ),
        (
            "ben-0007",
            "standard-tax-invoice",
            r#"{"beneficiary_id": "12345"}"#,
            &[],
            &[],
        ),
        (
            "trn-spaces",
            "standard-tax-invoice",
            r#"{"seller_trn": "  198765432102003 "}"#,
            &[],
            &[],
        ),
        (
            "trn-inner",
            "standard-tax-invoice",
            r#"{"seller_trn": "19876543 2102003"}"#,
            &[],
            &["ibr-132-ae"],
        ),
    ];

    fn base(slug: &str) -> pb::Invoice {
        examples()
            .into_iter()
            .find(|(s, _)| s == slug)
            .unwrap_or_else(|| panic!("no example {slug}"))
            .1
    }

    fn patched(slug: &str, set: &str, remove: &[&str]) -> pb::Invoice {
        let mut inv = base(slug);
        let Value::Object(set) = serde_json::from_str::<Value>(set).unwrap() else {
            panic!("set must be an object");
        };
        let remove: Vec<String> = remove.iter().map(|s| s.to_string()).collect();
        apply_patch(&mut inv, &set, &remove).unwrap();
        inv
    }

    /// Sorted ids of the issues of `parties` rules.
    fn parties_ids(inv: &pb::Invoice) -> Vec<String> {
        let rs = default_ruleset();
        let mut ids: Vec<String> = rs
            .validate(inv)
            .issues
            .into_iter()
            .filter(|i| rs.catalog().get(&i.rule_id).map(|e| e.family) == Some(Family::Parties))
            .map(|i| i.rule_id)
            .collect();
        ids.sort();
        ids
    }

    #[test]
    fn the_registered_rules_are_the_implemented_rows() {
        let registered: BTreeSet<&str> = RULES.iter().map(|r| r.id).collect();
        assert_eq!(registered.len(), RULES.len(), "a rule is registered twice");
        let rs = default_ruleset();
        let implemented: BTreeSet<&str> = rs
            .catalog()
            .entries()
            .iter()
            .filter(|e| e.family == Family::Parties && e.status == Status::Implemented)
            .map(|e| e.rule_id)
            .collect();
        assert_eq!(registered, implemented);
    }

    #[test]
    fn no_pending_row_is_left_and_every_row_is_accounted_for() {
        let rs = default_ruleset();
        let mut counts = std::collections::BTreeMap::new();
        for e in rs
            .catalog()
            .entries()
            .iter()
            .filter(|e| e.family == Family::Parties)
        {
            *counts.entry(format!("{:?}", e.status)).or_insert(0) += 1;
        }
        assert_eq!(
            counts,
            [
                ("Implemented".to_string(), 50),
                ("Structural".to_string(), 18),
                ("UpstreamNoop".to_string(), 3),
            ]
            .into()
        );
    }

    #[test]
    fn probes_confirmed_by_the_official_schematron() {
        let mut problems = Vec::new();
        for (name, slug, set, remove, want) in PROBES {
            let got = parties_ids(&patched(slug, set, remove));
            if got != *want {
                problems.push(format!(
                    "{name}: got {got:?}, the official run has {want:?}"
                ));
            }
        }
        assert!(problems.is_empty(), "{}", problems.join("\n"));
    }

    // ------------------------------------------------------------------ identifier checks

    #[test]
    fn gln_check_digit() {
        assert!(gln("4012345000009"));
        assert!(!gln("4012345000008"));
        assert!(gln("0"));
        assert!(!gln("5"));
        assert!(gln("00"));
        assert!(!gln("01"));
    }

    #[test]
    fn norwegian_numbers_use_mod11() {
        assert!(mod11("974760673"));
        assert!(!mod11("974760674"));
        // number($val) > 0 excludes the all-zero number although its check digit would match
        assert!(!mod11("000000000"));
    }

    #[test]
    fn belgian_numbers_use_mod97() {
        assert!(mod97_0208("0403170701"));
        assert!(!mod97_0208("0403170702"));
        assert!(!mod97_0208("0123456789"));
    }

    #[test]
    fn australian_business_numbers() {
        assert!(abn("51824753556"));
        assert!(!abn("51824753557"));
        assert!(!abn("12345678901"));
    }

    #[test]
    fn italian_tax_codes() {
        assert!(check_cf("12345678901"));
        assert!(check_cf("+1234567890"));
        assert!(!check_cf("1234567890"));
        assert!(!check_cf("1234567890A"));
        assert!(check_cf("ABCDEF12G34H567Z"));
        assert!(check_cf("ABCDEF+2G34H567Z"));
        assert!(!check_cf("1BCDEF12G34H567Z"));
        assert!(!check_cf("ABCDEF12G34H5671"));
        assert!(!check_cf("ABCDEF12G34H567\u{e9}"));
        assert!(check_piva_se_it("IT01234567897"));
        assert!(!check_piva_se_it("IT12345678901"));
        assert!(!check_piva_se_it("it12345678901"));
        assert!(check_piva_se_it("it01234567897"));
        assert!(!check_piva_se_it("IT1234"));
        assert!(check_piva_se_it("DE12"));
        assert!(check_piva_se_it("I"));
        // a sign is a dynamic error in the official XSLT; it is reported here
        assert!(!check_piva_se_it("IT+1234567890"));
        assert!(check_codice_ipa("aB3dE9"));
        assert!(!check_codice_ipa("aB3dE"));
        assert!(!check_codice_ipa("aB3dE\u{e9}"));
    }

    #[test]
    fn swedish_numbers_need_ten_characters_that_read_as_a_double() {
        for ok in [
            "1234567890",
            "1234.5e+10",
            "+123456789",
            "12345678.9",
            "1234567.e1",
            ".123456789",
            "-123456789",
        ] {
            assert!(is_xpath_double(ok), "{ok}");
        }
        for bad in ["0x12345678", "12345abcde", "", "e5", "1e", "--1", "1.2.3"] {
            assert!(!is_xpath_double(bad), "{bad}");
        }
    }

    #[test]
    fn normalize_space_collapses_xml_whitespace_only() {
        assert_eq!(normalize_space("a  b\t\nc"), "a b c");
        assert_eq!(normalize_space(" a "), "a");
        assert_eq!(normalize_space("a\u{a0}b"), "a\u{a0}b");
        assert_eq!(normalize_space(""), "");
    }

    #[test]
    fn trn_format() {
        assert!(is_trn("198765432102003"));
        assert!(!is_trn("298765432102003"));
        assert!(!is_trn("198765432102004"));
        assert!(!is_trn("19876543210200"));
        assert!(!is_trn("19876543210200A"));
        assert!(!is_trn("\u{661}98765432102003"));
    }

    #[test]
    fn transaction_type_flags_are_read_from_exactly_eight_zeros_and_ones() {
        assert_eq!(
            flags(Some("10000000")),
            Some([true, false, false, false, false, false, false, false])
        );
        assert_eq!(flags(Some("00000100")).map(|f| f[5]), Some(true));
        assert_eq!(flags(Some("00000001")).map(|f| f[7]), Some(true));
        assert_eq!(flags(Some("1000000")), None);
        assert_eq!(flags(Some("100000000")), None);
        assert_eq!(flags(Some("1000000x")), None);
        assert_eq!(flags(None), None);
    }

    // --------------------------------------------------------------------------- upstream_noop

    /// The three `upstream_noop` rows: the official schematron cannot report them, and the family
    /// fixtures (whose `expect` lists come from that run) never contain them.
    #[test]
    fn upstream_noop_rules_are_never_expected() {
        const NOOP: [&str; 3] = ["ibr-011-ae", "ibr-013-ae", "ibr-sr-23"];
        let rs = default_ruleset();
        for id in NOOP {
            assert_eq!(rs.catalog().get(id).unwrap().status, Status::UpstreamNoop);
        }
        let fixtures = mutations(Family::Parties).unwrap();
        for m in &fixtures {
            for id in NOOP {
                assert!(!m.expect.iter().any(|e| e == id), "{} expects {id}", m.id);
            }
        }
        // the fixtures that would trip them: a passport country in neither code list
        for (id, field) in [
            (
                "ibr-010-ae#2",
                "buyer.legal_registration.passport_issuing_country",
            ),
            (
                "ibr-012-ae#2",
                "seller.legal_registration.passport_issuing_country",
            ),
        ] {
            let m = fixtures.iter().find(|m| m.id == id).unwrap();
            assert!(m.set.contains_key(field), "{id}");
            assert_eq!(m.expect.len(), 1, "{id} {:?}", m.expect);
        }
        // ibr-sr-23: TaxRepresentativeParty has no cac:Party child in the UBL 2.1 XSD
        let model = &xsd().models["cac:TaxRepresentativeParty"];
        assert!(model.iter().any(|p| p.name == "cac:PartyTaxScheme"));
        assert!(!model.iter().any(|p| p.name == "cac:Party"));
    }

    // ----------------------------------------------------------------------------- structural

    fn step<'a>(from: &[Node<'a, 'a>], name: &str) -> Vec<Node<'a, 'a>> {
        from.iter()
            .flat_map(|n| {
                n.children()
                    .filter(|c| c.is_element() && c.tag_name().name() == name)
            })
            .collect()
    }

    fn at<'a>(from: &[Node<'a, 'a>], path: &str) -> Vec<Node<'a, 'a>> {
        path.split('/')
            .fold(from.to_vec(), |acc, name| step(&acc, name))
    }

    struct Ctx<'a> {
        seller: Vec<Node<'a, 'a>>,
        buyer: Vec<Node<'a, 'a>>,
        payee: Vec<Node<'a, 'a>>,
        rep: Vec<Node<'a, 'a>>,
        all_tax_schemes: Vec<Node<'a, 'a>>,
    }

    fn vat_schemes<'a>(party: &[Node<'a, 'a>]) -> Vec<Node<'a, 'a>> {
        step(party, "PartyTaxScheme")
            .into_iter()
            .filter(|s| {
                at(&[*s], "TaxScheme/ID")
                    .iter()
                    .any(|id| id.text().unwrap_or("").to_uppercase() == "VAT")
            })
            .collect()
    }

    /// Each structural rule's official XPath, evaluated on exported XML (true = the assert holds).
    #[allow(clippy::type_complexity)]
    const STRUCTURAL: &[(&str, fn(&Ctx<'_>) -> bool)] = &[
        ("aligned-ibrp-sr-12", |c| {
            at(&vat_schemes(&c.seller), "CompanyID").len() <= 1
        }),
        ("ibr-098", |c| {
            at(&c.seller, "PartyLegalEntity/RegistrationName").len() <= 1
        }),
        ("ibr-099", |c| at(&c.seller, "PartyName/Name").len() <= 1),
        ("ibr-100", |c| {
            at(&c.seller, "PartyLegalEntity/CompanyID").len() <= 1
        }),
        ("ibr-101", |c| {
            at(&c.seller, "PartyLegalEntity/CompanyLegalForm").len() <= 1
        }),
        ("ibr-102", |c| {
            at(&c.buyer, "PartyLegalEntity/RegistrationName").len() <= 1
        }),
        ("ibr-103", |c| {
            at(&c.buyer, "PartyLegalEntity/CompanyID").len() <= 1
        }),
        ("ibr-105", |c| {
            step(&c.payee, "PartyIdentification")
                .iter()
                .flat_map(|p| step(&[*p], "ID"))
                .filter(|id| {
                    id.attribute("schemeID")
                        .is_some_and(|s| s.to_uppercase() != "SEPA")
                })
                .count()
                <= 1
        }),
        ("ibr-106", |c| {
            at(&c.payee, "PartyLegalEntity/CompanyID").len() <= 1
        }),
        ("ibr-112", |c| at(&c.buyer, "PartyName/Name").len() <= 1),
        ("ibr-178-ae", |c| {
            let all = step(&c.seller, "PartyTaxScheme").len();
            (all == 2 && vat_schemes(&c.seller).len() == 1) || all < 2
        }),
        ("ibr-sr-19", |c| at(&c.payee, "PartyName/Name").len() <= 1),
        ("ibr-sr-22", |c| at(&c.rep, "PartyName/Name").len() <= 1),
        ("ibr-sr-42", |c| {
            step(&c.seller, "PartyTaxScheme").len() <= 2
        }),
        ("ibr-sr-53", |c| {
            at(&c.seller, "PostalAddress/AddressLine/Line").len() <= 1
        }),
        ("ibr-sr-54", |c| {
            at(&c.buyer, "PostalAddress/AddressLine/Line").len() <= 1
        }),
        ("ibr-sr-55", |c| {
            at(&c.rep, "PostalAddress/AddressLine/Line").len() <= 1
        }),
        ("ibr-sr-57", |c| {
            c.all_tax_schemes
                .iter()
                .all(|s| !step(&[*s], "CompanyID").is_empty())
        }),
    ];

    /// The 18 `structural` rows hold on every exported document of the platform: the maximal
    /// documents, the 30 official examples and every `parties` fixture.
    #[test]
    fn structural_rules_hold_on_every_exported_party() {
        let rs = default_ruleset();
        let structural: BTreeSet<&str> = rs
            .catalog()
            .entries()
            .iter()
            .filter(|e| e.family == Family::Parties && e.status == Status::Structural)
            .map(|e| e.rule_id)
            .collect();
        let checked: BTreeSet<&str> = STRUCTURAL.iter().map(|(id, _)| *id).collect();
        assert_eq!(structural, checked);

        let mut docs: Vec<(String, pb::Invoice)> = examples();
        docs.push(("maximal 380".into(), maximal("380")));
        docs.push(("maximal 381".into(), maximal("381")));
        let bases = examples();
        for m in mutations(Family::Parties).unwrap() {
            docs.push((m.id.clone(), m.apply(&bases).unwrap()));
        }
        let mut exported = 0;
        for (name, inv) in &docs {
            let Ok(xml) = crate::export::to_xml(&crate::doc::Doc::new(inv)) else {
                continue;
            };
            exported += 1;
            let xml = String::from_utf8(xml).unwrap();
            let doc = Document::parse(&xml).unwrap();
            let root = [doc.root_element()];
            let ctx = Ctx {
                seller: at(&root, "AccountingSupplierParty/Party"),
                buyer: at(&root, "AccountingCustomerParty/Party"),
                payee: at(&root, "PayeeParty"),
                rep: at(&root, "TaxRepresentativeParty"),
                all_tax_schemes: doc
                    .descendants()
                    .filter(|n| n.is_element() && n.tag_name().name() == "PartyTaxScheme")
                    .collect(),
            };
            for (id, holds) in STRUCTURAL {
                assert!(holds(&ctx), "{id} fails on {name}");
            }
        }
        assert!(exported >= 100, "only {exported} documents were exported");
    }
}
