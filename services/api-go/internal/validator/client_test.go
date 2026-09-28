package validator

import (
	"context"
	"net"
	"net/http"
	"testing"

	"connectrpc.com/connect"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1/compliancev1connect"
)

type grpcOnly struct{}

func (grpcOnly) Validate(_ context.Context, req *connect.Request[compliancev1.ValidateRequest]) (*connect.Response[compliancev1.ValidateResponse], error) {
	if req.Peer().Protocol != connect.ProtocolGRPC {
		return nil, connect.NewError(connect.CodeFailedPrecondition, nil)
	}
	return connect.NewResponse(&compliancev1.ValidateResponse{Run: &compliancev1.ValidationRun{RulesetVersion: "v-" + req.Msg.GetInvoice().GetSellerTrn()}}), nil
}

// TestClientSpeaksGRPCOverH2C mimics tonic: plaintext HTTP/2, gRPC protocol.
func TestClientSpeaksGRPCOverH2C(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle(compliancev1connect.NewValidatorServiceHandler(grpcOnly{}))
	var protos http.Protocols
	protos.SetUnencryptedHTTP2(true)
	srv := &http.Server{Handler: mux, Protocols: &protos}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	c, err := New("http://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Validate(context.Background(), connect.NewRequest(&compliancev1.ValidateRequest{Invoice: &compliancev1.Invoice{SellerTrn: "123"}}))
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Msg.GetRun().GetRulesetVersion(); got != "v-123" {
		t.Fatalf("ruleset = %q", got)
	}
}
