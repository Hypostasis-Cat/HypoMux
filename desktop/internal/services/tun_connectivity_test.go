package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeTUNConnectivityAcceptsRealHTTPResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	original := tunConnectivityURLs
	tunConnectivityURLs = []string{server.URL}
	t.Cleanup(func() { tunConnectivityURLs = original })

	detail, err := probeTUNConnectivity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if detail == "" {
		t.Fatal("expected connectivity evidence")
	}
}

func TestProbeTUNConnectivityRejectsUnavailableEndpoints(t *testing.T) {
	original := tunConnectivityURLs
	tunConnectivityURLs = []string{"http://127.0.0.1:1/"}
	t.Cleanup(func() { tunConnectivityURLs = original })
	if _, err := probeTUNConnectivity(context.Background()); err == nil {
		t.Fatal("expected failed connectivity probe")
	}
}
