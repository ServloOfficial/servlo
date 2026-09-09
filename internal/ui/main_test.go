package ui

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/push"
)

// TestMain neutralises the real web-push client for the whole package so no
// test can fire a notification at the developer's actual browser, even through
// the async goroutine in dispatchNotification that can outlive a single test,
// and points servlo's state at a temp directory for the same reason: a handler
// test that reads global config resolves the presets, and resolving one writes
// this install's service password.
func TestMain(m *testing.M) {
	push.HTTPClient = &http.Client{Transport: discardPushTransport{}}
	cleanup := config.IsolateStateForTests()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

type discardPushTransport struct{}

func (discardPushTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusCreated,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}, nil
}
