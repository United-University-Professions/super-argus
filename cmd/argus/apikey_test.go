package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeKeyServer struct {
	keys    []apiKeyView
	created []map[string]any
	revoked []string
}

func (f *fakeKeyServer) client(t *testing.T, stdin string) (*apikeyClient, *bytes.Buffer) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/login" && r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, `{"error":{"code":"unauthorized","message":"no token"}}`, http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/v1/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "tok"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/api-keys":
			_ = json.NewEncoder(w).Encode(f.keys)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/api-keys":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.created = append(f.created, in)
			w.WriteHeader(http.StatusCreated)
			name, _ := in["name"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "new1", "name": name, "key_prefix": "argus_ab", "label": "", "rate_limit": 0,
				"created_at": "2026-01-01T00:00:00Z", "key": "argus_plaintext_xyz",
			})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/api-keys/"):
			f.revoked = append(f.revoked, strings.TrimPrefix(r.URL.Path, "/api/v1/api-keys/"))
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	out := &bytes.Buffer{}
	c := &apikeyClient{base: srv.URL, http: srv.Client(), in: strings.NewReader(stdin), out: out}
	require.NoError(t, c.login(context.Background(), "a@x.io", "pw"))
	return c, out
}

func TestApikeyList(t *testing.T) {
	revokedAt := "2026-02-01T00:00:00Z"
	f := &fakeKeyServer{keys: []apiKeyView{
		{ID: "k1", Name: "uup-k8s-agent", KeyPrefix: "argus_ab", RateLimit: 0, CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "k2", Name: "old-agent", KeyPrefix: "argus_cd", RateLimit: 100, RevokedAt: &revokedAt, CreatedAt: "2025-01-01T00:00:00Z"},
	}}
	c, out := f.client(t, "")
	keys, err := c.list(context.Background())
	require.NoError(t, err)
	printKeyTable(out, keys)
	require.Contains(t, out.String(), "uup-k8s-agent")
	require.Contains(t, out.String(), "revoked")
	require.Contains(t, out.String(), "active")
}

func TestApikeyListEmpty(t *testing.T) {
	f := &fakeKeyServer{}
	c, out := f.client(t, "")
	keys, err := c.list(context.Background())
	require.NoError(t, err)
	printKeyTable(out, keys)
	require.Contains(t, out.String(), "no API keys")
}

func TestApikeyCreate(t *testing.T) {
	f := &fakeKeyServer{}
	c, _ := f.client(t, "")
	view, key, err := c.create(context.Background(), "uup-k8s-agent", "cluster=uup-k8s", 50)
	require.NoError(t, err)
	require.Equal(t, "uup-k8s-agent", view.Name)
	require.Equal(t, "argus_plaintext_xyz", key)
	require.Equal(t, []map[string]any{{"name": "uup-k8s-agent", "label": "cluster=uup-k8s", "rate_limit": float64(50)}}, f.created)
}

func TestApikeyCreateOmitsUnsetOptionalFields(t *testing.T) {
	f := &fakeKeyServer{}
	c, _ := f.client(t, "")
	_, _, err := c.create(context.Background(), "plain", "", 0)
	require.NoError(t, err)
	require.Equal(t, []map[string]any{{"name": "plain"}}, f.created)
}

func TestApikeyRevokeAsksForConfirmation(t *testing.T) {
	f := &fakeKeyServer{}
	c, out := f.client(t, "y\n")
	require.NoError(t, c.revoke(context.Background(), "k1", false))
	require.Equal(t, []string{"k1"}, f.revoked)
	require.Contains(t, out.String(), "Revoke API key k1? [y/N]")
}

func TestApikeyRevokeDeclinedSendsNothing(t *testing.T) {
	f := &fakeKeyServer{}
	c, _ := f.client(t, "n\n")
	require.EqualError(t, c.revoke(context.Background(), "k1", false), "aborted")
	require.Empty(t, f.revoked)
}

func TestApikeyRevokeYesSkipsPrompt(t *testing.T) {
	f := &fakeKeyServer{}
	c, out := f.client(t, "")
	require.NoError(t, c.revoke(context.Background(), "k1", true))
	require.NotContains(t, out.String(), "[y/N]")
	require.Equal(t, []string{"k1"}, f.revoked)
}
