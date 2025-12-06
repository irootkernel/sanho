package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	kkachihttp "github.com/SeventeenthEarth/kkachi/internal/interface/http"
)

func TestHealthz(t *testing.T) {
	srv := kkachihttp.NewHTTPServer(":5789", nil, nil, nil, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	if !body["ok"] {
		t.Errorf("Expected ok: true, got %v", body)
	}
}
