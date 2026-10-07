package trackb

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
)

// firmResolver maps a Zitadel organisation to its Firm for httpx.Firm. It is the same lookup as
// httpapi.PGStore.FirmIDForOrg; trackb cannot import httpapi (httpapi.WithTrackB imports trackb).
type firmResolver struct{ pool *pgxpool.Pool }

func (r firmResolver) FirmIDForOrg(ctx context.Context, orgID string) (uuid.UUID, error) {
	f, err := db.FirmByOrg(ctx, r.pool, orgID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("firm by org: %w", err)
	}
	return f.ID, nil
}
