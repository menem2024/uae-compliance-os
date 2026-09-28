package main

import (
	"net/http"
	"testing"
)

// TestNewHTTPServerSetsHardenedTimeouts guards against an http.Server with
// no ReadTimeout/WriteTimeout/IdleTimeout: an unauthenticated caller can open
// a connection, send nothing (or trickle a request), and hold it open
// forever, exhausting file descriptors and goroutines. ReadHeaderTimeout
// alone does not bound that (it only covers reading the header).
func TestNewHTTPServerSetsHardenedTimeouts(t *testing.T) {
	srv := newHTTPServer(":0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout not set")
	}
	if srv.ReadTimeout <= 0 {
		t.Error("ReadTimeout not set")
	}
	if srv.WriteTimeout <= 0 {
		t.Error("WriteTimeout not set")
	}
	if srv.IdleTimeout <= 0 {
		t.Error("IdleTimeout not set")
	}
	// ReadTimeout must be generous enough to receive a full MaxBodyBytes
	// upload from a slow client, not just headers.
	if srv.ReadTimeout < srv.ReadHeaderTimeout {
		t.Errorf("ReadTimeout %s is shorter than ReadHeaderTimeout %s", srv.ReadTimeout, srv.ReadHeaderTimeout)
	}
}
