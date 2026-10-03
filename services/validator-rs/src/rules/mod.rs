//! The registered rules, one module per family (spec 5.2.4). A family's `RULES` holds exactly
//! the rules whose coverage row is `implemented` (checked by the catalogue tests).

pub mod codelists;
pub mod header;
pub mod lines;
pub mod parties;
pub mod platform;
pub mod totals;
pub mod vat;

use crate::catalog::Family;
use crate::rule::Rule;

/// The registered rules of one family.
pub fn family(family: Family) -> &'static [Rule] {
    match family {
        Family::Header => header::RULES,
        Family::Parties => parties::RULES,
        Family::Lines => lines::RULES,
        Family::Totals => totals::RULES,
        Family::Vat => vat::RULES,
        Family::Codelists => codelists::RULES,
        Family::Platform => platform::RULES,
    }
}

/// Every registered rule, family by family.
pub fn all() -> Vec<&'static Rule> {
    Family::ALL.into_iter().flat_map(family).collect()
}
