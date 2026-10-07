package fieldpath_test

import (
	"errors"
	"testing"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/fieldpath"
)

func sample() *compliancev1.Invoice {
	return &compliancev1.Invoice{
		InvoiceNumber: "INV-1",
		Seller: &compliancev1.Party{
			Name:              "Seller LLC",
			PostalAddress:     &compliancev1.PostalAddress{CountrySubdivision: "DXB"},
			ElectronicAddress: &compliancev1.Identifier{Id: "1234567890"},
		},
		AllowancesCharges: []*compliancev1.AllowanceCharge{{IsCharge: true}},
		Lines: []*compliancev1.InvoiceLine{
			{Id: "1", Price: &compliancev1.Price{NetPrice: "10.00"}},
			{Id: "2"},
		},
		TaxBreakdown: []*compliancev1.TaxSubtotal{{}, {Category: &compliancev1.TaxCategory{Rate: "5.00"}}},
	}
}

func TestGet(t *testing.T) {
	inv := sample()
	cases := []struct {
		name, path, want string
		err              error
	}{
		{"plain", "invoice_number", "INV-1", nil},
		{"plain absent", "issue_date", "", nil},
		{"nested", "seller.postal_address.country_subdivision", "DXB", nil},
		{"nested absent message", "buyer.postal_address.country_subdivision", "", nil},
		{"indexed", "lines[0].id", "1", nil},
		{"indexed nested", "lines[0].price.net_price", "10.00", nil},
		{"indexed absent nested", "lines[1].price.net_price", "", nil},
		{"index out of range", "lines[7].id", "", nil},
		{"two lists", "tax_breakdown[1].category.rate", "5.00", nil},
		{"bool true", "allowances_charges[0].is_charge", "true", nil},
		{"bool false", "lines[0].id", "1", nil},
		{"bool absent parent", "allowances_charges[3].is_charge", "", nil},
		{"unknown field", "no_such", "", fieldpath.ErrBadPath},
		{"unknown nested", "seller.no_such", "", fieldpath.ErrBadPath},
		{"ends on message", "seller", "", fieldpath.ErrBadPath},
		{"ends on list", "lines", "", fieldpath.ErrBadPath},
		{"index on scalar", "invoice_number[0]", "", fieldpath.ErrBadPath},
		{"missing index on list", "lines.id", "", fieldpath.ErrBadPath},
		{"index on message", "seller[0].name", "", fieldpath.ErrBadPath},
		{"bad grammar", "lines[x].id", "", fieldpath.ErrBadPath},
		{"empty", "", "", fieldpath.ErrBadPath},
		{"invoice prefix", "invoice.invoice_number", "", fieldpath.ErrBadPath},
		{"trailing dot", "seller.", "", fieldpath.ErrBadPath},
		{"negative index", "lines[-1].id", "", fieldpath.ErrBadPath},
		{"camel case", "invoiceNumber", "", fieldpath.ErrBadPath},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := fieldpath.Get(inv, c.path)
			if !errors.Is(err, c.err) || (err == nil && got != c.want) {
				t.Fatalf("Get(%q) = %q, %v; want %q, %v", c.path, got, err, c.want, c.err)
			}
		})
	}
}

func TestGetDoesNotMutate(t *testing.T) {
	inv := sample()
	if _, err := fieldpath.Get(inv, "buyer.postal_address.city"); err != nil {
		t.Fatal(err)
	}
	if inv.Buyer != nil {
		t.Fatal("Get created the buyer message")
	}
}

