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
///
/// `rust_decimal` rounds a result that needs more digits than its 96-bit mantissa holds
/// (`1e25 + 0.0049999999999999999999999` becomes `…0.005`), while `xs:decimal` is exact. A rule
/// that compares or rounds a sum or difference of contract amounts uses [`Cents`] instead.
pub fn sum<I: IntoIterator<Item = Decimal>>(xs: I) -> Option<Decimal> {
    xs.into_iter().try_fold(Decimal::ZERO, add)
}

/// One cent in sub-cent units (`10^26`): a decimal of scale at most 28 has at most 26 digits
/// below the cent.
const SUB_PER_CENT: i128 = 10i128.pow(26);

/// An exact amount for sums, differences and XPath rounding: `value * 100 = whole + sub / 10^26`
/// with `0 <= sub < 10^26`. Every `Decimal` (scale at most 28) converts without rounding, so
/// additions and subtractions of contract amounts (28 significant digits each) stay exact where
/// [`add`] and [`sub`] would round: `|whole| < 10^31` and sums of millions of amounts stay far
/// below `i128::MAX` (`1.7 * 10^38`). All arithmetic is checked; `None` means overflow, which a
/// rule treats like a failed comparison. The derived order is the numeric order (`sub` is in
/// range).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct Cents {
    whole: i128,
    sub: i128,
}

impl Cents {
    pub const ZERO: Cents = Cents { whole: 0, sub: 0 };

    /// The exact amount of `x`; `None` only if `x` has a scale above 28.
    pub fn of(x: Decimal) -> Option<Cents> {
        let (m, scale) = (x.mantissa(), x.scale());
        if scale <= 2 {
            let whole = m.checked_mul(10i128.checked_pow(2 - scale)?)?;
            return Some(Cents { whole, sub: 0 });
        }
        // value * 100 = m / 10^(scale - 2), with 1 <= scale - 2 <= 26.
        let unit = 10i128.checked_pow(scale - 2)?;
        let sub = m
            .rem_euclid(unit)
            .checked_mul(10i128.checked_pow(28u32.checked_sub(scale)?)?)?;
        Some(Cents {
            whole: m.div_euclid(unit),
            sub,
        })
    }

    /// XPath `sum()` of exact amounts: zero for none, `None` on overflow.
    pub fn sum(xs: impl IntoIterator<Item = Decimal>) -> Option<Cents> {
        xs.into_iter()
            .try_fold(Cents::ZERO, |acc, x| acc.checked_add(Cents::of(x)?))
    }

    pub fn checked_add(self, other: Cents) -> Option<Cents> {
        let whole = self.whole.checked_add(other.whole)?;
        let sub = self.sub.checked_add(other.sub)?;
        if sub >= SUB_PER_CENT {
            Some(Cents {
                whole: whole.checked_add(1)?,
                sub: sub.checked_sub(SUB_PER_CENT)?,
            })
        } else {
            Some(Cents { whole, sub })
        }
    }

    pub fn checked_neg(self) -> Option<Cents> {
        if self.sub == 0 {
            return Some(Cents {
                whole: self.whole.checked_neg()?,
                sub: 0,
            });
        }
        Some(Cents {
            whole: self.whole.checked_neg()?.checked_sub(1)?,
            sub: SUB_PER_CENT.checked_sub(self.sub)?,
        })
    }

    pub fn checked_sub(self, other: Cents) -> Option<Cents> {
        self.checked_add(other.checked_neg()?)
    }

    /// `floor(value * 100)`: the whole cents, rounded toward negative infinity.
    pub fn floor_cents(self) -> i128 {
        self.whole
    }

    /// Whether the amount is a whole number of cents (at most two decimals).
    pub fn is_whole(self) -> bool {
        self.sub == 0
    }

    /// XPath `round(x * 10 * 10) div 100`: whole cents, a half rounded toward positive infinity
    /// (`floor(x * 100 + 0.5)`), the semantics of [`xpath_round2`].
    pub fn round_half_up(self) -> Option<Cents> {
        self.round(self.sub >= SUB_PER_CENT / 2)
    }

    /// Whole cents, a half rounded toward negative infinity (`ceil(x * 100 - 0.5)`): the one
    /// two-decimal `y` with `x - 0.005 <= y < x + 0.005`.
    pub fn round_half_down(self) -> Option<Cents> {
        self.round(self.sub > SUB_PER_CENT / 2)
    }

    fn round(self, up: bool) -> Option<Cents> {
        let whole = if up {
            self.whole.checked_add(1)?
        } else {
            self.whole
        };
        Some(Cents { whole, sub: 0 })
    }

    /// `u:slack(expected, value, slack)`: `expected - slack <= value <= expected + slack`,
    /// exactly; false when a bound overflows.
    pub fn slack(expected: Cents, value: Cents, slack: Cents) -> bool {
        match (expected.checked_sub(slack), expected.checked_add(slack)) {
            (Some(low), Some(high)) => low <= value && value <= high,
            _ => false,
        }
    }

