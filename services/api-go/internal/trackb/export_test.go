package trackb

import "github.com/menem2024/uae-platform/services/api-go/internal/agents"

// Hub exposes the SSE hub to the external tests.
func (a *App) Hub() *agents.Hub { return a.hub }
