//! Deterministic XML serialisation (spec 5.3.1): an element tree built by the emitters, written
//! as `<?xml version="1.0" encoding="UTF-8"?>`, LF line endings, two-space indentation, one
//! element per line, a trailing newline, attributes in the order the emitter adds them, text
//! escaped by quick-xml.
//!
//! The tree never holds an empty element (CI rule 4, `ibr-079`): [`El::push`] drops a child that
//! has neither text nor children, and [`El::leaf`] is only built from text that is present.

use std::borrow::Cow;

use super::ExportError;

/// The XML declaration every export starts with.
pub const DECLARATION: &str = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n";

/// One element: a qualified name (`cbc:ID`, `cac:Party`, or the unprefixed root), attributes in
/// write order, and either text or children.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct El<'a> {
    name: &'static str,
    attrs: Vec<(&'static str, Cow<'a, str>)>,
    text: Option<Cow<'a, str>>,
    kids: Vec<El<'a>>,
}

impl<'a> El<'a> {
    /// An aggregate; it is written only when at least one child is pushed.
    pub fn new(name: &'static str) -> Self {
        El {
            name,
            attrs: Vec::new(),
            text: None,
            kids: Vec::new(),
        }
    }

    /// A basic element with text. The caller passes text that is present (non-empty after the
    /// `doc::text` trim); empty text would make an empty element, so it is dropped by `push`.
    pub fn leaf(name: &'static str, text: impl Into<Cow<'a, str>>) -> Self {
        let text = text.into();
        El {
            name,
            attrs: Vec::new(),
            text: (!text.is_empty()).then_some(text),
            kids: Vec::new(),
        }
    }

    /// Adds an attribute, always (an XSD-required one such as `currencyID`, even when empty).
    pub fn attr(mut self, name: &'static str, value: impl Into<Cow<'a, str>>) -> Self {
        self.attrs.push((name, value.into()));
        self
    }

    /// Adds an attribute when `value` is present.
    pub fn attr_opt(self, name: &'static str, value: Option<&'a str>) -> Self {
        match value {
            Some(v) => self.attr(name, v),
            None => self,
        }
    }

    /// Appends a child unless it is absent or empty.
    pub fn push(&mut self, child: impl Into<Option<El<'a>>>) -> &mut Self {
        if let Some(child) = child.into()
            && !child.is_empty()
        {
            self.kids.push(child);
        }
        self
    }

    /// `push` in builder form.
    pub fn with(mut self, child: impl Into<Option<El<'a>>>) -> Self {
        self.push(child);
        self
    }

    /// No text and no children: such an element is never written.
    pub fn is_empty(&self) -> bool {
        self.text.is_none() && self.kids.is_empty()
    }

    /// `Some(self)` unless empty.
    pub fn non_empty(self) -> Option<Self> {
        (!self.is_empty()).then_some(self)
    }

    pub fn name(&self) -> &'static str {
        self.name
    }

    pub fn kids(&self) -> &[El<'a>] {
        &self.kids
    }
}

/// Characters XML 1.0 forbids (U+0000-U+0008, U+000B, U+000C, U+000E-U+001F, U+FFFE, U+FFFF;
/// Rust strings hold no surrogates). Shared with `AE-EXP-005`, which reports them before export.
pub fn is_forbidden_xml_char(c: char) -> bool {
    matches!(c,
        '\u{0}'..='\u{8}' | '\u{B}' | '\u{C}' | '\u{E}'..='\u{1F}' | '\u{FFFE}' | '\u{FFFF}')
}

fn check(element: &'static str, s: &str) -> Result<(), ExportError> {
    match s.chars().find(|&c| is_forbidden_xml_char(c)) {
        Some(ch) => Err(ExportError::ForbiddenChar { element, ch }),
        None => Ok(()),
    }
}

/// Serialises `root` as a complete document.
pub fn serialize(root: &El<'_>) -> Result<Vec<u8>, ExportError> {
    let mut out = String::with_capacity(4096);
    out.push_str(DECLARATION);
    write(&mut out, root, 0)?;
    Ok(out.into_bytes())
}

fn indent(out: &mut String, depth: usize) {
    for _ in 0..depth {
        out.push_str("  ");
    }
}

