// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

// Package dnsprovider adds only absent records after inspecting every requested
// name. It never updates/deletes DNS records or stores provider credentials.
package dnsprovider

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	contract "github.com/dujiao-next/internal/modules/reseller/contract"
)

const (
	Cloudflare = "cloudflare"
	Aliyun     = "aliyun"
	DNSPod     = "dnspod"
)

type nsResolver interface {
	LookupNS(context.Context, string) ([]*net.NS, error)
}

type Client struct {
	http     *http.Client
	resolver nsResolver
	now      func() time.Time
	locks    [64]sync.Mutex
}

var _ contract.DNSProvider = (*Client)(nil)

func New() *Client {
	return &Client{
		http:     &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		resolver: net.DefaultResolver, now: time.Now,
	}
}

func (c *Client) Detect(ctx context.Context, root string) (contract.DNSInspection, error) {
	result := contract.DNSInspection{NameServers: []string{}, RequiredPermissions: []string{}}
	root = normalizeName(root)
	if !validDomain(root) {
		return result, contract.ErrDomainInvalid
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	servers, err := c.resolver.LookupNS(lookupCtx, root)
	if err != nil || len(servers) == 0 {
		return result, contract.ErrDNSLookupFailed
	}
	provider := ""
	for i, ns := range servers {
		if ns == nil {
			return result, contract.ErrDNSLookupFailed
		}
		name := normalizeName(ns.Host)
		result.NameServers = append(result.NameServers, name)
		found := detectNameServer(name)
		if i == 0 {
			provider = found
		} else if provider != found {
			provider = "mixed"
		}
	}
	sort.Strings(result.NameServers)
	switch provider {
	case Cloudflare:
		result.Provider, result.ProviderLabel, result.Supported = provider, "Cloudflare", true
		result.RequiredPermissions = []string{"仅限此域名的 API Token：Zone Read、DNS Write"}
	case Aliyun:
		result.Provider, result.ProviderLabel, result.Supported = provider, "阿里云 DNS", true
		result.RequiredPermissions = []string{"仅授权此域名的 RAM 子账户：alidns:DescribeDomainRecords、alidns:AddDomainRecord"}
	case DNSPod:
		result.Provider, result.ProviderLabel, result.Supported = provider, "腾讯 DNSPod", true
		result.RequiredPermissions = []string{"仅授权此域名的 CAM 子账户：dnspod:DescribeRecordList、dnspod:CreateRecord"}
	default:
		result.Provider, result.ProviderLabel = "unsupported", "其他 DNS 平台"
	}
	return result, nil
}

func detectNameServer(name string) string {
	switch {
	case strings.HasSuffix(name, ".ns.cloudflare.com"):
		return Cloudflare
	case strings.HasSuffix(name, ".hichina.com"), strings.HasSuffix(name, ".alidns.com"):
		return Aliyun
	case strings.HasSuffix(name, ".dnspod.net"), strings.HasSuffix(name, ".dnspod.com"), strings.HasSuffix(name, ".dnspod.cn"):
		return DNSPod
	default:
		return ""
	}
}

// Inspection contains records read from an authenticated, exact zone. It is not
// a persisted object and must not be used as proof of domain ownership alone.
type Inspection struct {
	contract.DNSInspection
	ZoneID  string               `json:"zone_id"`
	Records []contract.DNSRecord `json:"records"`
}

type record struct {
	contract.DNSRecord
	blocked bool
}
type zone struct {
	id      string
	records []record
}

func (c *Client) Inspect(ctx context.Context, root string, creds contract.DNSCredentials) (Inspection, error) {
	root = normalizeName(root)
	detected, err := c.Detect(ctx, root)
	if err != nil {
		return Inspection{}, err
	}
	if !detected.Supported {
		return Inspection{DNSInspection: detected}, contract.ErrDNSProviderUnsupported
	}
	if !validCredentials(detected.Provider, creds) {
		return Inspection{}, contract.ErrDNSCredentialsInvalid
	}
	z, err := c.inspect(ctx, root, detected.Provider, creds)
	if err != nil {
		return Inspection{}, err
	}
	result := Inspection{DNSInspection: detected, ZoneID: z.id, Records: []contract.DNSRecord{}}
	for _, r := range z.records {
		result.Records = append(result.Records, r.DNSRecord)
	}
	return result, nil
}

// Authorize proves that credentials can read this exact authoritative zone before
// the application reserves its hostnames. It performs no DNS mutations.
func (c *Client) Authorize(ctx context.Context, root, provider string, creds contract.DNSCredentials) error {
	root = normalizeName(root)
	detected, err := c.Detect(ctx, root)
	if err != nil {
		return err
	}
	if !detected.Supported {
		return contract.ErrDNSProviderUnsupported
	}
	if provider != detected.Provider {
		return contract.ErrDNSProviderMismatch
	}
	if !validCredentials(provider, creds) {
		return contract.ErrDNSCredentialsInvalid
	}
	_, err = c.inspect(ctx, root, provider, creds)
	return err
}

func (c *Client) EnsureRecords(ctx context.Context, root, provider string, creds contract.DNSCredentials, desired []contract.DNSRecord) error {
	root = normalizeName(root)
	if !validDomain(root) {
		return contract.ErrDomainInvalid
	}
	wanted, err := validateDesired(root, desired)
	if err != nil {
		return err
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(root))
	lock := &c.locks[h.Sum32()%uint32(len(c.locks))]
	lock.Lock()
	defer lock.Unlock()
	detected, err := c.Detect(ctx, root)
	if err != nil {
		return err
	}
	if !detected.Supported {
		return contract.ErrDNSProviderUnsupported
	}
	if provider != detected.Provider {
		return contract.ErrDNSProviderMismatch
	}
	if !validCredentials(provider, creds) {
		return contract.ErrDNSCredentialsInvalid
	}
	z, err := c.inspect(ctx, root, provider, creds)
	if err != nil {
		return err
	}
	missing, err := preflight(root, z.records, wanted)
	if err != nil {
		return err
	}
	// All root/www/challenge names have been preflighted before the first write.
	for _, r := range missing {
		if err := c.create(ctx, root, provider, z.id, r, creds); err != nil {
			return err
		}
	}
	return nil
}

func validateDesired(root string, desired []contract.DNSRecord) ([]contract.DNSRecord, error) {
	if len(desired) == 0 || len(desired) > 4 {
		return nil, contract.ErrDNSRecordInvalid
	}
	result := make([]contract.DNSRecord, 0, len(desired))
	seen := map[string]string{}
	for _, r := range desired {
		r.Type = strings.ToUpper(strings.TrimSpace(r.Type))
		r.Name = normalizeName(r.Name)
		switch r.Type {
		case "A":
			ip := net.ParseIP(r.Value)
			if (r.Name != root && r.Name != "www."+root) || ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
				return nil, contract.ErrDNSRecordInvalid
			}
			r.Value = ip.String()
		case "CNAME":
			r.Value = normalizeName(r.Value)
			if (r.Name != root && r.Name != "www."+root) || !validDomain(r.Value) || r.Value == r.Name {
				return nil, contract.ErrDNSRecordInvalid
			}
		case "TXT":
			if (r.Name != "_opengpt-verification."+root && r.Name != "_opengpt-verification.www."+root) || r.Value == "" || len(r.Value) > 255 || strings.ContainsAny(r.Value, "\r\n\x00\"\\") {
				return nil, contract.ErrDNSRecordInvalid
			}
		default:
			return nil, contract.ErrDNSRecordInvalid
		}
		key := r.Type + "/" + r.Name
		if value, ok := seen[key]; ok {
			if value != r.Value {
				return nil, contract.ErrDNSRecordInvalid
			}
			continue
		}
		seen[key] = r.Value
		result = append(result, r)
	}
	return result, nil
}

