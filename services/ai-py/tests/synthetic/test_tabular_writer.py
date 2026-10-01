"""The template writer is reproducible and the importer reads its output back exactly (AC-6)."""

import io
import zipfile

import pytest

from ai.agents.extraction.tabular.importer import import_tabular
from ai.synthetic.generator import generate
from ai.synthetic.tabular import ZIP_TIME, headers, write_csv, write_xlsx


@pytest.mark.parametrize("lang", ["ar", "en"])
def test_round_trip_is_exact(lang):
    for s in range(20):
        truths = [generate(500 + s * 4 + k, lang).truth for k in range(1 + s % 4)]
        for fmt, data in (("xlsx", write_xlsx(truths, lang)), ("csv", write_csv(truths, lang)),
                          ("csv", write_csv(truths, lang, encoding="cp1256"))):
            res = import_tabular(data, fmt)
            assert [i.invoice for i in res.invoices] == truths, (fmt, s)
            assert [i.source_ordinal for i in res.invoices] == list(range(len(truths)))


def test_xlsx_bytes_are_pinned():
    truths = [generate(7, "ar").truth]
    data = write_xlsx(truths, "ar")
    assert data == write_xlsx(truths, "ar")
    zf = zipfile.ZipFile(io.BytesIO(data))
    assert {i.date_time for i in zf.infolist()} == {ZIP_TIME}
    assert zf.namelist() == sorted(zf.namelist())
    assert b"2026-01-01T00:00:00Z" in zf.read("docProps/core.xml")


def test_headers_are_localised():
    assert headers("en")[0] == "Invoice number" and headers("ar")[0] == "رقم الفاتورة"
