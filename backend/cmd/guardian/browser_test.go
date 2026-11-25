package main

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"testing"
)

// The browser suite starts this test binary with a local Telegram test double.
func TestBrowserBackend(t *testing.T) {
	configPath := os.Getenv("GUARDIAN_TEST_CONFIG")
	if configPath == "" {
		t.Skip("browser suite only")
	}
	endpoint, err := url.Parse(os.Getenv("GUARDIAN_TEST_TELEGRAM"))
	if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" {
		t.Fatal("browser suite requires a loopback Telegram endpoint")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, configPath, localTelegram{endpoint}, nil); err != nil {
		t.Fatal(err)
	}
}

type localTelegram struct{ endpoint *url.URL }

func (l localTelegram) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.URL.Scheme = l.endpoint.Scheme
	copy.URL.Host = l.endpoint.Host
	copy.Host = l.endpoint.Host
	return http.DefaultTransport.RoundTrip(copy)
}
