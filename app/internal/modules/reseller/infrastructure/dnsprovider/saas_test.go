// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package dnsprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/dujiao-next/internal/modules/reseller/contract"
)

type saasFixtureRecord struct {
	contract.DNSRecord
	disabled  bool
	proxied   bool
	defaultNS bool
}

func saasDesiredRecords() []contract.DNSRecord {
	return []contract.DNSRecord{
		{Type: "CNAME", Name: "example.com", Value: "entry.shop.example.com"},
		{Type: "TXT", Name: "_opengpt-verification.example.com", Value: "root-proof"},
		{Type: "CNAME", Name: "www.example.com", Value: "entry.shop.example.com"},
		{Type: "TXT", Name: "_opengpt-verification.www.example.com", Value: "www-proof"},
	}
}

func saasFixtureClient(t *testing.T, provider string, initial []saasFixtureRecord) (*Client, *int) {
	t.Helper()
	stored := append([]saasFixtureRecord{}, initial...)
	writes := 0
	c := fakeClient(provider, func(req *http.Request) (*http.Response, error) {
		var created contract.DNSRecord
		switch provider {
		case Cloudflare:
			if strings.HasSuffix(req.URL.Path, "/zones") {
				return jsonResponse(map[string]any{"success": true, "result": []any{map[string]any{"id": "zone", "name": "example.com", "status": "active"}}}), nil
			}
			if req.Method == http.MethodGet {
				records := []any{}
				for _, r := range stored {
					records = append(records, map[string]any{"type": r.Type, "name": r.Name, "content": r.Value, "proxied": r.proxied})
				}
				return jsonResponse(map[string]any{"success": true, "result": records}), nil
			}
			if req.Method != http.MethodPost {
				t.Fatal("CNAME path must never update or delete")
			}
			var body map[string]any
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["ttl"] != float64(600) {
				t.Fatal("unexpected TTL")
			}
			created = contract.DNSRecord{Type: body["type"].(string), Name: body["name"].(string), Value: body["content"].(string)}
			if created.Type == "CNAME" && body["proxied"] != false {
				t.Fatal("SaaS CNAME must start DNS-only")
			}
		case Aliyun:
			if err := req.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if req.Form.Get("DomainName") != "example.com" {
				t.Fatal("wrong zone")
			}
			if req.Form.Get("Action") == "DescribeDomainRecords" {
				records := []any{}
				for _, r := range stored {
					status := "Enable"
					if r.disabled {
						status = "Disable"
					}
					records = append(records, map[string]any{"DomainName": "example.com", "RR": relativeName("example.com", r.Name), "Type": r.Type, "Value": r.Value, "Status": status})
				}
				return jsonResponse(map[string]any{"RequestId": "request", "TotalCount": len(records), "DomainRecords": map[string]any{"Record": records}}), nil
			}
			if req.Form.Get("Action") != "AddDomainRecord" || req.Form.Get("Line") != "default" || req.Form.Get("TTL") != "600" {
				t.Fatal("unexpected DNS mutation")
			}
			created = contract.DNSRecord{Type: req.Form.Get("Type"), Name: fullName("example.com", req.Form.Get("RR")), Value: req.Form.Get("Value")}
		case DNSPod:
			var body map[string]any
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["Domain"] != "example.com" {
				t.Fatal("wrong zone")
			}
			if req.Header.Get("X-TC-Action") == "DescribeRecordList" {
				records := []any{}
				for _, r := range stored {
					status := "ENABLE"
					if r.disabled {
						status = "DISABLE"
					}
					records = append(records, map[string]any{"Name": relativeName("example.com", r.Name), "Type": r.Type, "Value": r.Value, "Status": status, "DefaultNS": r.defaultNS})
				}
				return jsonResponse(map[string]any{"Response": map[string]any{"RequestId": "request", "RecordCountInfo": map[string]any{"TotalCount": len(records)}, "RecordList": records}}), nil
			}
			if req.Header.Get("X-TC-Action") != "CreateRecord" || body["RecordLine"] != "默认" || body["TTL"] != float64(600) {
				t.Fatal("unexpected DNS mutation")
			}
			created = contract.DNSRecord{Type: body["RecordType"].(string), Name: fullName("example.com", body["SubDomain"].(string)), Value: body["Value"].(string)}
		}
		found := false
		for _, want := range saasDesiredRecords() {
			if created == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("unexpected SaaS record: %+v", created)
		}
		stored = append(stored, saasFixtureRecord{DNSRecord: created})
		writes++
		switch provider {
		case Cloudflare:
			return jsonResponse(map[string]any{"success": true, "result": map[string]any{"id": "record"}}), nil
		case Aliyun:
			return jsonResponse(map[string]any{"RequestId": "request", "RecordId": "record"}), nil
		default:
			return jsonResponse(map[string]any{"Response": map[string]any{"RequestId": "request", "RecordId": writes}}), nil
		}
	})
	return c, &writes
}