func preflight(root string, existing []record, desired []contract.DNSRecord) ([]contract.DNSRecord, error) {
	missing := []contract.DNSRecord{}
	for _, want := range desired {
		found := false
		for _, have := range existing {
			if normalizeName(have.Name) != want.Name {
				continue
			}
			typ := strings.ToUpper(have.Type)
			relevant := want.Type == "CNAME" || typ == "CNAME" || (typ == "NS" && want.Name != root) || typ == "DNAME" || typ == "显性URL" || typ == "隐性URL" || typ == want.Type || (want.Type == "A" && typ == "AAAA")
			if !relevant {
				continue
			}
			value := have.Value
			if typ == "TXT" {
				if unquoted, err := strconv.Unquote(value); err == nil {
					value = unquoted
				}
			}
			if typ == "CNAME" {
				value = normalizeName(value)
			}
			if have.blocked || typ != want.Type || value != want.Value {
				return nil, contract.ErrDNSRecordConflict
			}
			found = true
		}
		if !found {
			missing = append(missing, want)
		}
	}
	return missing, nil
}

func validCredentials(provider string, credentials contract.DNSCredentials) bool {
	keys := []string{}
	switch provider {
	case Cloudflare:
		keys = []string{"api_token"}
	case Aliyun:
		keys = []string{"access_key_id", "access_key_secret"}
	case DNSPod:
		keys = []string{"secret_id", "secret_key"}
	default:
		return false
	}
	for _, key := range keys {
		value := credentials[key]
		if value == "" || len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") || strings.TrimSpace(value) != value {
			return false
		}
	}
	return true
}

func normalizeName(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}
func validDomain(s string) bool {
	if len(s) > 253 || net.ParseIP(s) != nil {
		return false
	}
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}

// exchange deliberately drops transport errors and response messages: either may
// contain a request URL, Authorization header, credential ID or reflected secret.
func (c *Client) exchange(req *http.Request, out any) error {
	if req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Port() != "" {
		return contract.ErrDNSProviderRequest
	}
	switch req.URL.Host {
	case "api.cloudflare.com", "alidns.aliyuncs.com", "dnspod.tencentcloudapi.com":
	default:
		return contract.ErrDNSProviderRequest
	}
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return contract.ErrDNSProviderRequest
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return contract.ErrDNSProviderRequest
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if err != nil || len(body) > 4*1024*1024 || json.Unmarshal(body, out) != nil {
		return contract.ErrDNSProviderRequest
	}
	return nil
}

func relevantName(root, name string) bool {
	name = normalizeName(name)
	return name == root || name == "www."+root || name == "_opengpt-verification."+root || name == "_opengpt-verification.www."+root
}
func fullName(root, rr string) string {
	if rr == "@" || rr == "" {
		return root
	}
	return normalizeName(rr) + "." + root
}
func relativeName(root, name string) string {
	if name == root {
		return "@"
	}
	return strings.TrimSuffix(name, "."+root)
}
