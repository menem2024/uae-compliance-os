//! Every numeric interpretation of the validator (spec 5.2.2). No other module parses a number
//! from a string, and rule code never applies the `+ - * /` operators to a `Decimal` (they panic
//! on overflow): it uses the checked helpers below, whose `None` makes a rule's comparison false.

use rust_decimal::Decimal;

/// Why a decimal string was rejected (contract rule 10).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DecError {
    /// Not `^[+-]?[0-9]+(\.[0-9]+)?$` with ASCII digits.
    Grammar,
    /// More than 28 significant digits, or not representable exactly.
    Precision,
}

impl std::fmt::Display for DecError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(match self {
            DecError::Grammar => "not a decimal string",
            DecError::Precision => "more than 28 significant digits",
        })
    }
}

impl std::error::Error for DecError {}

/// Most significant digits a decimal string may carry (contract rule 10, the `rust_decimal` limit).
pub const MAX_SIGNIFICANT_DIGITS: usize = 28;

/// Parses a trimmed decimal string: `^[+-]?[0-9]+(\.[0-9]+)?$` with ASCII digits only and at most
/// 28 significant digits (leading zeros do not count). Arabic-Indic digits, exponents, separators,
/// currency symbols and inner or surrounding spaces are [`DecError::Grammar`]. The value keeps the
/// lexical scale (`"115.70"` stays `115.70`) and is never rounded.
pub fn parse(raw: &str) -> Result<Decimal, DecError> {
    let bytes = raw.as_bytes();
    let unsigned = match bytes.first() {
        Some(b'+' | b'-') => &bytes[1..],
        _ => bytes,
    };
    let (int, frac): (&[u8], &[u8]) = match unsigned.iter().position(|&c| c == b'.') {
        Some(dot) => (&unsigned[..dot], &unsigned[dot + 1..]),
        None => (unsigned, &[]),
    };
    let has_dot = int.len() < unsigned.len();
    if int.is_empty()
        || (has_dot && frac.is_empty())
        || !int.iter().chain(frac).all(u8::is_ascii_digit)
    {
        return Err(DecError::Grammar);
    }
    let significant = int.iter().chain(frac).skip_while(|&&c| c == b'0').count();
    if significant > MAX_SIGNIFICANT_DIGITS {
        return Err(DecError::Precision);
    }
    let text = raw.strip_prefix('+').unwrap_or(raw);
    Decimal::from_str_exact(text).map_err(|_| DecError::Precision)
}

/// Lexical number of characters after the first `.`, i.e. XPath
/// `string-length(substring-after(x, '.'))`. Rules that bound the number of decimals (for example
/// `ibr-091`) use this, never the parsed value.
pub fn fraction_digits(raw: &str) -> usize {
    raw.split_once('.')
        .map_or(0, |(_, frac)| frac.chars().count())
}

/// XPath `round(x * 10 * 10) div 100`: round to two decimals, half toward positive infinity,
/// i.e. `floor(x * 100 + 0.5) / 100` (`2.345 -> 2.35`, `-2.345 -> -2.34`). `rust_decimal` has no
/// such midpoint strategy, so this is the only implementation. The result carries exactly two
/// decimals (`1050 -> 1050.00`) when that fits the 96-bit mantissa; otherwise `x` is returned
/// unchanged, which is equal in value because it then has at most two decimals already.
pub fn xpath_round2(x: Decimal) -> Decimal {
    let scale = x.scale();
    let mantissa = x.mantissa();
    let rounded = if scale <= 2 {
        10i128
            .checked_pow(2 - scale)
            .and_then(|factor| mantissa.checked_mul(factor))
    } else {
        // x * 100 = mantissa / d with d = 10^(scale - 2) <= 10^26, so
        // floor(x * 100 + 1/2) = floor((2 * mantissa + d) / (2 * d)). |mantissa| < 2^96, so
        // nothing below can overflow an i128; the checked forms only keep the code panic-free.
        10i128.checked_pow(scale - 2).and_then(|d| {
            let num = mantissa.checked_mul(2)?.checked_add(d)?;
            let den = d.checked_mul(2)?;
            Some(num.div_euclid(den))
        })
    };
    rounded
        .and_then(|m| Decimal::try_from_i128_with_scale(m, 2).ok())
        .unwrap_or(x)
}

/// `u:slack(expected, value, slack)`: `expected - slack <= value <= expected + slack`
/// (`PINT-UBL-validation-preprocessed.sch:16-21`). False when a bound overflows.
pub fn slack(expected: Decimal, value: Decimal, slack: Decimal) -> bool {
    match (sub(expected, slack), add(expected, slack)) {
        (Some(low), Some(high)) => low <= value && value <= high,
        _ => false,
    }
}

