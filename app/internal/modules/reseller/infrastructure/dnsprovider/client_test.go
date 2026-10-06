// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package dnsprovider

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	contract "github.com/dujiao-next/internal/modules/reseller/contract"
)

type resolverFunc func(context.Context, string) ([]*net.NS, error)

func (f resolverFunc) LookupNS(ctx context.Context, host string) ([]*net.NS, error) {
	return f(ctx, host)
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func jsonResponse(v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(b)))}
}
func fakeClient(provider string, transport transportFunc) *Client {
	c := New()
	c.now = func() time.Time { return time.Unix(1790812800, 0) }
	ns := map[string]string{Cloudflare: "alice.ns.cloudflare.com.", Aliyun: "dns1.hichina.com.", DNSPod: "f1g1ns1.dnspod.net."}[provider]
	c.resolver = resolverFunc(func(context.Context, string) ([]*net.NS, error) { return []*net.NS{{Host: ns}}, nil })
	c.http.Transport = transport
	return c
}
func desiredRecords() []contract.DNSRecord {
	return []contract.DNSRecord{
		{"A", "example.com", "8.8.4.4"},
		{"TXT", "_opengpt-verification.example.com", "root-proof"},
		{"A", "www.example.com", "8.8.4.4"},
		{"TXT", "_opengpt-verification.www.example.com", "www-proof"},
	}
}
func credentials(provider string) contract.DNSCredentials {
	switch provider {
	case Cloudflare:
		return contract.DNSCredentials{"api_token": "CF-SECRET"}
	case Aliyun:
		return contract.DNSCredentials{"access_key_id": "test-id", "access_key_secret": "test-secret"}
	default:
		return contract.DNSCredentials{"secret_id": "test-id", "secret_key": "test-secret"}
	}
}

func TestDetectNameserverAuthority(t *testing.T) {
	for _, tt := range []struct {
		name string
		ns   []string
		want string
	}{
		{"cloudflare", []string{"Alice.NS.Cloudflare.com.", "bob.ns.cloudflare.com."}, Cloudflare},
		{"aliyun", []string{"dns1.hichina.com.", "ns2.alidns.com."}, Aliyun},
		{"dnspod", []string{"f1g1ns1.dnspod.net.", "f1g1ns2.dnspod.net."}, DNSPod},
		{"lookalike", []string{"alice.ns.cloudflare.com.evil.test."}, "unsupported"},
		{"mixed", []string{"alice.ns.cloudflare.com.", "ns2.alidns.com."}, "unsupported"},
		{"vanity", []string{"ns1.example.com."}, "unsupported"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := New()
			c.resolver = resolverFunc(func(_ context.Context, name string) ([]*net.NS, error) {
				if name != "example.com" {
					t.Fatalf("wrong query: %q", name)
				}
				ns := []*net.NS{}
				for _, v := range tt.ns {
					ns = append(ns, &net.NS{Host: v})
				}
				return ns, nil
			})
			got, err := c.Detect(context.Background(), "EXAMPLE.COM.")
			if err != nil || got.Provider != tt.want {
				t.Fatalf("%+v %v", got, err)
			}
			if got.Supported && len(got.RequiredPermissions) == 0 {
				t.Fatal("missing permission hint")
			}
		})
	}
	c := New()
	c.resolver = resolverFunc(func(context.Context, string) ([]*net.NS, error) { return nil, errors.New("private resolver detail") })
	if _, err := c.Detect(context.Background(), "example.com"); !errors.Is(err, contract.ErrDNSLookupFailed) {
		t.Fatal(err)
	}
}