fn write(out: &mut String, el: &El<'_>, depth: usize) -> Result<(), ExportError> {
    indent(out, depth);
    out.push('<');
    out.push_str(el.name);
    for (name, value) in &el.attrs {
        check(el.name, value)?;
        out.push(' ');
        out.push_str(name);
        out.push_str("=\"");
        // Attribute-value normalisation would turn a raw TAB or LF into a space.
        let escaped = quick_xml::escape::escape(value.as_ref());
        for c in escaped.chars() {
            match c {
                '\t' => out.push_str("&#9;"),
                '\n' => out.push_str("&#10;"),
                c => out.push(c),
            }
        }
        out.push('"');
    }
    match (&el.text, el.kids.is_empty()) {
        (Some(text), _) => {
            check(el.name, text)?;
            out.push('>');
            out.push_str(&quick_xml::escape::partial_escape(text.as_ref()));
            out.push_str("</");
            out.push_str(el.name);
            out.push_str(">\n");
        }
        (None, true) => out.push_str("/>\n"),
        (None, false) => {
            out.push_str(">\n");
            for kid in &el.kids {
                write(out, kid, depth + 1)?;
            }
            indent(out, depth);
            out.push_str("</");
            out.push_str(el.name);
            out.push_str(">\n");
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use sha2::{Digest, Sha256};

    fn sample() -> El<'static> {
        El::new("Invoice")
            .attr("xmlns", "urn:x")
            .attr("xmlns:cac", "urn:cac")
            .with(El::leaf("cbc:ID", "INV-1"))
            .with(El::new("cac:Empty"))
            .with(
                El::new("cac:Party").with(
                    El::leaf("cbc:Amount", "10.50")
                        .attr("currencyID", "AED")
                        .attr_opt("unitCode", None),
                ),
            )
            .with(El::leaf("cbc:Note", ""))
            .with(None)
    }

    #[test]
    fn output_has_a_fixed_header_lf_two_space_indent_and_a_trailing_newline() {
        let xml = String::from_utf8(serialize(&sample()).unwrap()).unwrap();
        assert_eq!(
            xml,
            "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n\
             <Invoice xmlns=\"urn:x\" xmlns:cac=\"urn:cac\">\n\
             \x20 <cbc:ID>INV-1</cbc:ID>\n\
             \x20 <cac:Party>\n\
             \x20   <cbc:Amount currencyID=\"AED\">10.50</cbc:Amount>\n\
             \x20 </cac:Party>\n\
             </Invoice>\n"
        );
        assert!(!xml.contains('\r'));
    }

    #[test]
    fn two_serialisations_have_the_same_sha256() {
        let a = Sha256::digest(serialize(&sample()).unwrap());
        let b = Sha256::digest(serialize(&sample()).unwrap());
        assert_eq!(a, b);
    }

    #[test]
    fn empty_elements_are_never_written() {
        let mut el = El::new("cac:A");
        el.push(El::new("cac:B").with(El::new("cac:C")));
        el.push(El::leaf("cbc:D", ""));
        assert!(el.is_empty());
        assert!(el.clone().non_empty().is_none());
        assert_eq!(El::new("cac:X").with(el).kids().len(), 0);
    }

    #[test]
    fn text_and_attributes_are_escaped_and_read_back_unchanged() {
        let text = "a < b & c > d \"q\" 'a'\r\nline";
        let attr = "a\"b<c&d\te\nf'g\rh";
        let root = El::new("R")
            .with(El::leaf("Note", text))
            .with(El::leaf("ID", "x").attr("name", attr));
        let xml = String::from_utf8(serialize(&root).unwrap()).unwrap();
        assert!(
            xml.contains("<Note>a &lt; b &amp; c &gt; d \"q\" 'a'&#13;\nline</Note>"),
            "{xml}"
        );
        assert!(
            xml.contains("name=\"a&quot;b&lt;c&amp;d&#9;e&#10;f&apos;g&#13;h\""),
            "{xml}"
        );
        // A conforming parser (line-end and attribute-value normalisation) reads back the
        // original strings.
        let doc = roxmltree::Document::parse(&xml).unwrap();
        let note = doc.descendants().find(|n| n.has_tag_name("Note")).unwrap();
        assert_eq!(note.text(), Some(text));
        let id = doc.descendants().find(|n| n.has_tag_name("ID")).unwrap();
        assert_eq!(id.attribute("name"), Some(attr));
    }

    #[test]
    fn a_forbidden_character_is_an_error_not_output() {
        for (bad, cp) in [
            ("a\u{1}b", '\u{1}'),
            ("\u{0}", '\u{0}'),
            ("x\u{B}", '\u{B}'),
            ("x\u{C}", '\u{C}'),
            ("x\u{1F}", '\u{1F}'),
            ("x\u{FFFE}", '\u{FFFE}'),
            ("x\u{FFFF}", '\u{FFFF}'),
        ] {
            let text = El::new("R").with(El::leaf("cbc:Note", bad));
            assert_eq!(
                serialize(&text),
                Err(ExportError::ForbiddenChar {
                    element: "cbc:Note",
                    ch: cp
                })
            );
            let attr = El::new("R").with(El::leaf("cbc:ID", "1").attr("schemeID", bad));
            assert_eq!(
                serialize(&attr),
                Err(ExportError::ForbiddenChar {
                    element: "cbc:ID",
                    ch: cp
                })
            );
        }
        for ok in [
            '\t',
            '\n',
            '\r',
            ' ',
            '\u{7F}',
            '\u{FFFD}',
            '\u{10000}',
            'ع',
        ] {
            assert!(!is_forbidden_xml_char(ok), "{ok:?}");
        }
    }
}
