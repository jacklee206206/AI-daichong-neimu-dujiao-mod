// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	"github.com/dujiao-next/internal/config"
)

type resellerResolverRepoStub struct {
	domain     *resellerdomain.Domain
	err        error
	calls      int
	primary    *resellerdomain.Domain
	primaryErr error
}

func (s *resellerResolverRepoStub) FindActivePrimaryDomain(resellerID uint) (*resellerdomain.Domain, error) {
	return s.primary, s.primaryErr
}

func (s *resellerResolverRepoStub) FindActiveVerifiedDomain(host string) (*resellerdomain.Domain, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.domain, nil
}

func TestResellerDomainResolverDisabledTreatsEmptyHostAsMain(t *testing.T) {
	repo := &resellerResolverRepoStub{}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: false})
	tenant, err := resolver.ResolveHost(context.Background(), "")
	if err != nil {
		t.Fatalf("disabled resolver should not fail on empty host: %v", err)
	}
	if !tenant.IsMain || tenant.Unavailable || tenant.ResellerID != nil {
		t.Fatalf("disabled resolver should return main tenant for empty host, got %+v", tenant)
	}
	if repo.calls != 0 {
		t.Fatalf("disabled resolver should not query repo, calls=%d", repo.calls)
	}
}

func TestResellerDomainResolverMainHost(t *testing.T) {
	repo := &resellerResolverRepoStub{}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true, MainHosts: []string{"main.example.test"}})
	tenant, err := resolver.ResolveHost(context.Background(), "MAIN.example.test:443")
	if err != nil {
		t.Fatalf("resolve main host failed: %v", err)
	}
	if !tenant.IsMain || tenant.Unavailable {
		t.Fatalf("expected main tenant, got %+v", tenant)
	}
	if repo.calls != 0 {
		t.Fatalf("main host should not query repo, calls=%d", repo.calls)
	}
}

func TestResellerDomainResolverActiveDomain(t *testing.T) {
	id := uint(7)
	repo := &resellerResolverRepoStub{domain: &resellerdomain.Domain{
		ID:                 11,
		ResellerID:         id,
		Domain:             "shop.example.test",
		Status:             resellerdomain.DomainStatusActive,
		VerificationStatus: resellerdomain.DomainVerificationVerified,
		Profile:            &resellerdomain.Profile{ID: id, UserID: 88, Status: resellerdomain.ProfileStatusActive},
	}}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true, MainHosts: []string{"main.example.test"}})
	tenant, err := resolver.ResolveHost(context.Background(), "shop.example.test")
	if err != nil {
		t.Fatalf("resolve reseller host failed: %v", err)
	}
	if tenant.IsMain || tenant.ResellerID == nil || *tenant.ResellerID != id {
		t.Fatalf("expected reseller tenant, got %+v", tenant)
	}
	if tenant.ResellerUserID != 88 {
		t.Fatalf("expected reseller user id 88, got %d", tenant.ResellerUserID)
	}
}

func TestResellerDomainResolverInactiveProfileUnavailable(t *testing.T) {
	id := uint(7)
	repo := &resellerResolverRepoStub{domain: &resellerdomain.Domain{
		ID:         11,
		ResellerID: id,
		Domain:     "shop.example.test",
		Status:     resellerdomain.DomainStatusActive,
		Profile:    &resellerdomain.Profile{ID: id, UserID: 88, Status: resellerdomain.ProfileStatusDisabled},
	}}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true, MainHosts: []string{"main.example.test"}})
	tenant, err := resolver.ResolveHost(context.Background(), "shop.example.test")
	if err != nil {
		t.Fatalf("resolve disabled profile host failed: %v", err)
	}
	if !tenant.Unavailable || tenant.ResellerID != nil {
		t.Fatalf("expected disabled profile to be unavailable, got %+v", tenant)
	}
}

func TestResellerDomainResolverRequestUsesTrustedForwardedHost(t *testing.T) {
	id := uint(8)
	repo := &resellerResolverRepoStub{domain: &resellerdomain.Domain{
		ID:                 12,
		ResellerID:         id,
		Domain:             "shop.example.test",
		Profile:            &resellerdomain.Profile{ID: id, UserID: 89, Status: resellerdomain.ProfileStatusActive},
		Status:             resellerdomain.DomainStatusActive,
		VerificationStatus: resellerdomain.DomainVerificationVerified,
	}}
	resolver := NewDomainResolver(repo, config.ResellerConfig{
		Enabled:              true,
		MainHosts:            []string{"main.example.test"},
		TrustedForwardedHost: true,
	})
	req, err := http.NewRequest(http.MethodGet, "https://internal.example.test/api/v1/public/config", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Host = "internal.example.test"
	req.Header.Set("X-Forwarded-Host", "shop.example.test")
	tenant, err := resolver.ResolveRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("resolve request failed: %v", err)
	}
	if tenant.ResellerID == nil || *tenant.ResellerID != id {
		t.Fatalf("expected forwarded reseller tenant, got %+v", tenant)
	}
	if repo.calls != 1 {
		t.Fatalf("expected one repo lookup for forwarded host, calls=%d", repo.calls)
	}
}

