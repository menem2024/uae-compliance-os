package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

func TestWithFirmRejectsNilFirm(t *testing.T) {
	called := false
	err := db.WithFirm(context.Background(), nil, uuid.Nil, func(*sqlc.Queries) error {
		called = true
		return nil
	})
	if !errors.Is(err, db.ErrNilFirm) {
		t.Fatalf("expected ErrNilFirm, got %v", err)
	}
	if called {
		t.Fatal("fn ran without a firm id")
	}
}
