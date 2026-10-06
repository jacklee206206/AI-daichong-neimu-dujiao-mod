// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package cloudflaresaas

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/modules/reseller/contract"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(result any) *http.Response {
	b, _ := json.Marshal(map[string]any{"success": true, "result": result})
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(b)))}
}
func clientForTest(t *testing.T) *Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("test-platform-token-not-real"), 0600); err != nil {
		t.Fatal(err)
	}
	return New(config.CloudflareSaaSConfig{ZoneID: "platformzone123", APITokenFile: path, FallbackOrigin: "origin.example.com"})
}
func activeHostname() any {
	return map[string]any{"id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "hostname": "shop.example.net", "status": "active", "ssl": map[string]any{"type": "dv", "method": "http", "status": "active", "validation_records": []any{map[string]any{"txt_name": "_acme-challenge.shop.example.net", "txt_value": "public-proof"}}}, "ownership_verification": map[string]any{"type": "txt", "name": "_cf-custom-hostname.shop.example.net", "value": "ownership"}}
}

func TestCreateAndRecoverExactHostname(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "recover"}[existing], func(t *testing.T) {
			c := clientForTest(t)
			calls := 0
			c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "api.cloudflare.com" || r.Header.Get("Authorization") != "Bearer test-platform-token-not-real" {
					t.Fatal("unexpected destination/auth")
				}
				if strings.HasSuffix(r.URL.Path, "fallback_origin") {
					return response(map[string]string{"origin": "origin.example.com", "status": "active"}), nil
				}
				if r.Method == "GET" {
					if r.URL.Query().Get("hostname") != "shop.example.net" {
						t.Fatal("unscoped list")
					}
					if existing {
						return response([]any{activeHostname()}), nil
					}
					return response([]any{}), nil
				}
				var b map[string]any
				json.NewDecoder(r.Body).Decode(&b)
				if b["hostname"] != "shop.example.net" || b["custom_origin_server"] != nil {
					t.Fatalf("unsafe create %v", b)
				}
				ssl := b["ssl"].(map[string]any)
				if ssl["method"] != "http" || ssl["type"] != "dv" {
					t.Fatal("wrong ssl")
				}
				return response(activeHostname()), nil
			})
			row, err := c.EnsureHostname(context.Background(), "shop.example.net", "")
			if err != nil {
				t.Fatal(err)
			}
			if row.ID != "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" || row.Status != "active" || len(row.ValidationRecords) != 2 {
				t.Fatalf("bad output %+v", row)
			}
			want := 3
			if existing {
				want = 2
			}
			if calls != want {
				t.Fatalf("calls %d", calls)
			}
		})
	}
}
func TestFailsClosedAndDoesNotLeakToken(t *testing.T) {
	for _, scenario := range []string{"token_missing", "world_readable", "fallback_wrong", "fallback_pending", "redirect", "transport_error", "wrong_hostname", "wrong_origin", "wrong_id"} {
		t.Run(scenario, func(t *testing.T) {
			c := clientForTest(t)
			if scenario == "token_missing" {
				os.Remove(c.cfg.APITokenFile)
			}
			if scenario == "world_readable" {
				os.Chmod(c.cfg.APITokenFile, 0644)
			}
			calls := 0
			c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if scenario == "redirect" {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.test/"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				if scenario == "transport_error" {
					return nil, errors.New("test-platform-token-not-real")
				}
				if strings.HasSuffix(r.URL.Path, "fallback_origin") {
					origin, status := "origin.example.com", "active"
					if scenario == "fallback_wrong" {
						origin = "other.example.com"
					}
					if scenario == "fallback_pending" {
						status = "pending_deployment"
					}
					return response(map[string]string{"origin": origin, "status": status}), nil
				}
				h := activeHostname().(map[string]any)
				if scenario == "wrong_hostname" {
					h["hostname"] = "someone.else.net"
				}
				if scenario == "wrong_origin" {
					h["custom_origin_server"] = "other.example.com"
				}
				if scenario == "wrong_id" {
					h["id"] = "different"
				}
				return response(h), nil
			})
			_, err := c.EnsureHostname(context.Background(), "shop.example.net", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
			if err == nil {
				t.Fatal("unexpected success")
			}
			if strings.Contains(err.Error(), "test-platform-token") || strings.Contains(err.Error(), c.cfg.APITokenFile) {
				t.Fatal("secret leak")
			}
			if (scenario == "token_missing" || scenario == "world_readable") && calls != 0 {
				t.Fatal("request without valid token")
			}
		})
	}
}
func TestRejectsInvalidPathInputs(t *testing.T) {
	c := clientForTest(t)
	c.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unsafe API call"); return nil, nil })
	for _, host := range []string{"evil.net/path", "x.net?x", "localhost", "foo..net"} {
		if _, err := c.EnsureHostname(context.Background(), host, ""); !errors.Is(err, contract.ErrSaaSRequest) {
			t.Fatal(err)
		}
	}
	if _, err := c.EnsureHostname(context.Background(), "shop.example.net", "../other"); err == nil {
		t.Fatal("unsafe id")
	}
}