func TestDomainResolverUsesVerifiedPrimaryForBothAliasOrders(t *testing.T) {
	for _, hosts := range [][]string{{"shop.example.test", "www.shop.example.test"}, {"www.shop.example.test", "shop.example.test"}} {
		t.Run(hosts[0], func(t *testing.T) {
			now := time.Now()
			primary := &resellerdomain.Domain{ID: 1, ResellerID: 7, Domain: "shop.example.test", IsPrimary: true,
				Status: resellerdomain.DomainStatusActive, VerificationStatus: resellerdomain.DomainVerificationVerified,
				ConnectMode: resellercontract.DomainConnectCloudflareSaaS, TLSStatus: "ready", TLSReadyAt: &now,
				CloudflareHostnameID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", CloudflareHostnameStatus: "active", CloudflareSSLStatus: "active",
				Profile: &resellerdomain.Profile{ID: 7, UserID: 88, Status: resellerdomain.ProfileStatusActive}}
			repo := &resellerResolverRepoStub{primary: primary}
			resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true})
			for _, host := range hosts {
				row := *primary
				row.Domain, row.IsPrimary = host, host == primary.Domain
				repo.domain = &row
				tenant, err := resolver.ResolveHost(context.Background(), host)
				if err != nil || !tenant.IsReseller() || tenant.Host != host || tenant.PrimaryDomain != primary.Domain {
					t.Fatalf("host %s: tenant=%+v err=%v", host, tenant, err)
				}
			}
		})
	}
}

func TestDomainResolverRejectsInvalidHostsAndUnsafePrimary(t *testing.T) {
	now := time.Now()
	valid := resellerdomain.Domain{ID: 2, ResellerID: 7, Domain: "www.shop.example.test",
		Status: resellerdomain.DomainStatusActive, VerificationStatus: resellerdomain.DomainVerificationVerified,
		ConnectMode: resellercontract.DomainConnectCloudflareSaaS, TLSStatus: "ready", TLSReadyAt: &now,
		CloudflareHostnameID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", CloudflareHostnameStatus: "active", CloudflareSSLStatus: "active",
		Profile: &resellerdomain.Profile{ID: 7, UserID: 88, Status: resellerdomain.ProfileStatusActive}}
	for _, tc := range []struct {
		name   string
		mutate func(*resellerdomain.Domain)
	}{
		{"disabled", func(d *resellerdomain.Domain) { d.Status = resellerdomain.DomainStatusDisabled }},
		{"unverified", func(d *resellerdomain.Domain) { d.VerificationStatus = resellerdomain.DomainVerificationPending }},
		{"deleted", func(d *resellerdomain.Domain) { d.DeletedAt = &now }},
		{"tls_pending", func(d *resellerdomain.Domain) { d.TLSStatus = "pending" }},
		{"tls_no_proof", func(d *resellerdomain.Domain) { d.TLSReadyAt = nil }},
		{"edge_pending", func(d *resellerdomain.Domain) { d.CloudflareHostnameStatus = "pending" }},
		{"edge_ssl_pending", func(d *resellerdomain.Domain) { d.CloudflareSSLStatus = "pending" }},
		{"no_cf_id", func(d *resellerdomain.Domain) { d.CloudflareHostnameID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := valid
			tc.mutate(&bad)
			repo := &resellerResolverRepoStub{domain: &bad}
			resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true})
			tenant, err := resolver.ResolveHost(context.Background(), valid.Domain)
			if err != nil || !tenant.Unavailable {
				t.Fatalf("unsafe host resolved: %+v %v", tenant, err)
			}
			bad.Domain, bad.IsPrimary = "shop.example.test", true
			repo.domain, repo.primary = &valid, &bad
			tenant, err = resolver.ResolveHost(context.Background(), valid.Domain)
			if err != nil || !tenant.IsReseller() || tenant.PrimaryDomain != valid.Domain {
				t.Fatalf("unsafe primary used: %+v %v", tenant, err)
			}
		})
	}
	other := valid
	other.Domain, other.IsPrimary, other.ResellerID = "other.example.test", true, 99
	repo := &resellerResolverRepoStub{domain: &valid, primary: &other}
	tenant, err := NewDomainResolver(repo, config.ResellerConfig{Enabled: true}).ResolveHost(context.Background(), valid.Domain)
	if err != nil || tenant.PrimaryDomain != valid.Domain {
		t.Fatalf("cross-tenant primary used: %+v %v", tenant, err)
	}
	repo.primaryErr = errors.New("primary lookup failed")
	if _, err := NewDomainResolver(repo, config.ResellerConfig{Enabled: true}).ResolveHost(context.Background(), valid.Domain); err == nil {
		t.Fatal("primary lookup error ignored")
	}
}

func TestResellerDomainResolverUnknownDomainUnavailable(t *testing.T) {
	repo := &resellerResolverRepoStub{}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true, MainHosts: []string{"main.example.test"}})
	tenant, err := resolver.ResolveHost(context.Background(), "unknown.example.test")
	if err != nil {
		t.Fatalf("unknown domain should not return technical error: %v", err)
	}
	if !tenant.Unavailable || tenant.UnavailableReason != DomainUnavailableNotFound {
		t.Fatalf("expected unavailable not_found, got %+v", tenant)
	}
}

func TestResellerDomainResolverRepoError(t *testing.T) {
	repo := &resellerResolverRepoStub{err: errors.New("db down")}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true, MainHosts: []string{"main.example.test"}})
	_, err := resolver.ResolveHost(context.Background(), "shop.example.test")
	if err == nil {
		t.Fatal("expected repository error")
	}
}
