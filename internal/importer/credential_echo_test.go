package importer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A provider that echoes the credential in its error message must not get it into the import's error text,
// which is stored as the job's error summary and logged.
func TestProviderErrorNeverCarriesTheCredential(t *testing.T) {
	echo := func(field string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			msg := "invalid credentials: " + r.Header.Get("Authorization")
			w.WriteHeader(http.StatusUnauthorized)
			if field == "errors" {
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": msg}}})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"errorMessages": []string{msg}})
			}
		}))
	}
	t.Run("linear", func(t *testing.T) {
		srv := echo("errors")
		defer srv.Close()
		const key = "lin_api_SECRETSECRETSECRET"
		row, _ := newLinearSource(context.Background(), key, "TEAM", srv.URL, srv.Client()).Next()
		if row.Err == nil {
			t.Fatal("want an error from a 401")
		}
		if strings.Contains(row.Err.Error(), key) {
			t.Fatalf("error carries the API key: %q", row.Err)
		}
	})
	t.Run("jira", func(t *testing.T) {
		srv := echo("errorMessages")
		defer srv.Close()
		src := newJiraSource(context.Background(), "me@corp.com:SECRETSECRETSECRET", "PROJ", srv.URL, srv.Client())
		row, _ := src.Next()
		if row.Err == nil {
			t.Fatal("want an error from a 401")
		}
		if strings.Contains(row.Err.Error(), src.client.auth) {
			t.Fatalf("error carries the credential: %q", row.Err)
		}
	})
}
