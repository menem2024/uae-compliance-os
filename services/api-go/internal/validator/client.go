// Package validator builds the connect-go client for validator-rs.
package validator

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"golang.org/x/net/http2"

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
	httpClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, addr)
			},
		},
	}
	return compliancev1connect.NewValidatorServiceClient(httpClient, addr,
		connect.WithGRPC(),
		connect.WithInterceptors(otelInterceptor),
	), nil
}