func TestAllRecordsPreflightBeforeFirstWrite(t *testing.T) {
	for _, bad := range []map[string]any{
		{"type": "A", "name": "www.example.com", "content": "8.8.8.8"},
		{"type": "AAAA", "name": "www.example.com", "content": "2001:4860:4860::8888"},
		{"type": "CNAME", "name": "www.example.com", "content": "other.example.com"},
		{"type": "TXT", "name": "_opengpt-verification.www.example.com", "content": "different-proof"},
		{"type": "A", "name": "example.com", "content": "8.8.4.4", "proxied": true},
	} {
		t.Run(fmt.Sprint(bad["type"], bad["name"]), func(t *testing.T) {
			writes := 0
			c := fakeClient(Cloudflare, func(req *http.Request) (*http.Response, error) {
				if req.Method != "GET" {
					writes++
					return jsonResponse(map[string]any{"success": true, "result": map[string]any{"id": "created"}}), nil
				}
				if strings.HasSuffix(req.URL.Path, "/zones") {
					return jsonResponse(map[string]any{"success": true, "result": []any{map[string]any{"id": "zone", "name": "example.com", "status": "active"}}}), nil
				}
				return jsonResponse(map[string]any{"success": true, "result": []any{bad}}), nil
			})
			err := c.EnsureRecords(context.Background(), "example.com", Cloudflare, credentials(Cloudflare), desiredRecords())
			if !errors.Is(err, contract.ErrDNSRecordConflict) || writes != 0 {
				t.Fatalf("err=%v writes=%d", err, writes)
			}
		})
	}
}

func TestCloudflareCreateOnlyAbsentAndIdempotent(t *testing.T) {
	stored := []map[string]any{{"type": "A", "name": "example.com", "content": "8.8.4.4"}, {"type": "MX", "name": "example.com", "content": "mail.example.com"}, {"type": "TXT", "name": "example.com", "content": "v=spf1 ~all"}}
	writes := 0
	c := fakeClient(Cloudflare, func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme != "https" || req.URL.Host != "api.cloudflare.com" || req.Header.Get("Authorization") != "Bearer CF-SECRET" {
			t.Fatal("wrong authenticated destination")
		}
		if strings.HasSuffix(req.URL.Path, "/zones") {
			if req.URL.Query().Get("name") != "example.com" {
				t.Fatal("inexact zone")
			}
			return jsonResponse(map[string]any{"success": true, "result": []any{map[string]any{"id": "zone", "name": "example.com", "status": "active"}}}), nil
		}
		if req.Method == "GET" {
			return jsonResponse(map[string]any{"success": true, "result": stored}), nil
		}
		if req.Method != "POST" {
			t.Fatal("existing record mutation")
		}
		var body map[string]any
		if json.NewDecoder(req.Body).Decode(&body) != nil {
			t.Fatal("body")
		}
		if body["type"] == "A" && body["proxied"] != false {
			t.Fatal("new A must be DNS-only")
		}
		stored = append(stored, body)
		writes++
		return jsonResponse(map[string]any{"success": true, "result": map[string]any{"id": "created"}}), nil
	})
	for i := 0; i < 2; i++ {
		if err := c.EnsureRecords(context.Background(), "example.com", Cloudflare, credentials(Cloudflare), desiredRecords()); err != nil {
			t.Fatal(err)
		}
	}
	if writes != 3 {
		t.Fatalf("created %d records", writes)
	}
	if _, err := c.Inspect(context.Background(), "example.com", credentials(Cloudflare)); err != nil {
		t.Fatal(err)
	}
}

func TestCloudflareReadsAllPagesAndRefusesWrongZone(t *testing.T) {
	for _, wrongZone := range []bool{false, true} {
		t.Run(fmt.Sprint(wrongZone), func(t *testing.T) {
			calls := 0
			c := fakeClient(Cloudflare, func(req *http.Request) (*http.Response, error) {
				if req.Method != "GET" {
					t.Fatal("must fail before writes")
				}
				if strings.HasSuffix(req.URL.Path, "/zones") {
					name := "example.com"
					if wrongZone {
						name = "unrelated.com"
					}
					return jsonResponse(map[string]any{"success": true, "result": []any{map[string]any{"id": "zone", "name": name, "status": "active"}}}), nil
				}
				calls++
				records := []any{}
				if req.URL.Query().Get("page") == "1" {
					for i := 0; i < 100; i++ {
						records = append(records, map[string]any{"type": "TXT", "name": fmt.Sprintf("other%d.example.com", i), "content": "unrelated"})
					}
				} else {
					records = append(records, map[string]any{"type": "CNAME", "name": "www.example.com", "content": "old.example.com"})
				}
				return jsonResponse(map[string]any{"success": true, "result": records}), nil
			})
			err := c.EnsureRecords(context.Background(), "example.com", Cloudflare, credentials(Cloudflare), desiredRecords())
			if wrongZone {
				if !errors.Is(err, contract.ErrDNSProviderRequest) || calls != 0 {
					t.Fatal(err, calls)
				}
			} else if !errors.Is(err, contract.ErrDNSRecordConflict) || calls != 2 {
				t.Fatal(err, calls)
			}
		})
	}
}

