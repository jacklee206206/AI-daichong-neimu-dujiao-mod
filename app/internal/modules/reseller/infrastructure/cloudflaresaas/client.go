// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

// Package cloudflaresaas uses the platform's zone-scoped token. Customer DNS
// credentials never reach this client. All returned errors are safe to persist.
package cloudflaresaas

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/modules/reseller/contract"
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)
var hostnameIDPattern = regexp.MustCompile(`^(?:[a-fA-F0-9]{32}|[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12})$`)
var hostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)

type Client struct {
	cfg  config.CloudflareSaaSConfig
	http *http.Client
}

func New(cfg config.CloudflareSaaSConfig) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) token() (string, error) {
	if c == nil || !idPattern.MatchString(c.cfg.ZoneID) || !validHost(c.cfg.FallbackOrigin) || c.cfg.APITokenFile == "" {
		return "", contract.ErrSaaSUnavailable
	}
	f, err := os.Open(c.cfg.APITokenFile)
	if err != nil {
		return "", contract.ErrSaaSUnavailable
	}
	defer f.Close()
	info, err := f.Stat()
	// A root-owned file with a read-only application group (0640) is supported.
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0027 != 0 || info.Size() > 4096 {
		return "", contract.ErrSaaSUnavailable
	}
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	value := strings.TrimSpace(string(b))
	clear(b)
	if err != nil || len(value) < 20 || len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00 \t") {
		return "", contract.ErrSaaSUnavailable
	}
	return value, nil
}
func (c *Client) Ready() bool { _, err := c.token(); return err == nil }

func validHost(host string) bool {
	if !hostPattern.MatchString(host) || !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}

type hostname struct {
	ID                 string `json:"id"`
	Hostname           string `json:"hostname"`
	Status             string `json:"status"`
	CustomOriginServer string `json:"custom_origin_server"`
	SSL                struct {
		Status            string `json:"status"`
		Method            string `json:"method"`
		Type              string `json:"type"`
		ValidationRecords []struct {
			TXTName  string `json:"txt_name"`
			TXTValue string `json:"txt_value"`
		} `json:"validation_records"`
	} `json:"ssl"`
	OwnershipVerification struct {
		Type  string `json:"type"`
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"ownership_verification"`
}

func (c *Client) CheckPlatform(ctx context.Context) error {
	var fallback struct {
		Origin string `json:"origin"`
		Status string `json:"status"`
	}
	if err := c.exchange(ctx, http.MethodGet, "/custom_hostnames/fallback_origin", nil, &fallback); err != nil {
		return err
	}
	if fallback.Status != "active" || strings.TrimSuffix(strings.ToLower(fallback.Origin), ".") != c.cfg.FallbackOrigin {
		return contract.ErrSaaSUnavailable
	}
	return nil
}

func (c *Client) EnsureHostname(ctx context.Context, host, id string) (*contract.SaaSHostname, error) {
	if !validHost(host) || (id != "" && !hostnameIDPattern.MatchString(id)) {
		return nil, contract.ErrSaaSRequest
	}
	if err := c.CheckPlatform(ctx); err != nil {
		return nil, err
	}
	var result hostname
	if id != "" {
		if err := c.exchange(ctx, http.MethodGet, "/custom_hostnames/"+url.PathEscape(id), nil, &result); err != nil {
			return nil, err
		}
	} else {
		// The list lookup recovers a create whose response was lost, without creating
		// another paid hostname. Never patch or take over incompatible records.
		var rows []hostname
		if err := c.exchange(ctx, http.MethodGet, "/custom_hostnames?hostname="+url.QueryEscape(host)+"&per_page=50", nil, &rows); err != nil {
			return nil, err
		}
		if len(rows) > 1 {
			return nil, contract.ErrSaaSConflict
		}
		if len(rows) == 1 {
			result = rows[0]
		} else {
			body := map[string]any{"hostname": host, "ssl": map[string]any{"method": "http", "type": "dv", "settings": map[string]any{"min_tls_version": "1.2"}}}
			if err := c.exchange(ctx, http.MethodPost, "/custom_hostnames", body, &result); err != nil {
				return nil, err
			}
		}
	}
	if !hostnameIDPattern.MatchString(result.ID) || result.Hostname != host || result.CustomOriginServer != "" || result.SSL.Type != "dv" || result.SSL.Method != "http" {
		return nil, contract.ErrSaaSConflict
	}
	if id != "" && result.ID != id {
		return nil, contract.ErrSaaSConflict
	}
	out := &contract.SaaSHostname{ID: result.ID, Hostname: host, Status: safeStatus(result.Status), SSLStatus: safeStatus(result.SSL.Status), ValidationRecords: []contract.DNSRecord{}}
	addTXT := func(name, value string) {
		name = strings.TrimSuffix(strings.ToLower(name), ".")
		if validHost(strings.ReplaceAll(name, "_", "x")) && (name == host || strings.HasSuffix(name, "."+host)) && len(value) > 0 && len(value) <= 1024 && !strings.ContainsAny(value, "\r\n\x00") {
			out.ValidationRecords = append(out.ValidationRecords, contract.DNSRecord{Type: "TXT", Name: name, Value: value})
		}
	}
	if result.OwnershipVerification.Type == "txt" {
		addTXT(result.OwnershipVerification.Name, result.OwnershipVerification.Value)
	}
	for _, r := range result.SSL.ValidationRecords {
		addTXT(r.TXTName, r.TXTValue)
	}
	return out, nil
}

func safeStatus(value string) string {
	if len(value) > 40 {
		return "unknown"
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c == '_') {
			return "unknown"
		}
	}
	return value
}

func (c *Client) exchange(ctx context.Context, method, path string, body any, out any) error {
	token, err := c.token()
	if err != nil {
		return err
	}
	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return contract.ErrSaaSRequest
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.cloudflare.com/client/v4/zones/"+c.cfg.ZoneID+path, bytes.NewReader(payload))
	if err != nil {
		return contract.ErrSaaSRequest
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return contract.ErrSaaSRequest
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return contract.ErrSaaSRequest
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return contract.ErrSaaSRequest
	}
	var envelope struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
	}
	if json.Unmarshal(data, &envelope) != nil || !envelope.Success || json.Unmarshal(envelope.Result, out) != nil {
		return contract.ErrSaaSRequest
	}
	return nil
}
