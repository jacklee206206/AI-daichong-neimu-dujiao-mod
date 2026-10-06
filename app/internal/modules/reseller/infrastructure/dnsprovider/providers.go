// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package dnsprovider

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	contract "github.com/dujiao-next/internal/modules/reseller/contract"
)

func (c *Client) inspect(ctx context.Context, root, provider string, creds contract.DNSCredentials) (zone, error) {
	switch provider {
	case Cloudflare:
		return c.inspectCloudflare(ctx, root, creds)
	case Aliyun:
		return c.inspectAliyun(ctx, root, creds)
	case DNSPod:
		return c.inspectDNSPod(ctx, root, creds)
	default:
		return zone{}, contract.ErrDNSProviderUnsupported
	}
}
func (c *Client) create(ctx context.Context, root, provider, zoneID string, r contract.DNSRecord, creds contract.DNSCredentials) error {
	switch provider {
	case Cloudflare:
		body := map[string]any{"type": r.Type, "name": r.Name, "content": r.Value, "ttl": 600}
		if r.Type == "A" || r.Type == "CNAME" {
			body["proxied"] = false
		}
		var out struct {
			ID string `json:"id"`
		}
		if err := c.cloudflare(ctx, http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records", nil, body, creds, &out); err != nil {
			return err
		}
		if out.ID == "" {
			return contract.ErrDNSProviderRequest
		}
		return nil
	case Aliyun:
		var out struct {
			RecordID string `json:"RecordId"`
		}
		err := c.aliyun(ctx, "AddDomainRecord", url.Values{"DomainName": {root}, "RR": {relativeName(root, r.Name)}, "Type": {r.Type}, "Value": {r.Value}, "TTL": {"600"}, "Line": {"default"}}, creds, &out)
		if err != nil {
			return err
		}
		if out.RecordID == "" {
			return contract.ErrDNSProviderRequest
		}
		return nil
	case DNSPod:
		var out struct {
			RecordID uint64 `json:"RecordId"`
		}
		err := c.dnspod(ctx, "CreateRecord", map[string]any{"Domain": root, "SubDomain": relativeName(root, r.Name), "RecordType": r.Type, "RecordLine": "默认", "Value": r.Value, "TTL": 600}, creds, &out)
		if err != nil {
			return err
		}
		if out.RecordID == 0 {
			return contract.ErrDNSProviderRequest
		}
		return nil
	default:
		return contract.ErrDNSProviderUnsupported
	}
}

// Cloudflare REST API: exact-name zone lookup avoids modifying a parent/sibling.
func (c *Client) inspectCloudflare(ctx context.Context, root string, creds contract.DNSCredentials) (zone, error) {
	var zones []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := c.cloudflare(ctx, http.MethodGet, "/zones", url.Values{"name": {root}, "per_page": {"50"}}, nil, creds, &zones); err != nil {
		return zone{}, err
	}
	if len(zones) != 1 || normalizeName(zones[0].Name) != root || zones[0].ID == "" || zones[0].Status != "active" {
		return zone{}, contract.ErrDNSProviderRequest
	}
	z := zone{id: zones[0].ID}
	for page := 1; page <= 100; page++ {
		var records []struct {
			Type    string `json:"type"`
			Name    string `json:"name"`
			Content string `json:"content"`
			Proxied bool   `json:"proxied"`
		}
		if err := c.cloudflare(ctx, http.MethodGet, "/zones/"+url.PathEscape(z.id)+"/dns_records", url.Values{"per_page": {"100"}, "page": {strconv.Itoa(page)}}, nil, creds, &records); err != nil {
			return zone{}, err
		}
		for _, r := range records {
			if relevantName(root, r.Name) {
				z.records = append(z.records, record{contract.DNSRecord{Type: r.Type, Name: r.Name, Value: r.Content}, r.Proxied})
			}
		}
		if len(records) < 100 {
			return z, nil
		}
	}
	return zone{}, contract.ErrDNSProviderRequest
}
func (c *Client) cloudflare(ctx context.Context, method, path string, query url.Values, body any, creds contract.DNSCredentials, out any) error {
	endpoint := "https://api.cloudflare.com/client/v4" + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return contract.ErrDNSProviderRequest
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return contract.ErrDNSProviderRequest
	}
	req.Header.Set("Authorization", "Bearer "+creds["api_token"])
	req.Header.Set("Content-Type", "application/json")
	var envelope struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
	}
	if err := c.exchange(req, &envelope); err != nil {
		return err
	}
	if !envelope.Success || len(envelope.Result) == 0 || json.Unmarshal(envelope.Result, out) != nil {
		return contract.ErrDNSProviderRequest
	}
	return nil
}