func TestProviderMismatchAndInvalidRecordsNeverSendCredentials(t *testing.T) {
	calls := 0
	c := fakeClient(Cloudflare, func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected") })
	if err := c.EnsureRecords(context.Background(), "example.com", DNSPod, credentials(DNSPod), desiredRecords()); !errors.Is(err, contract.ErrDNSProviderMismatch) {
		t.Fatal(err)
	}
	for _, bad := range []contract.DNSRecord{{"A", "elsewhere.com", "8.8.4.4"}, {"A", "example.com", "127.0.0.1"}, {"MX", "example.com", "mail.example.com"}, {"TXT", "_opengpt-verification.example.com", "unsafe\nvalue"}} {
		if err := c.EnsureRecords(context.Background(), "example.com", Cloudflare, credentials(Cloudflare), []contract.DNSRecord{bad}); !errors.Is(err, contract.ErrDNSRecordInvalid) {
			t.Fatal(err)
		}
	}
	if calls != 0 {
		t.Fatal("credentials sent during failed validation")
	}
}

func TestRedirectAndProviderErrorsAreRedacted(t *testing.T) {
	for _, mode := range []string{"redirect", "transport", "json-error"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			c := fakeClient(Cloudflare, func(req *http.Request) (*http.Response, error) {
				calls++
				switch mode {
				case "redirect":
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.example/steal"}}, Body: io.NopCloser(strings.NewReader("CF-SECRET"))}, nil
				case "transport":
					return nil, errors.New("CF-SECRET transport failure")
				default:
					return jsonResponse(map[string]any{"success": false, "errors": []any{map[string]any{"message": "CF-SECRET"}}}), nil
				}
			})
			err := c.EnsureRecords(context.Background(), "example.com", Cloudflare, credentials(Cloudflare), desiredRecords())
			if !errors.Is(err, contract.ErrDNSProviderRequest) || strings.Contains(err.Error(), "CF-SECRET") || calls != 1 {
				t.Fatal(err, calls)
			}
		})
	}
	c := New()
	c.http.Transport = transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("untrusted host contacted"); return nil, nil })
	req, _ := http.NewRequest("GET", "https://api.cloudflare.com.evil.example/", nil)
	var out any
	if !errors.Is(c.exchange(req, &out), contract.ErrDNSProviderRequest) {
		t.Fatal("untrusted endpoint allowed")
	}
}

func TestAliyunRequestsAreSignedAndIdempotent(t *testing.T) {
	stored := []map[string]any{{"DomainName": "example.com", "RR": "@", "Type": "NS", "Value": "dns1.hichina.com", "Status": "Enable"}}
	writes := 0
	c := fakeClient(Aliyun, func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://alidns.aliyuncs.com/" || req.Method != "POST" {
			t.Fatal("unsafe endpoint")
		}
		raw, _ := io.ReadAll(req.Body)
		params, err := url.ParseQuery(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		signature := params.Get("Signature")
		params.Del("Signature")
		mac := hmac.New(sha1.New, []byte("test-secret&"))
		_, _ = mac.Write([]byte("POST&%2F&" + url.QueryEscape(strings.ReplaceAll(params.Encode(), "+", "%20"))))
		if signature != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
			t.Fatal("bad Alibaba signature")
		}
		if params.Get("DomainName") != "example.com" || params.Get("Version") != "2015-01-09" || params.Get("AccessKeyId") != "test-id" || params.Get("SignatureNonce") == "" {
			t.Fatal("bad params")
		}
		if params.Get("Action") == "DescribeDomainRecords" {
			return jsonResponse(map[string]any{"RequestId": "request", "TotalCount": len(stored), "DomainRecords": map[string]any{"Record": stored}}), nil
		}
		if params.Get("Action") != "AddDomainRecord" {
			t.Fatal("unexpected mutation")
		}
		stored = append(stored, map[string]any{"DomainName": "example.com", "RR": params.Get("RR"), "Type": params.Get("Type"), "Value": params.Get("Value"), "Status": "Enable"})
		writes++
		return jsonResponse(map[string]any{"RequestId": "request", "RecordId": "record"}), nil
	})
	for i := 0; i < 2; i++ {
		if err := c.EnsureRecords(context.Background(), "example.com", Aliyun, credentials(Aliyun), desiredRecords()); err != nil {
			t.Fatal(err)
		}
	}
	if writes != 4 {
		t.Fatal(writes)
	}
}