func TestSaaSAllProvidersCreateRootWWWAndUniqueTXTIdempotently(t *testing.T) {
	for _, provider := range []string{Cloudflare, Aliyun, DNSPod} {
		t.Run(provider, func(t *testing.T) {
			initial := []saasFixtureRecord{}
			if provider == DNSPod {
				initial = append(initial, saasFixtureRecord{DNSRecord: contract.DNSRecord{Type: "NS", Name: "example.com", Value: "f1g1ns1.dnspod.net."}, defaultNS: true}, saasFixtureRecord{DNSRecord: contract.DNSRecord{Type: "NS", Name: "example.com", Value: "f1g1ns2.dnspod.net."}, defaultNS: true})
			}
			c, writes := saasFixtureClient(t, provider, initial)
			for i := 0; i < 2; i++ {
				if err := c.EnsureRecords(context.Background(), "example.com", provider, credentials(provider), saasDesiredRecords()); err != nil {
					t.Fatal(err)
				}
			}
			if *writes != 4 {
				t.Fatalf("root/www CNAME and unique TXT must be created once each, writes=%d", *writes)
			}
		})
	}
}

func TestSaaSAllProvidersPreflightWholePairBeforeAnyWrite(t *testing.T) {
	cases := []saasFixtureRecord{
		{DNSRecord: contract.DNSRecord{Type: "A", Name: "example.com", Value: "8.8.8.8"}},
		{DNSRecord: contract.DNSRecord{Type: "AAAA", Name: "example.com", Value: "2001:4860:4860::8888"}},
		{DNSRecord: contract.DNSRecord{Type: "MX", Name: "example.com", Value: "mail.example.com"}},
		{DNSRecord: contract.DNSRecord{Type: "TXT", Name: "example.com", Value: "v=spf1 ~all"}},
		{DNSRecord: contract.DNSRecord{Type: "NS", Name: "example.com", Value: "ns.other.com"}},
		{DNSRecord: contract.DNSRecord{Type: "NS", Name: "www.example.com", Value: "ns.other.com"}, defaultNS: true},
		{DNSRecord: contract.DNSRecord{Type: "CNAME", Name: "www.example.com", Value: "old.example.net"}},
		{DNSRecord: contract.DNSRecord{Type: "TXT", Name: "_opengpt-verification.www.example.com", Value: "other-proof"}},
	}
	for _, provider := range []string{Cloudflare, Aliyun, DNSPod} {
		for _, existing := range cases {
			t.Run(provider+"/"+existing.Type+"/"+existing.Name, func(t *testing.T) {
				c, writes := saasFixtureClient(t, provider, []saasFixtureRecord{existing})
				if err := c.EnsureRecords(context.Background(), "example.com", provider, credentials(provider), saasDesiredRecords()); !errors.Is(err, contract.ErrDNSRecordConflict) || *writes != 0 {
					t.Fatalf("conflict should stop all writes: err=%v writes=%d", err, *writes)
				}
			})
		}
	}
}

func TestSaaSCloudflareProxyAndDNSPodNonSystemNSRemainConflicts(t *testing.T) {
	for _, tc := range []struct {
		name, provider string
		row            saasFixtureRecord
	}{
		{"proxied_cname", Cloudflare, saasFixtureRecord{DNSRecord: saasDesiredRecords()[0], proxied: true}},
		{"custom_apex_ns", DNSPod, saasFixtureRecord{DNSRecord: contract.DNSRecord{Type: "NS", Name: "example.com", Value: "f1g1ns1.dnspod.net."}}},
		{"disabled_default_ns", DNSPod, saasFixtureRecord{DNSRecord: contract.DNSRecord{Type: "NS", Name: "example.com", Value: "f1g1ns1.dnspod.net."}, defaultNS: true, disabled: true}},
		{"disabled_cname", DNSPod, saasFixtureRecord{DNSRecord: saasDesiredRecords()[0], disabled: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, writes := saasFixtureClient(t, tc.provider, []saasFixtureRecord{tc.row})
			if err := c.EnsureRecords(context.Background(), "example.com", tc.provider, credentials(tc.provider), saasDesiredRecords()); !errors.Is(err, contract.ErrDNSRecordConflict) || *writes != 0 {
				t.Fatalf("unsafe exception: err=%v writes=%d", err, *writes)
			}
		})
	}
}
