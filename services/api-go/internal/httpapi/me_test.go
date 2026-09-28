package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestMe(t *testing.T) {
	firm := uuid.New()
	h := NewRouter(fakeVerifier{}, fakeStore{firm: firm}, &fakePub{}, fakeLimiter{ok: true}, func(context.Context) error { return nil })

	rec := do(h, "GET", "/v1/me", "tok-a", "")
	if rec.Code != 200 {
		t.Fatalf("me: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		FirmID     string  `json:"firm_id"`
		FirmName   string  `json:"firm_name"`
		BrandColor *string `json:"brand_color"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.FirmID != firm.String() || out.FirmName != "Demo Firm A" || out.BrandColor != nil {
		t.Errorf("got %+v", out)
	}
	if rec := do(h, "GET", "/v1/me", "tok-x", ""); rec.Code != 403 {
		t.Errorf("unknown org: %d", rec.Code)
	}
	if rec := do(h, "GET", "/v1/me", "", ""); rec.Code != 401 {
		t.Errorf("no token: %d", rec.Code)
	}
}
