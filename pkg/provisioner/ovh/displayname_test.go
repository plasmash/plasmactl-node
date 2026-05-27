package ovh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisplayNameFetcher_Fetch_Present(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/dedicated/server/ns1234567.ip-192-0-2-1.net", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"iam":{"displayName":"demo-control-001"}}`))
	}))
	defer srv.Close()

	f := &DisplayNameFetcher{Client: &Client{HTTP: srv.Client(), BaseURL: srv.URL}}
	name, err := f.Fetch(context.Background(), "ns1234567.ip-192-0-2-1.net")
	require.NoError(t, err)
	assert.Equal(t, "demo-control-001", name)
}

func TestDisplayNameFetcher_Fetch_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"iam":{"displayName":""}}`))
	}))
	defer srv.Close()

	f := &DisplayNameFetcher{Client: &Client{HTTP: srv.Client(), BaseURL: srv.URL}}
	name, err := f.Fetch(context.Background(), "ns1234567.ip-192-0-2-1.net")
	require.NoError(t, err)
	assert.Equal(t, "", name)
}

func TestDisplayNameFetcher_Fetch_Null(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"iam":{}}`))
	}))
	defer srv.Close()

	f := &DisplayNameFetcher{Client: &Client{HTTP: srv.Client(), BaseURL: srv.URL}}
	name, err := f.Fetch(context.Background(), "ns1234567.ip-192-0-2-1.net")
	require.NoError(t, err)
	assert.Equal(t, "", name)
}

func TestDisplayNameFetcher_Fetch_PropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"message":"service unavailable"}`))
	}))
	defer srv.Close()

	f := &DisplayNameFetcher{Client: &Client{HTTP: srv.Client(), BaseURL: srv.URL}}
	_, err := f.Fetch(context.Background(), "ns1234567.ip-192-0-2-1.net")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch displayName")
}