// Alibaba Cloud DNS RPC 2015-01-09 uses HMAC-SHA1. Parameters are POSTed rather
// than put into the URL, keeping credentials/signatures out of request URLs.
func (c *Client) inspectAliyun(ctx context.Context, root string, creds contract.DNSCredentials) (zone, error) {
	z := zone{id: root}
	count := 0
	for page := 1; page <= 100; page++ {
		var out struct {
			TotalCount    int `json:"TotalCount"`
			DomainRecords struct {
				Record []struct{ DomainName, RR, Type, Value, Status string } `json:"Record"`
			} `json:"DomainRecords"`
		}
		err := c.aliyun(ctx, "DescribeDomainRecords", url.Values{"DomainName": {root}, "PageSize": {"100"}, "PageNumber": {strconv.Itoa(page)}}, creds, &out)
		if err != nil {
			return zone{}, err
		}
		records := out.DomainRecords.Record
		count += len(records)
		for _, r := range records {
			if normalizeName(r.DomainName) != root {
				return zone{}, contract.ErrDNSProviderRequest
			}
			name := fullName(root, r.RR)
			if relevantName(root, name) {
				z.records = append(z.records, record{contract.DNSRecord{Type: r.Type, Name: name, Value: r.Value}, !strings.EqualFold(r.Status, "enable")})
			}
		}
		if count >= out.TotalCount {
			return z, nil
		}
		if len(records) == 0 {
			return zone{}, contract.ErrDNSProviderRequest
		}
	}
	return zone{}, contract.ErrDNSProviderRequest
}
func (c *Client) aliyun(ctx context.Context, action string, params url.Values, creds contract.DNSCredentials, out any) error {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return contract.ErrDNSProviderRequest
	}
	params.Set("Action", action)
	params.Set("Version", "2015-01-09")
	params.Set("Format", "JSON")
	params.Set("AccessKeyId", creds["access_key_id"])
	params.Set("SignatureMethod", "HMAC-SHA1")
	params.Set("SignatureVersion", "1.0")
	params.Set("SignatureNonce", hex.EncodeToString(nonce))
	params.Set("Timestamp", c.now().UTC().Format("2006-01-02T15:04:05Z"))
	canonical := strings.ReplaceAll(params.Encode(), "+", "%20")
	sign := hmac.New(sha1.New, []byte(creds["access_key_secret"]+"&"))
	_, _ = sign.Write([]byte("POST&%2F&" + escapeRFC3986(canonical)))
	params.Set("Signature", base64.StdEncoding.EncodeToString(sign.Sum(nil)))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://alidns.aliyuncs.com/", strings.NewReader(params.Encode()))
	if err != nil {
		return contract.ErrDNSProviderRequest
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var raw json.RawMessage
	if err := c.exchange(req, &raw); err != nil {
		return err
	}
	var envelope struct {
		Code      string `json:"Code"`
		RequestID string `json:"RequestId"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Code != "" || envelope.RequestID == "" || json.Unmarshal(raw, out) != nil {
		return contract.ErrDNSProviderRequest
	}
	return nil
}
func escapeRFC3986(v string) string { return strings.ReplaceAll(url.QueryEscape(v), "+", "%20") }

func (c *Client) inspectDNSPod(ctx context.Context, root string, creds contract.DNSCredentials) (zone, error) {
	z := zone{id: root}
	count := 0
	for page := 0; page < 100; page++ {
		var out struct {
			RecordCountInfo struct {
				TotalCount int `json:"TotalCount"`
			} `json:"RecordCountInfo"`
			RecordList []struct {
				Name, Type, Value, Status string
				DefaultNS                 bool `json:"DefaultNS"`
			} `json:"RecordList"`
		}
		err := c.dnspod(ctx, "DescribeRecordList", map[string]any{"Domain": root, "Offset": page * 100, "Limit": 100, "ErrorOnEmpty": "no"}, creds, &out)
		if err != nil {
			return zone{}, err
		}
		count += len(out.RecordList)
		for _, r := range out.RecordList {
			name := fullName(root, r.Name)
			// DNSPod includes its hidden system @ NS records in DescribeRecordList.
			// They are zone authority metadata, not user CNAME conflicts. Never
			// ignore custom/delegated NS, disabled records, or other record types.
			if r.DefaultNS && name == root && strings.EqualFold(r.Type, "NS") && strings.EqualFold(r.Status, "enable") {
				continue
			}
			if relevantName(root, name) {
				z.records = append(z.records, record{contract.DNSRecord{Type: r.Type, Name: name, Value: r.Value}, !strings.EqualFold(r.Status, "enable")})
			}
		}
		if count >= out.RecordCountInfo.TotalCount {
			return z, nil
		}
		if len(out.RecordList) == 0 {
			return zone{}, contract.ErrDNSProviderRequest
		}
	}
	return zone{}, contract.ErrDNSProviderRequest
}
func (c *Client) dnspod(ctx context.Context, action string, params map[string]any, creds contract.DNSCredentials, out any) error {
	payload, err := json.Marshal(params)
	if err != nil {
		return contract.ErrDNSProviderRequest
	}
	const host = "dnspod.tencentcloudapi.com"
	const contentType = "application/json; charset=utf-8"
	now := c.now().UTC()
	timestamp := strconv.FormatInt(now.Unix(), 10)
	date := now.Format("2006-01-02")
	canonicalHeaders := "content-type:" + contentType + "\nhost:" + host + "\nx-tc-action:" + strings.ToLower(action) + "\n"
	signedHeaders := "content-type;host;x-tc-action"
	canonicalRequest := "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + sha256Hex(payload)
	scope := date + "/dnspod/tc3_request"
	stringToSign := "TC3-HMAC-SHA256\n" + timestamp + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))
	dateKey := hmacSHA256([]byte("TC3"+creds["secret_key"]), date)
	serviceKey := hmacSHA256(dateKey, "dnspod")
	signingKey := hmacSHA256(serviceKey, "tc3_request")
	authorization := "TC3-HMAC-SHA256 Credential=" + creds["secret_id"] + "/" + scope + ", SignedHeaders=" + signedHeaders + ", Signature=" + hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+host+"/", bytes.NewReader(payload))
	if err != nil {
		return contract.ErrDNSProviderRequest
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", authorization)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", "2021-03-23")
	req.Header.Set("X-TC-Timestamp", timestamp)
	var envelope struct {
		Response json.RawMessage `json:"Response"`
	}
	if err := c.exchange(req, &envelope); err != nil {
		return err
	}
	var status struct {
		Error *struct {
			Code string `json:"Code"`
		} `json:"Error"`
		RequestID string `json:"RequestId"`
	}
	if json.Unmarshal(envelope.Response, &status) != nil || status.RequestID == "" {
		return contract.ErrDNSProviderRequest
	}
	if status.Error != nil {
		// Empty record sets are not a missing domain or an authorization success.
		if action == "DescribeRecordList" && status.Error.Code == "ResourceNotFound.NoDataOfRecord" {
			return nil
		}
		return contract.ErrDNSProviderRequest
	}
	if json.Unmarshal(envelope.Response, out) != nil {
		return contract.ErrDNSProviderRequest
	}
	return nil
}
func sha256Hex(data []byte) string { v := sha256.Sum256(data); return hex.EncodeToString(v[:]) }
func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(data))
	return h.Sum(nil)
}
