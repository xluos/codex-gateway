package oauth

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestCallbackServer_ReceivesCodeAndState(t *testing.T) {
	server := NewCallbackServer("127.0.0.1:0", "/auth/callback")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resultCh, err := server.Start(ctx)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	resp, err := http.Get("http://" + server.Address() + "/auth/callback?code=abc&state=xyz")
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	_ = resp.Body.Close()

	select {
	case result := <-resultCh:
		if result.Code != "abc" || result.State != "xyz" {
			t.Fatalf("unexpected callback result: %#v", result)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for callback result")
	}
}
