// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/modules/reseller/domain"
)

// TXT demonstrates ownership. Public DNS alone is not evidence of correct
// routing: Cloudflare uses shared anycast IPs and flattens apex CNAMEs. Both the
// Cloudflare API state and an exact HTTPS token probe are required afterwards.
func verifySaaSDNS(ctx context.Context, row *domain.Domain, resolver domainDNSResolver) error {
	if err := verifyDomainOwnership(ctx, row, resolver); err != nil {
		return err
	}
	addresses, err := resolver.LookupIPAddr(ctx, row.Domain)
	if err != nil || len(addresses) == 0 {
		return errors.New("尚未查到网站解析，请添加指向统一入口的 CNAME 后等待生效。")
	}
	for _, a := range addresses {
		if !safePublicIP(a.IP) {
			return errors.New("网站解析必须指向公网统一入口，不能使用内网、回环或保留地址。")
		}
	}
	return nil
}
func safePublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	// Deny shared carrier, benchmarking, documentation and reserved ranges too.
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		_, block, _ := net.ParseCIDR(raw)
		if block.Contains(ip) {
			return false
		}
	}
	return true
}

func probeSaaSEdgeHTTPS(ctx context.Context, host, token string) error {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return errors.New("公网 HTTPS 解析尚未生效。")
	}
	for _, a := range addresses {
		if !safePublicIP(a.IP) {
			return errors.New("公网 HTTPS 解析包含不允许的地址。")
		}
	}
	// Pin the addresses checked above to avoid DNS rebinding. TLS uses the actual
	// customer hostname, with the system CA trust store and redirects disabled.
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	tr := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second}
	tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		var last error
		for _, a := range addresses {
			c, e := dialer.DialContext(ctx, "tcp", net.JoinHostPort(a.IP.String(), "443"))
			if e == nil {
				return c, nil
			}
			last = e
		}
		return nil, last
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return probeDomainHTTPSWithClient(ctx, client, host, token)
}

func (s *ManagementService) progressSaaSConnection(ctx context.Context, row *domain.Domain, resolver domainDNSResolver, originProbe func(context.Context, string, string, string) error, edgeProbe func(context.Context, string, string) error) error {
	if row == nil || row.AutoConnectRequestedAt == nil || row.ConnectMode != contract.DomainConnectCloudflareSaaS || row.Status != domain.DomainStatusPendingReview || row.Type != domain.DomainTypeCustom {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	dnsErr := verifySaaSDNS(checkCtx, row, resolver)
	var host *contract.SaaSHostname
	var tlsErr error
	if dnsErr == nil {
		if !s.saasReady() {
			tlsErr = contract.ErrSaaSUnavailable
		} else {
			host, tlsErr = s.saasProvider.EnsureHostname(checkCtx, row.Domain, row.CloudflareHostnameID)
		}
	}
	ready := false
	if tlsErr == nil && host != nil && host.Status == "active" && host.SSLStatus == "active" && row.TLSStatus == "provisioned" {
		tlsErr = originProbe(checkCtx, row.Domain, s.cfg.DomainEntryIP, row.VerificationToken)
		if tlsErr == nil {
			tlsErr = edgeProbe(checkCtx, row.Domain, row.VerificationToken)
		}
		ready = tlsErr == nil
	}
	now := time.Now().UTC()
	activated := false
	err := s.store.WithinManagementTransaction(func(tx contract.ManagementStore) error {
		current, e := tx.GetDomainByIDForUpdate(row.ID)
		if e != nil {
			return e
		}
		if current == nil || current.ResellerID != row.ResellerID || current.Status != domain.DomainStatusPendingReview || current.ConnectMode != contract.DomainConnectCloudflareSaaS || current.VerificationToken != row.VerificationToken || current.AutoConnectRequestedAt == nil || !current.AutoConnectRequestedAt.Equal(*row.AutoConnectRequestedAt) || current.CloudflareHostnameID != row.CloudflareHostnameID {
			return nil
		}
		profile, e := tx.GetProfileByID(current.ResellerID)
		if e != nil {
			return e
		}
		if profile == nil || profile.Status != domain.ProfileStatusActive {
			return nil
		}
		current.LastDNSCheckAt = &now
		if dnsErr != nil {
			current.LastDNSError = dnsErr.Error()
			current.VerificationStatus = domain.DomainVerificationPending
			current.VerifiedAt = nil
			return tx.UpdateDomain(current)
		}
		current.VerificationStatus = domain.DomainVerificationVerified
		current.VerifiedAt = &now
		current.LastDNSError = ""
		if host != nil {
			current.CloudflareHostnameID = host.ID
			current.CloudflareHostnameStatus = host.Status
			current.CloudflareSSLStatus = host.SSLStatus
			b, _ := json.Marshal(host.ValidationRecords)
			current.CloudflareValidationJSON = string(b)
		}
		if tlsErr != nil {
			current.LastTLSError = tlsErr.Error()
		} else if current.TLSStatus != "failed" {
			current.LastTLSError = ""
		}
		if ready && current.TLSStatus == "provisioned" && current.CloudflareHostnameStatus == "active" && current.CloudflareSSLStatus == "active" {
			current.TLSStatus = "ready"
			current.TLSReadyAt = &now
			current.Status = domain.DomainStatusActive
			all, e := tx.ListDomainsByResellerID(current.ResellerID)
			if e != nil {
				return e
			}
			if !hasActiveVerifiedPrimary(all, current.ID) {
				current.IsPrimary = true
			}
			activated = true
		}
		return tx.UpdateDomain(current)
	})
	if err == nil && activated {
		_ = cache.DelResellerDomain(ctx, row.Domain)
	}
	return err
}

// decodeSaaSRecords permits only public DNS challenges scoped to this hostname.
func decodeSaaSRecords(row *domain.Domain) []DomainDNSRecord {
	var records []DomainDNSRecord
	if len(row.CloudflareValidationJSON) > 8192 || json.Unmarshal([]byte(row.CloudflareValidationJSON), &records) != nil {
		return nil
	}
	safe := []DomainDNSRecord{}
	for _, r := range records {
		if r.Type == "TXT" && strings.HasSuffix(r.Name, "."+row.Domain) && !strings.ContainsAny(r.Name+r.Value, "\r\n\x00") {
			safe = append(safe, r)
		}
	}
	return safe
}
