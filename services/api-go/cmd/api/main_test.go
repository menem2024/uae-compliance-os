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

func TestParseRevalidateArgs(t *testing.T) {
	a, err := parseRevalidateArgs([]string{"--ruleset", "pint-ae@1.0.4+r1"})
	if err != nil || a.ruleset != "pint-ae@1.0.4+r1" || a.rate != 50 || a.firm.String() != "00000000-0000-0000-0000-000000000000" || a.report != "" {
		t.Errorf("defaults: %+v, %v", a, err)
	}
	a, err = parseRevalidateArgs([]string{"--ruleset=x", "--firm", "6f1a1c1e-7d1f-4b36-9e56-5b5c2b8f0a11", "--rate", "5", "--report", "/tmp/r.jsonl"})
	if err != nil || a.ruleset != "x" || a.rate != 5 || a.report != "/tmp/r.jsonl" || a.firm.String() != "6f1a1c1e-7d1f-4b36-9e56-5b5c2b8f0a11" {
		t.Errorf("explicit: %+v, %v", a, err)
	}
	for name, args := range map[string][]string{
		"no ruleset": {},
		"bad firm":   {"--ruleset", "x", "--firm", "nope"},
		"zero rate":  {"--ruleset", "x", "--rate", "0"},
		"junk":       {"--ruleset", "x", "extra"},
		"unknown":    {"--ruleset", "x", "--nope"},
	} {
		if _, err := parseRevalidateArgs(args); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
