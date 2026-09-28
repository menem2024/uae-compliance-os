// Package validator builds the connect-go client for validator-rs.
package validator

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"

	"github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1/compliancev1connect"
)

// New returns a gRPC (not Connect-protocol) client for validator-rs over
// plaintext HTTP/2 (h2c), traced with otelconnect so the trace continues
// into the Rust service.
func New(addr string) (compliancev1connect.ValidatorServiceClient, error) {
	otelInterceptor, err := otelconnect.NewInterceptor()
	if err != nil {
		return nil, fmt.Errorf("otelconnect interceptor: %w", err)
	}
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	httpClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Protocols: &protocols,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, addr)
			},
		},
	}
	return compliancev1connect.NewValidatorServiceClient(httpClient, addr,
		connect.WithGRPC(),
		connect.WithInterceptors(otelInterceptor),
	), nil
}
