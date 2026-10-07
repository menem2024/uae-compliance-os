#!/usr/bin/env bash
# Fetch the official PINT AE 1.0.4 validation artefacts and the OASIS UBL 2.1 XSD closure into
# services/validator-rs/rulesets/pint-ae-1.0.4/upstream/{schematron,examples,xsd}, and verify them
# against the pinned sha256 list (upstream/SHA256SUMS).
#
# Why a fetch script and not vendored files: owner decision D-3 (licence check before vendoring
# OpenPeppol / OASIS artefacts) is still open. Nothing downloaded here is committed
# (upstream/.gitignore); the derived rule TSVs (upstream/rules-*.tsv) are committed. CI runs this
# script and fails on any hash mismatch, so a silent upstream hotfix under the same version
# string cannot slip in.
#
# Usage: scripts/fetch-upstream.sh [--check]
#   --check   only verify (sha256sum -c), download nothing
#
# Offline overrides (both optional):
#   UPSTREAM_ZIP      path to a local copy of resources.zip (still verified against the pinned hash)
#   UPSTREAM_XSD_DIR  path to a local directory holding maindoc/ and common/ (verified via SHA256SUMS)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UP="$ROOT/services/validator-rs/rulesets/pint-ae-1.0.4/upstream"

ZIP_URL="https://docs.peppol.eu/poac/ae/pint-ae/resources.zip"
ZIP_SHA256="1e8b0bd595c672ac9d6fc886ddd0eb8027cb39a1d26bebdcb1ada37b7eee491f"
XSD_BASE="https://docs.oasis-open.org/ubl/os-UBL-2.1/xsd"
XSD_FILES=(
  maindoc/UBL-Invoice-2.1.xsd
  maindoc/UBL-CreditNote-2.1.xsd
  common/CCTS_CCT_SchemaModule-2.1.xsd
  common/UBL-CommonAggregateComponents-2.1.xsd
  common/UBL-CommonBasicComponents-2.1.xsd
  common/UBL-CommonExtensionComponents-2.1.xsd
  common/UBL-CommonSignatureComponents-2.1.xsd
  common/UBL-ExtensionContentDataType-2.1.xsd
  common/UBL-QualifiedDataTypes-2.1.xsd
  common/UBL-SignatureAggregateComponents-2.1.xsd
  common/UBL-SignatureBasicComponents-2.1.xsd
  common/UBL-UnqualifiedDataTypes-2.1.xsd
  common/UBL-XAdESv132-2.1.xsd
  common/UBL-XAdESv141-2.1.xsd
  common/UBL-xmldsig-core-schema-2.1.xsd
)

verify() {
  (cd "$UP" && sha256sum --quiet -c SHA256SUMS)
}

if [[ "${1:-}" == "--check" ]]; then
  verify
  echo "upstream: all files match SHA256SUMS"
  exit 0
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# 1. resources.zip -> schematron (compiled .xslt only) and examples.
if [[ -n "${UPSTREAM_ZIP:-}" ]]; then
  cp "$UPSTREAM_ZIP" "$TMP/resources.zip"
else
  curl -fsSL --retry 3 -o "$TMP/resources.zip" "$ZIP_URL"
fi
echo "$ZIP_SHA256  $TMP/resources.zip" | sha256sum --quiet -c - \
  || { echo "resources.zip does not match the pinned sha256 $ZIP_SHA256" >&2; exit 1; }

unzip -q -o "$TMP/resources.zip" -d "$TMP/zip"
rm -rf "$UP/schematron" "$UP/examples"
for kind in trn-invoice trn-creditnote; do
  mkdir -p "$UP/schematron/$kind" "$UP/examples/$kind"
  cp "$TMP"/zip/"$kind"/schematron/*.xslt "$UP/schematron/$kind/"
  cp "$TMP"/zip/"$kind"/example/*.xml "$UP/examples/$kind/"
done

# 2. UBL 2.1 XSD closure (2 maindoc + 13 common files).
rm -rf "$UP/xsd"
for f in "${XSD_FILES[@]}"; do
  mkdir -p "$UP/xsd/$(dirname "$f")"
  if [[ -n "${UPSTREAM_XSD_DIR:-}" ]]; then
    cp "$UPSTREAM_XSD_DIR/$f" "$UP/xsd/$f"
  else
    curl -fsSL --retry 3 -o "$UP/xsd/$f" "$XSD_BASE/$f"
  fi
done

# 3. Per-file hashes: any mismatch fails.
verify
echo "upstream: fetched and verified ($(grep -c . "$UP/SHA256SUMS") files)"