func TestDNSPodRequestsAreSignedAndIdempotent(t *testing.T) {
	stored := []map[string]any{{"Name": "@", "Type": "NS", "Value": "f1g1ns1.dnspod.net.", "Status": "ENABLE"}}
	writes := 0
	c := fakeClient(DNSPod, func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://dnspod.tencentcloudapi.com/" || req.Method != "POST" {
			t.Fatal("unsafe endpoint")
		}
		payload, _ := io.ReadAll(req.Body)
		action := req.Header.Get("X-TC-Action")
		sha := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
		hmac256 := func(k []byte, v string) []byte {
			h := hmac.New(sha256.New, k)
			_, _ = h.Write([]byte(v))
			return h.Sum(nil)
		}
		canonical := "POST\n/\n\ncontent-type:" + req.Header.Get("Content-Type") + "\nhost:dnspod.tencentcloudapi.com\nx-tc-action:" + strings.ToLower(action) + "\n\ncontent-type;host;x-tc-action\n" + sha(payload)
		date := time.Unix(1790812800, 0).UTC().Format("2006-01-02")
		scope := date + "/dnspod/tc3_request"
		key := hmac256(hmac256(hmac256([]byte("TC3test-secret"), date), "dnspod"), "tc3_request")
		signature := hex.EncodeToString(hmac256(key, "TC3-HMAC-SHA256\n"+req.Header.Get("X-TC-Timestamp")+"\n"+scope+"\n"+sha([]byte(canonical))))
		expected := "TC3-HMAC-SHA256 Credential=test-id/" + scope + ", SignedHeaders=content-type;host;x-tc-action, Signature=" + signature
		if req.Header.Get("Authorization") != expected || req.Header.Get("X-TC-Version") != "2021-03-23" {
			t.Fatal("bad Tencent signature")
		}
		var params map[string]any
		if json.Unmarshal(payload, &params) != nil || params["Domain"] != "example.com" {
			t.Fatal("bad params")
		}
		if action == "DescribeRecordList" {
			return jsonResponse(map[string]any{"Response": map[string]any{"RequestId": "request", "RecordCountInfo": map[string]any{"TotalCount": len(stored)}, "RecordList": stored}}), nil
		}
		if action != "CreateRecord" || params["RecordLine"] != "默认" {
			t.Fatal("unexpected mutation")
		}
		stored = append(stored, map[string]any{"Name": params["SubDomain"], "Type": params["RecordType"], "Value": params["Value"], "Status": "ENABLE"})
		writes++
		return jsonResponse(map[string]any{"Response": map[string]any{"RequestId": "request", "RecordId": writes}}), nil
	})
	for i := 0; i < 2; i++ {
		if err := c.EnsureRecords(context.Background(), "example.com", DNSPod, credentials(DNSPod), desiredRecords()); err != nil {
			t.Fatal(err)
		}
	}
	if writes != 4 {
		t.Fatal(writes)
	}
}

func TestPreflightDelegationDisabledAndUnrelatedRecords(t *testing.T) {
	for _, have := range []record{{contract.DNSRecord{"NS", "www.example.com", "ns1.other.com"}, false}, {contract.DNSRecord{"A", "www.example.com", "8.8.4.4"}, true}, {contract.DNSRecord{"TXT", "_opengpt-verification.example.com", "root-proof"}, true}} {
		if _, err := preflight("example.com", []record{have}, desiredRecords()); !errors.Is(err, contract.ErrDNSRecordConflict) {
			t.Fatal(err)
		}
	}
	have := []record{{contract.DNSRecord{"NS", "example.com", "alice.ns.cloudflare.com"}, false}, {contract.DNSRecord{"TXT", "_opengpt-verification.example.com", "\"root-proof\""}, false}, {contract.DNSRecord{"MX", "example.com", "mail.example.com"}, false}}
	missing, err := preflight("example.com", have, desiredRecords())
	if err != nil || len(missing) != 3 {
		t.Fatal(err, missing)
	}
}

