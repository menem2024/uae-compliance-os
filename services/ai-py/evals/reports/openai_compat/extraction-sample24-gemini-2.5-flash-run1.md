# Extraction accuracy sample (openai_compat)

Models: gemini-2.5-flash. Cases run 24 of 24 planned, 7 errored.

Synthetic dataset `extraction-v1`; stratified by (lang, format, defect), lowest case ids first. Only case ids, field names and pass flags are recorded.

## Overall

- Field accuracy: 68.6% (644/939)
- Invoice exact match: 54.2% (excluding errored cases: 76.5%)
- Verifier: accept 100.0%, review (revise or escalate) 0.0% over 13 verified; verdicts {'accept': 13}
- Calls and tokens: 21 calls, 37424 in, 21394 out (extraction 17 calls, verifier 4 calls)

## By language and format

| tag | cases | field accuracy | exact match |
|---|---|---|---|
| format:image | 12 | 48.8% | 41.7% |
| format:pdf | 12 | 86.4% | 66.7% |
| lang:ar | 12 | 87.8% | 66.7% |
| lang:en | 12 | 50.4% | 41.7% |

## Per field

| field | correct | counted | accuracy |
|---|---|---|---|
| buyer.name | 15 | 24 | 62.5% |
| buyer_trn | 10 | 17 | 58.8% |
| category.code | 21 | 29 | 72.4% |
| category.rate | 22 | 29 | 75.9% |
| currency | 17 | 24 | 70.8% |
| invoice_number | 17 | 24 | 70.8% |
| invoice_type_code | 17 | 24 | 70.8% |
| issue_date | 15 | 24 | 62.5% |
| item.name | 49 | 67 | 73.1% |
| net_amount | 41 | 67 | 61.2% |
| note | 7 | 10 | 70.0% |
| payment_due_date | 11 | 15 | 73.3% |
| price.net_price | 43 | 67 | 64.2% |
| quantity | 49 | 67 | 73.1% |
| seller.name | 17 | 24 | 70.8% |
| seller.postal_address.country_subdivision | 17 | 24 | 70.8% |
| seller_trn | 15 | 24 | 62.5% |
| tax.code | 49 | 67 | 73.1% |
| tax.rate | 50 | 67 | 74.6% |
| tax_amount | 20 | 29 | 69.0% |
| taxable_amount | 19 | 29 | 65.5% |
| total_amount | 15 | 24 | 62.5% |
| totals.line_extension_amount | 15 | 24 | 62.5% |
| totals.payable_amount | 15 | 24 | 62.5% |
| totals.tax_exclusive_amount | 15 | 24 | 62.5% |
| unit_code | 49 | 67 | 73.1% |
| vat_amount | 16 | 24 | 66.7% |

## Verifier by kind

| kind | verified | accept | revise | escalate |
|---|---|---|---|---|
| clean | 8 | 8 | 0 | 0 |
| defect | 5 | 5 | 0 | 0 |

## Failure causes

- Errors: {'model_transient': 6, 'timeout': 1}
- Cases with wrong values (no error): 4
- Cases retried after a transient error: 10

## Cases

| case | pass | error | verdict | wrong fields |
|---|---|---|---|---|
| ar-image-000 | yes | - | accept | - |
| ar-image-001 | no | - | accept | buyer.name |
| ar-image-002 | yes | - | accept | - |
| ar-image-005 | yes | - | accept | - |
| ar-image-007 | no | - | - | issue_date, net_amount, price.net_price, seller_trn, tax_amount, taxable_amount, total_amount, totals.line_extension_amount, totals.payable_amount, totals.tax_exclusive_amount, vat_amount |
| ar-image-008 | yes | - | - | - |
| ar-pdf-000 | yes | - | - | - |
| ar-pdf-001 | yes | - | accept | - |
| ar-pdf-002 | yes | - | accept | - |
| ar-pdf-003 | yes | - | accept | - |
| ar-pdf-004 | no | - | - | buyer.name, buyer_trn, issue_date, net_amount, price.net_price, seller_trn, taxable_amount, total_amount, totals.line_extension_amount, totals.payable_amount, totals.tax_exclusive_amount |
| ar-pdf-006 | no | model_transient | - | buyer.name, buyer_trn, category.code, category.rate, currency, invoice_number, invoice_type_code, issue_date, item.name, net_amount, price.net_price, quantity, seller.name, seller.postal_address.country_subdivision, seller_trn, tax.code, tax.rate, tax_amount, taxable_amount, total_amount, totals.line_extension_amount, totals.payable_amount, totals.tax_exclusive_amount, unit_code, vat_amount |
| en-image-000 | no | model_transient | - | buyer.name, buyer_trn, category.code, category.rate, currency, invoice_number, invoice_type_code, issue_date, item.name, net_amount, note, payment_due_date, price.net_price, quantity, seller.name, seller.postal_address.country_subdivision, seller_trn, tax.code, tax.rate, tax_amount, taxable_amount, total_amount, totals.line_extension_amount, totals.payable_amount, totals.tax_exclusive_amount, unit_code, vat_amount |
| en-image-001 | no | model_transient | - | buyer.name, buyer_trn, category.code, category.rate, currency, invoice_number, invoice_type_code, issue_date, item.name, net_amount, payment_due_date, price.net_price, quantity, seller.name, seller.postal_address.country_subdivision, seller_trn, tax.code, tax.rate, tax_amount, taxable_amount, total_amount, totals.line_extension_amount, totals.payable_amount, totals.tax_exclusive_amount, unit_code, vat_amount |
| en-image-002 | no | model_transient | - | buyer.name, buyer_trn, category.code, category.rate, currency, invoice_number, invoice_type_code, issue_date, item.name, net_amount, note, price.net_price, quantity, seller.name, seller.postal_address.country_subdivision, seller_trn, tax.code, tax.rate, tax_amount, taxable_amount, total_amount, totals.line_extension_amount, totals.payable_amount, totals.tax_exclusive_amount, unit_code, vat_amount |
| en-image-003 | no | model_transient | - | buyer.name, buyer_trn, category.code, category.rate, currency, invoice_number, invoice_type_code, issue_date, item.name, net_amount, payment_due_date, price.net_price, quantity, seller.name, seller.postal_address.country_subdivision, seller_trn, tax.code, tax.rate, tax_amount, taxable_amount, total_amount, totals.line_extension_amount, totals.payable_amount, totals.tax_exclusive_amount, unit_code, vat_amount |
| en-image-007 | no | model_transient | - | buyer.name, buyer_trn, category.code, category.rate, currency, invoice_number, invoice_type_code, issue_date, item.name, net_amount, note, price.net_price, quantity, seller.name, seller.postal_address.country_subdivision, seller_trn, tax.code, tax.rate, tax_amount, taxable_amount, total_amount, totals.line_extension_amount, totals.payable_amount, totals.tax_exclusive_amount, unit_code, vat_amount |
| en-image-010 | yes | - | accept | - |
| en-pdf-000 | no | timeout | - | buyer.name, category.code, category.rate, currency, invoice_number, invoice_type_code, issue_date, item.name, net_amount, payment_due_date, price.net_price, quantity, seller.name, seller.postal_address.country_subdivision, seller_trn, tax.code, tax.rate, tax_amount, taxable_amount, total_amount, totals.line_extension_amount, totals.payable_amount, totals.tax_exclusive_amount, unit_code, vat_amount |
| en-pdf-001 | yes | - | accept | - |
| en-pdf-002 | yes | - | accept | - |
| en-pdf-003 | yes | - | accept | - |
| en-pdf-005 | yes | - | accept | - |
| en-pdf-008 | no | - | accept | net_amount |