    /// The amount as a decimal with two decimals (fewer when 28 significant digits do not hold
    /// two), or `None` when it has a sub-cent part or no form the contract grammar accepts.
    /// Suggested values of rounded amounts use this.
    pub fn to_decimal(self) -> Option<Decimal> {
        if self.sub != 0 {
            return None;
        }
        let mut whole = self.whole;
        for scale in [2u32, 1, 0] {
            if let Ok(d) = Decimal::try_from_i128_with_scale(whole, scale)
                && parse(&d.to_string()).is_ok()
            {
                return Some(d);
            }
            if whole % 10 != 0 {
                return None;
            }
            whole /= 10;
        }
        None
    }

    /// The exact amount as a decimal (two decimals when it is whole cents, else no trailing
    /// zero), or `None` when it has no form the contract grammar accepts (more than 28
    /// significant digits). Suggested values of unrounded sums use this.
    pub fn to_decimal_exact(self) -> Option<Decimal> {
        if self.sub == 0 {
            return self.to_decimal();
        }
        // value = (whole * 10^(26 - k) + sub / 10^k) / 10^(28 - k), k = trailing zeros of sub.
        let mut sub = self.sub;
        let mut k = 0u32;
        while sub % 10 == 0 {
            sub /= 10;
            k += 1;
        }
        let mantissa = self
            .whole
            .checked_mul(10i128.checked_pow(26 - k)?)?
            .checked_add(sub)?;
        let d = Decimal::try_from_i128_with_scale(mantissa, 28 - k).ok()?;
        parse(&d.to_string()).is_ok().then_some(d)
    }
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

    /// SplitMix64, for the property tests below.
    struct Rng(u64);

    impl Rng {
        fn next(&mut self) -> u64 {
            self.0 = self.0.wrapping_add(0x9E37_79B9_7F4A_7C15);
            let mut z = self.0;
            z = (z ^ (z >> 30)).wrapping_mul(0xBF58_476D_1CE4_E5B9);
            z = (z ^ (z >> 27)).wrapping_mul(0x94D0_49BB_1331_11EB);
            z ^ (z >> 31)
        }

        /// A decimal below `int_max` with 0 to `max_dec` decimals, negative one time in eight.
        fn decimal(&mut self, int_max: u64, max_dec: u32) -> Decimal {
            let int = self.next() % int_max;
            let dec = u32::try_from(self.next() % (u64::from(max_dec) + 1)).unwrap();
            let frac = self.next() % 10u64.pow(dec);
            let sign = if self.next().is_multiple_of(8) {
                "-"
            } else {
                ""
            };
            let text = if dec > 0 {
                format!("{sign}{int}.{frac:0width$}", width = dec as usize)
            } else {
                format!("{sign}{int}")
            };
            d(&text)
        }
    }

    /// The exact cents agree with `add`, `sub` and `xpath_round2` wherever `rust_decimal` is
    /// exact (operands of 12 integer digits and 6 decimals), and value equality and order are
    /// exact.
    #[test]
    fn cents_agree_with_the_decimal_helpers_wherever_those_are_exact() {
        let mut rng = Rng(7);
        let rounded =
            |c: Option<Cents>| c.and_then(Cents::round_half_up).and_then(Cents::to_decimal);
        for _ in 0..20_000 {
            let (a, b) = (
                rng.decimal(1_000_000_000_000, 6),
                rng.decimal(1_000_000_000_000, 6),
            );
            let (ca, cb) = (Cents::of(a).unwrap(), Cents::of(b).unwrap());
            assert_eq!(
                rounded(ca.checked_add(cb)),
                Some(xpath_round2(add(a, b).unwrap())),
                "{a} + {b}"
            );
            assert_eq!(
                rounded(ca.checked_sub(cb)),
                Some(xpath_round2(sub(a, b).unwrap())),
                "{a} - {b}"
            );
            assert_eq!(rounded(Some(ca)), Some(xpath_round2(a)), "{a}");
            assert_eq!(ca == cb, a == b, "{a} {b}");
            assert_eq!(ca.cmp(&cb), a.cmp(&b), "{a} {b}");
            assert_eq!(
                ca.checked_add(cb).and_then(Cents::to_decimal_exact),
                add(a, b)
            );
            assert_eq!(ca.to_decimal_exact(), Some(a), "{a}");
            assert_eq!(ca.checked_neg().and_then(Cents::checked_neg), Some(ca));
            // Half down is half up mirrored.
            assert_eq!(
                ca.round_half_down(),
                ca.checked_neg()
                    .and_then(Cents::round_half_up)
                    .and_then(Cents::checked_neg),
                "{a}"
            );
            let s = d("0.02");
            assert_eq!(
                Cents::slack(ca, cb, Cents::of(s).unwrap()),
                slack(a, b, s),
                "{a} {b}"
            );
        }
    }

