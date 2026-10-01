//go:build integration

package firms_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/firms"
)

func strp(s string) *string { return &s }

// TestPGStoreUpdateOwnFirmOnly covers that Update only ever changes the
// caller's own Firm (the firms_update_own RLS policy, keyed off the row
// itself rather than firm_id), and that brand_color round-trips through null.
func TestPGStoreUpdateOwnFirmOnly(t *testing.T) {
	ctx := context.Background()
	env := dbtest.Setup(t)
	store := firms.PGStore{Pool: env.App}

	before, err := store.Get(ctx, env.FirmB)
	if err != nil {
		t.Fatalf("get firm B: %v", err)
	}

	updated, err := store.Update(ctx, env.FirmA, firms.Patch{Name: strp("Renamed A"),
		BrandColor: firms.Nullable{Set: true, Value: strp("#123ABC")}})
	if err != nil {
		t.Fatalf("update firm A: %v", err)
	}
	if updated.Name != "Renamed A" || updated.BrandColor == nil || *updated.BrandColor != "#123ABC" {
		t.Fatalf("update firm A result: %+v", updated)
	}

	// Reading firm B directly (owner role, bypassing the app pool entirely)
	// confirms firm A's update never touched it.
	var name string
	var color pgtype.Text
	err = env.Owner.QueryRow(ctx, `SELECT name, brand_color FROM firms WHERE id = $1`, env.FirmB).Scan(&name, &color)
	if err != nil {
		t.Fatalf("read firm B as owner: %v", err)
	}
	if name != before.Name || color.Valid != (before.BrandColor != nil) {
		t.Fatalf("firm B changed: name=%q color=%+v want name=%q color=%v", name, color, before.Name, before.BrandColor)
	}

	// brand_color round-trips null.
	cleared, err := store.Update(ctx, env.FirmA, firms.Patch{BrandColor: firms.Nullable{Set: true, Value: nil}})
	if err != nil {
		t.Fatalf("clear brand_color: %v", err)
	}
	if cleared.BrandColor != nil {
		t.Fatalf("brand_color not cleared: %+v", cleared)
	}
	got, err := store.Get(ctx, env.FirmA)
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if got.BrandColor != nil {
		t.Fatalf("brand_color not null after reload: %+v", got)
	}
}
