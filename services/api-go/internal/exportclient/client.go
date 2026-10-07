// Package exportclient builds the connect-go client for validator-rs's ExportService.
package exportclient

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

// MaxMessageBytes is the gRPC message limit in both directions (a UBL document may be large).
const MaxMessageBytes = 16 << 20

// callTimeout bounds one Export call (connect, send, wait, receive a document of up to 16 MiB).
const callTimeout = 30 * time.Second

// New returns a gRPC (not Connect-protocol) client for the ExportService over plaintext HTTP/2
// (h2c), traced with otelconnect, with 16 MiB read and send limits.
func New(addr string) (compliancev1connect.ExportServiceClient, error) {
	return newClient(addr, callTimeout)
}

func newClient(addr string, timeout time.Duration) (compliancev1connect.ExportServiceClient, error) {
	otelInterceptor, err := otelconnect.NewInterceptor()
	if err != nil {
		return nil, fmt.Errorf("otelconnect interceptor: %w", err)
	}
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	httpClient := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Protocols: &protocols,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, addr)
			},
		},
	}
	return compliancev1connect.NewExportServiceClient(httpClient, addr,
		connect.WithGRPC(),
		connect.WithReadMaxBytes(MaxMessageBytes),
		connect.WithSendMaxBytes(MaxMessageBytes),
		connect.WithInterceptors(otelInterceptor),
	), nil
}