func TestAuthorizeExactZoneReadOnly(t *testing.T) {
	for _, provider := range []string{Cloudflare, Aliyun, DNSPod} {
		t.Run(provider, func(t *testing.T) {
			calls := 0
			c := fakeClient(provider, func(req *http.Request) (*http.Response, error) {
				calls++
				switch provider {
				case Cloudflare:
					if req.Method != "GET" {
						t.Fatal("authorization mutated DNS")
					}
					if strings.HasSuffix(req.URL.Path, "/zones") {
						return jsonResponse(map[string]any{"success": true, "result": []any{map[string]any{"id": "zone", "name": "example.com", "status": "active"}}}), nil
					}
					return jsonResponse(map[string]any{"success": true, "result": []any{}}), nil
				case Aliyun:
					if err := req.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if req.Form.Get("Action") != "DescribeDomainRecords" || req.Form.Get("DomainName") != "example.com" {
						t.Fatal("authorization mutated DNS or wrong zone")
					}
					return jsonResponse(map[string]any{"RequestId": "request", "TotalCount": 0, "DomainRecords": map[string]any{"Record": []any{}}}), nil
				default:
					if req.Header.Get("X-TC-Action") != "DescribeRecordList" {
						t.Fatal("authorization mutated DNS")
					}
					var params map[string]any
					if json.NewDecoder(req.Body).Decode(&params) != nil || params["Domain"] != "example.com" {
						t.Fatal("wrong zone")
					}
					return jsonResponse(map[string]any{"Response": map[string]any{"RequestId": "request", "RecordCountInfo": map[string]any{"TotalCount": 0}, "RecordList": []any{}}}), nil
				}
			})
			if err := c.Authorize(context.Background(), "example.com", provider, credentials(provider)); err != nil || calls == 0 {
				t.Fatal(err, calls)
			}
			calls = 0
			if err := c.Authorize(context.Background(), "example.com", provider, contract.DNSCredentials{}); !errors.Is(err, contract.ErrDNSCredentialsInvalid) || calls != 0 {
				t.Fatal(err, calls)
			}
		})
	}
	c := fakeClient(Aliyun, func(req *http.Request) (*http.Response, error) {
		return jsonResponse(map[string]any{"RequestId": "request", "Code": "Forbidden", "Message": "test-secret"}), nil
	})
	if err := c.Authorize(context.Background(), "example.com", Aliyun, credentials(Aliyun)); !errors.Is(err, contract.ErrDNSProviderRequest) || strings.Contains(err.Error(), "test-secret") {
		t.Fatal(err)
	}
	c = fakeClient(DNSPod, func(req *http.Request) (*http.Response, error) {
		return jsonResponse(map[string]any{"Response": map[string]any{"RequestId": "request", "Error": map[string]any{"Code": "UnauthorizedOperation", "Message": "test-secret"}}}), nil
	})
	if err := c.Authorize(context.Background(), "example.com", DNSPod, credentials(DNSPod)); !errors.Is(err, contract.ErrDNSProviderRequest) || strings.Contains(err.Error(), "test-secret") {
		t.Fatal(err)
	}
}

func TestSaaSCNAMEPreflightProtectsExistingRecords(t *testing.T) {
	wanted, err := validateDesired("example.com", []contract.DNSRecord{{Type: "CNAME", Name: "example.com", Value: "entry.shop.example.com"}, {Type: "TXT", Name: "_opengpt-verification.example.com", Value: "proof"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"A", "AAAA", "MX", "CNAME"} {
		if _, err := preflight("example.com", []record{{DNSRecord: contract.DNSRecord{Type: typ, Name: "example.com", Value: "old.example.net"}}}, wanted); !errors.Is(err, contract.ErrDNSRecordConflict) {
			t.Fatalf("overwrote %s: %v", typ, err)
		}
	}
	existing := []record{{DNSRecord: contract.DNSRecord{Type: "CNAME", Name: "example.com", Value: "entry.shop.example.com."}}}
	missing, err := preflight("example.com", existing, wanted)
	if err != nil || len(missing) != 1 || missing[0].Type != "TXT" {
		t.Fatalf("non-idempotent %v %v", missing, err)
	}
}