func TestApplyChanges(t *testing.T) {
	t.Run("sets strings and bools", func(t *testing.T) {
		inv := sample()
		err := fieldpath.ApplyChanges(inv, []fieldpath.FieldChange{
			{Path: "invoice_number", OldValue: "INV-1", NewValue: "INV-2"},
			{Path: "seller.postal_address.country_subdivision", OldValue: "DXB", NewValue: "AUH"},
			{Path: "allowances_charges[0].is_charge", OldValue: "true", NewValue: "false"},
			{Path: "buyer.postal_address.city", OldValue: "", NewValue: "Dubai"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if inv.InvoiceNumber != "INV-2" || inv.Seller.PostalAddress.CountrySubdivision != "AUH" ||
			inv.AllowancesCharges[0].IsCharge || inv.Buyer.PostalAddress.City != "Dubai" {
			t.Fatalf("unexpected result: %v", inv)
		}
	})
	t.Run("old value mismatch leaves the invoice untouched", func(t *testing.T) {
		inv := sample()
		err := fieldpath.ApplyChanges(inv, []fieldpath.FieldChange{
			{Path: "invoice_number", OldValue: "INV-1", NewValue: "INV-2"},
			{Path: "seller.name", OldValue: "wrong", NewValue: "X"},
		})
		if !errors.Is(err, fieldpath.ErrOldValueMismatch) {
			t.Fatalf("err = %v, want ErrOldValueMismatch", err)
		}
		if inv.InvoiceNumber != "INV-1" || inv.Seller.Name != "Seller LLC" {
			t.Fatalf("invoice changed on error: %v", inv)
		}
	})
	t.Run("append at index == len", func(t *testing.T) {
		inv := sample()
		err := fieldpath.ApplyChanges(inv, []fieldpath.FieldChange{
			{Path: "lines[2].id", OldValue: "", NewValue: "3"},
			{Path: "lines[2].price.net_price", OldValue: "", NewValue: "7.50"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(inv.Lines) != 3 || inv.Lines[2].Id != "3" || inv.Lines[2].Price.NetPrice != "7.50" {
			t.Fatalf("lines = %v", inv.Lines)
		}
	})
	t.Run("index beyond len", func(t *testing.T) {
		inv := sample()
		err := fieldpath.ApplyChanges(inv, []fieldpath.FieldChange{{Path: "lines[3].id", OldValue: "", NewValue: "x"}})
		if !errors.Is(err, fieldpath.ErrBadPath) {
			t.Fatalf("err = %v, want ErrBadPath", err)
		}
		if len(inv.Lines) != 2 {
			t.Fatal("list grew on error")
		}
	})
	t.Run("path must end on string or bool", func(t *testing.T) {
		for _, p := range []string{"seller", "lines", "lines[0]", "lines[0].price", "no_such"} {
			err := fieldpath.ApplyChanges(sample(), []fieldpath.FieldChange{{Path: p, NewValue: "x"}})
			if !errors.Is(err, fieldpath.ErrBadPath) {
				t.Errorf("path %q: err = %v, want ErrBadPath", p, err)
			}
		}
	})
	t.Run("bad bool value", func(t *testing.T) {
		err := fieldpath.ApplyChanges(sample(), []fieldpath.FieldChange{
			{Path: "allowances_charges[0].is_charge", OldValue: "true", NewValue: "yes"}})
		if !errors.Is(err, fieldpath.ErrBadPath) {
			t.Fatalf("err = %v, want ErrBadPath", err)
		}
	})
	t.Run("empty changes", func(t *testing.T) {
		if err := fieldpath.ApplyChanges(sample(), nil); !errors.Is(err, fieldpath.ErrNoChanges) {
			t.Fatalf("err = %v, want ErrNoChanges", err)
		}
	})
	t.Run("clearing a value", func(t *testing.T) {
		inv := sample()
		if err := fieldpath.ApplyChanges(inv, []fieldpath.FieldChange{
			{Path: "invoice_number", OldValue: "INV-1", NewValue: ""}}); err != nil {
			t.Fatal(err)
		}
		if inv.InvoiceNumber != "" {
			t.Fatal("not cleared")
		}
	})
}

func TestAgentForbidden(t *testing.T) {
	forbidden := []string{
		"invoice_number", "uuid", "issue_date", "seller_trn", "buyer_trn",
		"seller.tax_registration_identifier", "seller.electronic_address.id", "buyer.electronic_address.id",
		"seller.legal_registration.id", "buyer.legal_registration.id", "principal_id", "beneficiary_id",
		"tax_representative.vat_identifier",
	}
	if len(forbidden) != 13 {
		t.Fatal("the spec lists 13 paths")
	}
	for _, p := range forbidden {
		if !fieldpath.AgentForbidden(p) {
			t.Errorf("%q must be agent-forbidden", p)
		}
	}
	for _, p := range []string{"seller.postal_address.country_subdivision", "lines[0].id", "currency", "invoice_number2", ""} {
		if fieldpath.AgentForbidden(p) {
			t.Errorf("%q must not be agent-forbidden", p)
		}
	}
}