    /// Where `rust_decimal` rounds a sum or difference (its mantissa holds 28 to 29 digits),
    /// `Cents` stays exact, as Saxon's `xs:decimal` does: the cases behind the
    /// extreme-magnitude fixtures `ibr-co-14#5`, `aligned-ibrp-z-08#2`, `aligned-ibrp-s-08#5`
    /// and `aligned-ibrp-004#3`.
    #[test]
    fn cents_are_exact_where_rust_decimal_rounds() {
        let c = |s: &str| Cents::of(d(s)).unwrap();
        let (big, tiny) = (
            d("10000000000000000000000000"),
            d("0.0049999999999999999999999"),
        );
        // ibr-co-14: the rounded sum is .00, not .01.
        assert_eq!(
            xpath_round2(add(big, tiny).unwrap()),
            d("10000000000000000000000000.01")
        );
        let exact = Cents::sum([big, tiny]).unwrap();
        assert_eq!(
            exact.round_half_up().and_then(Cents::to_decimal),
            Some(d("10000000000000000000000000.00"))
        );
        assert_eq!(exact.to_decimal_exact(), None, "32 significant digits");
        // -08: the difference is not ...9.995.
        assert_eq!(sub(big, tiny), Some(d("9999999999999999999999999.995")));
        assert_ne!(
            c("10000000000000000000000000").checked_sub(c("0.0049999999999999999999999")),
            Some(c("9999999999999999999999999.995"))
        );
        // s-08: 0.0200000000000000000000001 below is outside the 0.02 slack.
        let below = sub(big, d("0.0200000000000000000000001")).unwrap();
        assert!(slack(big, below, d("0.02")));
        let below = c("10000000000000000000000000")
            .checked_sub(c("0.0200000000000000000000001"))
            .unwrap();
        assert!(!Cents::slack(
            c("10000000000000000000000000"),
            below,
            c("0.02")
        ));
        // 004: gross minus a discount of 1e-25 is not the gross.
        assert_eq!(sub(big, d("0.0000000000000000000000001")), Some(big));
        assert_ne!(
            c("10000000000000000000000000").checked_sub(c("0.0000000000000000000000001")),
            Some(c("10000000000000000000000000"))
        );
    }

    #[test]
    fn cents_render_within_the_contract_grammar() {
        let c = |s: &str| Cents::of(d(s)).unwrap();
        assert_eq!(c("1050").to_decimal().unwrap().to_string(), "1050.00");
        assert_eq!(c("-0.05").to_decimal().unwrap().to_string(), "-0.05");
        assert_eq!(c("0.001").to_decimal(), None);
        assert_eq!(
            c("99999999999999999999999999.99")
                .to_decimal()
                .unwrap()
                .to_string(),
            "99999999999999999999999999.99"
        );
        assert_eq!(
            c("999999999999999999999999999.9")
                .to_decimal()
                .unwrap()
                .to_string(),
            "999999999999999999999999999.9"
        );
        assert_eq!(
            c("9999999999999999999999999999")
                .to_decimal()
                .unwrap()
                .to_string(),
            "9999999999999999999999999999"
        );
        let too_wide = Cents {
            whole: 99_999_999_999_999_999_999_999_999_999,
            sub: 0,
        };
        assert_eq!(too_wide.to_decimal(), None);
        assert_eq!(too_wide.to_decimal_exact(), None);
        assert_eq!(
            Cents::of(d("0.0000000000000000000000000001")),
            Some(Cents { whole: 0, sub: 1 })
        );
        assert_eq!(
            Cents::of(d("-0.0000000000000000000000000001")),
            Some(Cents {
                whole: -1,
                sub: SUB_PER_CENT - 1
            })
        );
        for exact in [
            "0.001",
            "-0.001",
            "1050.125",
            "-1050.125",
            "0.0000000000000000000000000001",
            "-9999999999999999999999999.995",
            "123456789012345678901234.5678",
        ] {
            assert_eq!(
                c(exact).to_decimal_exact().unwrap().to_string(),
                exact,
                "{exact}"
            );
        }
        assert_eq!(c("7.10").to_decimal_exact().unwrap().to_string(), "7.10");
        assert_eq!(c("7.1000").to_decimal_exact().unwrap().to_string(), "7.10");
        assert_eq!(c("-0.00"), Cents::ZERO);
        assert!(c("1.00").is_whole() && !c("1.001").is_whole());
        assert_eq!(c("-0.001").floor_cents(), -1);
        assert_eq!(Cents::sum(Vec::<Decimal>::new()), Some(Cents::ZERO));
    }

    /// The extremes of `rust_decimal` convert and add without panicking.
    #[test]
    fn cents_never_panic_at_the_limits() {
        for x in [Decimal::MAX, Decimal::MIN, Decimal::ZERO, d("-0.00")] {
            let c = Cents::of(x).unwrap();
            assert!(c.checked_add(c).is_some() && c.checked_sub(c).is_some());
            let _ = c.round_half_up().and_then(Cents::to_decimal);
            let _ = c.to_decimal_exact();
            let _ = Cents::slack(c, c, c);
        }
        let max = Cents {
            whole: i128::MAX,
            sub: SUB_PER_CENT - 1,
        };
        assert_eq!(max.checked_add(max), None);
        assert_eq!(max.round_half_up(), None);
        assert_eq!(max.to_decimal_exact(), None);
        assert!(!Cents::slack(max, max, max));
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