/// XPath effective boolean value of `xs:decimal(x)`: false when absent or zero
/// (`ibr-co-16` treats `PrepaidAmount = 0.00` like an absent one).
pub fn ebv(d: Option<Decimal>) -> bool {
    d.is_some_and(|v| !v.is_zero())
}

/// `a + b`, `None` on overflow.
pub fn add(a: Decimal, b: Decimal) -> Option<Decimal> {
    a.checked_add(b)
}

/// `a - b`, `None` on overflow.
pub fn sub(a: Decimal, b: Decimal) -> Option<Decimal> {
    a.checked_sub(b)
}

/// `a * b`, `None` on overflow. A product needing more than 28 decimals is rounded to 28 by
/// `rust_decimal`; operands come from 28-digit strings, so that only affects products of two
/// values that each have more than 14 decimals.
pub fn mul(a: Decimal, b: Decimal) -> Option<Decimal> {
    a.checked_mul(b)
}

/// XPath `sum()`: zero for no items, `None` on overflow.
pub fn sum<I: IntoIterator<Item = Decimal>>(xs: I) -> Option<Decimal> {
    xs.into_iter().try_fold(Decimal::ZERO, add)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::str::FromStr;

    fn d(s: &str) -> Decimal {
        Decimal::from_str(s).unwrap()
    }

    #[test]
    fn parse_accepts_the_contract_grammar() {
        for (raw, want) in [
            ("0", "0"),
            ("5", "5"),
            ("-5", "-5"),
            ("+5", "5"),
            ("1050.00", "1050.00"),
            ("0.05", "0.05"),
            ("-0.5", "-0.5"),
            ("007.10", "7.10"),
            ("3.67285", "3.67285"),
        ] {
            assert_eq!(parse(raw), Ok(d(want)), "{raw:?}");
        }
    }

    #[test]
    fn parse_keeps_the_lexical_scale() {
        assert_eq!(parse("115.70").unwrap().to_string(), "115.70");
        assert_eq!(parse("10486").unwrap().to_string(), "10486");
    }

    #[test]
    fn parse_rejects_everything_else_as_grammar() {
        for raw in [
            "",
            " ",
            " 1",
            "1 ",
            "1 000",
            "1,000.00",
            "1.000,00",
            ".5",
            "5.",
            "+",
            "-",
            "+-1",
            "--1",
            "1e3",
            "1E3",
            "0x10",
            "AED200000",
            "200 AED",
            "$5",
            "1.2.3",
            "١٠٠",
            "۱۰۰",
            "１００",
            "NaN",
            "inf",
            "1_000",
            "\u{a0}1",
        ] {
            assert_eq!(parse(raw), Err(DecError::Grammar), "{raw:?}");
        }
    }

    #[test]
    fn parse_limits_significant_digits_to_28() {
        let max = "1234567890123456789012345678";
        assert_eq!(max.len(), 28);
        assert!(parse(max).is_ok());
        assert!(parse(&format!("-{max}")).is_ok());
        assert!(parse("12345678901234567890.12345678").is_ok());
        assert!(parse("0.1234567890123456789012345678").is_ok());
        // Leading zeros are not significant.
        assert!(parse(&format!("0000{max}")).is_ok());
        assert_eq!(parse(&format!("{max}9")), Err(DecError::Precision));
        assert_eq!(
            parse("12345678901234567890.123456789"),
            Err(DecError::Precision)
        );
        assert_eq!(
            parse("99999999999999999999999999999999999999"),
            Err(DecError::Precision)
        );
        // 29 digits after the point cannot be held exactly (scale limit 28).
        assert_eq!(
            parse("0.00000000000000000000000000001"),
            Err(DecError::Precision)
        );
    }

    #[test]
    fn fraction_digits_is_lexical() {
        assert_eq!(fraction_digits("100"), 0);
        assert_eq!(fraction_digits("100.0"), 1);
        assert_eq!(fraction_digits("100.00"), 2);
        assert_eq!(fraction_digits("1.123"), 3);
        assert_eq!(fraction_digits("-0.50"), 2);
        // string-length(substring-after(x, '.')) counts characters after the first dot.
        assert_eq!(fraction_digits("1.2.3"), 3);
        assert_eq!(fraction_digits(""), 0);
    }

    #[test]
    fn xpath_round2_rounds_half_toward_positive_infinity() {
        for (x, want) in [
            ("2.345", "2.35"),
            ("-2.345", "-2.34"),
            ("-2.355", "-2.35"),
            ("2.344", "2.34"),
            ("2.3449999", "2.34"),
            ("-2.346", "-2.35"),
            ("0.005", "0.01"),
            ("-0.005", "0.00"),
            ("532.1645", "532.16"),
            ("1.5", "1.50"),
            ("1050", "1050.00"),
            ("-7", "-7.00"),
            ("0", "0.00"),
        ] {
            let got = xpath_round2(d(x));
            assert_eq!(got, d(want), "{x}");
            assert_eq!(got.to_string(), want, "{x} keeps two decimals");
        }
    }

    #[test]
    fn xpath_round2_never_panics_at_the_limits() {
        for x in [
            Decimal::MAX,
            Decimal::MIN,
            d("0.0000000000000000000000000005"),
            d("-0.0000000000000000000000000005"),
            d("79228162514264337593543950.335"),
        ] {
            let _ = xpath_round2(x);
        }
        assert_eq!(xpath_round2(Decimal::MAX), Decimal::MAX);
        assert_eq!(
            xpath_round2(d("0.0000000000000000000000000005")),
            Decimal::ZERO
        );
        assert_eq!(
            xpath_round2(d("79228162514264337593543950.335")),
            d("79228162514264337593543950.34")
        );
    }

    #[test]
    fn slack_is_inclusive_on_both_sides() {
        let s = d("0.02");
        assert!(slack(d("10.00"), d("10.00"), s));
        assert!(slack(d("10.00"), d("10.02"), s));
        assert!(slack(d("10.00"), d("9.98"), s));
        assert!(!slack(d("10.00"), d("10.021"), s));
        assert!(!slack(d("10.00"), d("9.979"), s));
        assert!(slack(d("1"), d("1"), Decimal::ZERO));
        assert!(!slack(d("1"), d("1.0001"), Decimal::ZERO));
    }

    #[test]
    fn slack_is_false_on_overflow() {
        assert!(!slack(Decimal::MAX, Decimal::MAX, d("1")));
        assert!(!slack(Decimal::MIN, Decimal::MIN, d("1")));
    }

    #[test]
    fn ebv_is_false_for_absent_or_zero() {
        assert!(!ebv(None));
        assert!(!ebv(Some(d("0"))));
        assert!(!ebv(Some(d("0.00"))));
        assert!(!ebv(Some(d("-0.00"))));
        assert!(ebv(Some(d("0.01"))));
        assert!(ebv(Some(d("-1"))));
    }

    #[test]
    fn checked_helpers_are_exact_and_return_none_on_overflow() {
        assert_eq!(add(d("0.1"), d("0.2")), Some(d("0.3")));
        assert_eq!(sub(d("10486"), d("262.15")), Some(d("10223.85")));
        assert_eq!(mul(d("4.9"), d("2000")), Some(d("9800.0")));
        assert_eq!(add(Decimal::MAX, d("1")), None);
        assert_eq!(sub(Decimal::MIN, d("1")), None);
        assert_eq!(mul(Decimal::MAX, d("2")), None);
    }

    #[test]
    fn sum_of_nothing_is_zero_and_overflow_is_none() {
        assert_eq!(sum(Vec::<Decimal>::new()), Some(Decimal::ZERO));
        assert_eq!(sum([d("262.15"), d("419.44")]), Some(d("681.59")));
        assert_eq!(sum([Decimal::MAX, d("1")]), None);
    }

    /// G2: no binary floating-point type anywhere in `src/` except `telemetry.rs` (spec 5.2.5
    /// rule 5). The pattern is assembled at run time so that this file does not match itself.
    #[test]
    fn no_binary_float_types_in_src() {
        let pattern = regex::Regex::new(&format!(r"\bf(?:{}|{})\b", 32, 64)).unwrap();
        let src = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("src");
        let mut stack = vec![src.clone()];
        let mut offenders = Vec::new();
        let mut scanned = 0;
        while let Some(dir) = stack.pop() {
            for entry in std::fs::read_dir(&dir).unwrap() {
                let path = entry.unwrap().path();
                if path.is_dir() {
                    stack.push(path);
                    continue;
                }
                if path.extension().is_none_or(|x| x != "rs") || path == src.join("telemetry.rs") {
                    continue;
                }
                scanned += 1;
                let text = std::fs::read_to_string(&path).unwrap();
                for (n, line) in text.lines().enumerate() {
                    if pattern.is_match(line) {
                        offenders.push(format!("{}:{}: {line}", path.display(), n + 1));
                    }
                }
            }
        }
        assert!(scanned > 10, "scanned only {scanned} files");
        assert!(
            offenders.is_empty(),
            "float types in src:\n{}",
            offenders.join("\n")
        );
    }
}
