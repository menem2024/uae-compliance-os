package httpapi

import "github.com/menem2024/uae-platform/services/api-go/internal/trackb"

// WithTrackB mounts every Track B route (/v1/firm, /v1/client-companies*, /v1/documents*,
// /v1/agents*) inside the authenticated group. It is the only way Track B routes reach the router.
func WithTrackB(app *trackb.App) RouterOption { return RouterOption(app.RouterOption()) }
