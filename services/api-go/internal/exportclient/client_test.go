package exportclient

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1/compliancev1connect"
)

type fakeExporter struct {
	delay   time.Duration
	xmlSize int
}

func (f fakeExporter) Export(ctx context.Context, req *connect.Request[compliancev1.ExportRequest]) (*connect.Response[compliancev1.ExportResponse], error) {
	if req.Peer().Protocol != connect.ProtocolGRPC {
		return nil, connect.NewError(connect.CodeFailedPrecondition, nil)
	}
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return connect.NewResponse(&compliancev1.ExportResponse{
		Xml:      []byte(strings.Repeat("x", f.xmlSize)),
		Format:   "pint-ae-billing-1.0.4/ubl-2.1",
		Exported: true,
	}), nil
}

func serve(t *testing.T, h compliancev1connect.ExportServiceHandler) string {
	t.Helper()
	mux := http.NewServeMux()
	// Server side of the 16 MiB limits lands in validator-rs (task 9); here the handler accepts anything.
	mux.Handle(compliancev1connect.NewExportServiceHandler(h,
		connect.WithReadMaxBytes(64<<20), connect.WithSendMaxBytes(64<<20)))
	var protos http.Protocols
	protos.SetUnencryptedHTTP2(true)
	srv := &http.Server{Handler: mux, Protocols: &protos}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String()
}

func TestClientSpeaksGRPCOverH2C(t *testing.T) {
	c, err := New(serve(t, fakeExporter{xmlSize: 10}))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Export(context.Background(), connect.NewRequest(&compliancev1.ExportRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Msg.GetExported() || len(resp.Msg.GetXml()) != 10 {
		t.Fatalf("unexpected response %v", resp.Msg)
	}
}

func TestLimitsAre16MiB(t *testing.T) {
	if MaxMessageBytes != 16<<20 {
		t.Fatalf("MaxMessageBytes = %d", MaxMessageBytes)
	}
	// A 15 MiB document passes, a 17 MiB one is refused by the client's read limit.
	c, err := New(serve(t, fakeExporter{xmlSize: 15 << 20}))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Export(context.Background(), connect.NewRequest(&compliancev1.ExportRequest{}))
	if err != nil {
		t.Fatalf("15 MiB response: %v", err)
	}
	if len(resp.Msg.GetXml()) != 15<<20 {
		t.Fatalf("got %d bytes", len(resp.Msg.GetXml()))
	}
	c, err = New(serve(t, fakeExporter{xmlSize: 17 << 20}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Export(context.Background(), connect.NewRequest(&compliancev1.ExportRequest{}))
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("17 MiB response: code %v err %v", connect.CodeOf(err), err)
	}
	// And the send limit refuses a 17 MiB request before it leaves the process.
	big := &compliancev1.ExportRequest{RulesetVersion: strings.Repeat("v", 17<<20)}
	c, err = New(serve(t, fakeExporter{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Export(context.Background(), connect.NewRequest(big)); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("17 MiB request: code %v err %v", connect.CodeOf(err), err)
	}
}

func TestTimeout(t *testing.T) {
	c, err := newClient(serve(t, fakeExporter{delay: 2 * time.Second}), 150*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = c.Export(context.Background(), connect.NewRequest(&compliancev1.ExportRequest{}))
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("timeout took %v", time.Since(start))
	}
}
