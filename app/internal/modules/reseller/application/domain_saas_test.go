// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/modules/reseller/domain"
)

type fakeSaaS struct {
	ready       bool
	calls       int
	status, ssl string
	err         error
	platformErr error
	onEnsure    func()
}

func (f *fakeSaaS) CheckPlatform(context.Context) error { return f.platformErr }
func (f *fakeSaaS) Ready() bool                         { return f.ready }
func (f *fakeSaaS) EnsureHostname(_ context.Context, host, id string) (*contract.SaaSHostname, error) {
	f.calls++
	if f.onEnsure != nil {
		f.onEnsure()
	}
	if f.err != nil {
		return nil, f.err
	}
	return &contract.SaaSHostname{ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Hostname: host, Status: f.status, SSLStatus: f.ssl, ValidationRecords: []contract.DNSRecord{{Type: "TXT", Name: "_cf-custom-hostname." + host, Value: "edge-proof"}}}, nil
}
func setSaaS(s *ManagementService, f *fakeSaaS) {
	s.cfg.DomainConnectMode = contract.DomainConnectCloudflareSaaS
	s.cfg.DomainEntryHost = "entry.shop.example.com"
	s.SetSaaSProvider(f)
}

func TestSaaSWithoutPlatformTokenDoesNotReserveOrWriteDNS(t *testing.T) {
	s, db, dns, uid, _ := connectTestService(t)
	setSaaS(s, &fakeSaaS{})
	info, err := s.InspectDomainConnect(context.Background(), uid, "example.com")
	if err != nil || info.PlatformReady || info.ConnectMode != contract.DomainConnectCloudflareSaaS || info.PlatformMessage == "" {
		t.Fatalf("bad readiness %+v %v", info, err)
	}
	for _, provider := range []string{"manual", "cloudflare"} {
		if _, err = s.ConnectUserDomain(context.Background(), uid, "example.com", provider, nil); !errors.Is(err, contract.ErrSaaSUnavailable) {
			t.Fatal(err)
		}
	}
	var count int64
	db.Model(&domain.Domain{}).Count(&count)
	if count != 0 || dns.authorizations != 0 || dns.writes != 0 {
		t.Fatal("unconfigured service mutated")
	}
}
func TestSaaSManualSubmissionAndIdempotentPair(t *testing.T) {
	s, db, dns, uid, _ := connectTestService(t)
	cf := &fakeSaaS{ready: true}
	setSaaS(s, cf)
	first, err := s.ConnectUserDomain(context.Background(), uid, "example.com", "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.ConnectUserDomain(context.Background(), uid, "example.com", "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Domains) != 2 || first.Domains[0].VerificationToken == first.Domains[1].VerificationToken || first.Domains[0].VerificationToken != again.Domains[0].VerificationToken || dns.writes != 0 || cf.calls != 0 {
		t.Fatal("unsafe submission")
	}
	var count int64
	db.Model(&domain.Domain{}).Count(&count)
	if count != 2 {
		t.Fatal("duplicate pair")
	}
	setup, err := s.DomainConnectionSetupForUser(uid, first.Domains[0].ID)
	if err != nil || setup.Records[0].Type != "CNAME" || setup.Records[0].Value != "entry.shop.example.com" {
		t.Fatalf("bad setup %+v %v", setup, err)
	}
	if _, err = s.DomainConnectionSetupForUser(uid+1, first.Domains[0].ID); err == nil {
		t.Fatal("ownership leak")
	}
	if _, err = s.ApproveDomain(context.Background(), 1, first.Domains[0].ID); err == nil {
		t.Fatal("admin approval bypassed live SaaS checks")
	}
}
func TestSaaSAuthorizedDNSUsesCNAMEAndPreservesLegacy(t *testing.T) {
	s, _, dns, uid, _ := connectTestService(t)
	setSaaS(s, &fakeSaaS{ready: true})
	result, err := s.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", nil)
	if err != nil {
		t.Fatal(err)
	}
	if dns.authorizations != 1 || dns.writes != 1 || len(dns.records) != 4 || dns.records[0].Type != "CNAME" || dns.records[0].Value != s.cfg.DomainEntryHost || result.Domains[0].ConnectMode != contract.DomainConnectCloudflareSaaS {
		t.Fatal("wrong DNS mode")
	}
	if result.Domains[0].VerificationToken == result.Domains[1].VerificationToken {
		t.Fatal("root and www must receive independent ownership tokens")
	}
	for i, row := range result.Domains {
		wantHost := []string{"example.com", "www.example.com"}[i]
		if row.Domain != wantHost || row.ConnectMode != contract.DomainConnectCloudflareSaaS ||
			dns.records[2*i] != (contract.DNSRecord{Type: "CNAME", Name: wantHost, Value: s.cfg.DomainEntryHost}) ||
			dns.records[2*i+1] != (contract.DNSRecord{Type: "TXT", Name: "_opengpt-verification." + wantHost, Value: row.VerificationToken}) {
			t.Fatal("SaaS records did not preserve each hostname and its ownership token")
		}
	}
}

func TestSaaSRequiresOwnershipEdgeOriginAndPublicProof(t *testing.T) {
	s, db, _, uid, _ := connectTestService(t)
	cf := &fakeSaaS{ready: true, status: "pending", ssl: "pending_validation"}
	setSaaS(s, cf)
	result, err := s.ConnectUserDomain(context.Background(), uid, "example.com", "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	row := result.Domains[0]
	dns := fakeDomainDNS{txt: []string{"wrong"}, ips: []net.IPAddr{{IP: net.ParseIP("104.18.0.1")}}}
	originCalls, edgeCalls := 0, 0
	origin := func(context.Context, string, string, string) error { originCalls++; return nil }
	edge := func(context.Context, string, string) error { edgeCalls++; return errors.New("edge not ready") }
	progress := func() {
		t.Helper()
		if err := s.progressSaaSConnection(context.Background(), &row, dns, origin, edge); err != nil {
			t.Fatal(err)
		}
		db.First(&row, row.ID)
	}
	progress()
	if cf.calls != 0 {
		t.Fatal("Cloudflare called before ownership")
	}
	dns.txt = []string{row.VerificationToken}
	progress()
	if row.CloudflareHostnameID == "" || row.Status == domain.DomainStatusActive || originCalls != 0 {
		t.Fatal("edge pending became active")
	}
	cf.status = "active"
	cf.ssl = "active"
	progress()
	if originCalls != 0 {
		t.Fatal("probed unprovisioned origin")
	}
	db.Model(&domain.Domain{}).Where("id = ?", row.ID).Update("tls_status", "provisioned")
	row.TLSStatus = "provisioned"
	progress()
	if row.Status == domain.DomainStatusActive || row.LastTLSError == "" || edgeCalls != 1 {
		t.Fatal("public failure became ready")
	}
	origin = func(context.Context, string, string, string) error { return errors.New("origin invalid") }
	edgeCalls = 0
	progress()
	if edgeCalls != 0 || row.Status == domain.DomainStatusActive {
		t.Fatal("origin failure bypass")
	}
	origin = func(_ context.Context, host, ip, token string) error {
		if host != row.Domain || ip != s.cfg.DomainEntryIP || token != row.VerificationToken {
			t.Fatal("unpinned origin")
		}
		return nil
	}
	edge = func(context.Context, string, string) error { return nil }
	progress()
	if row.Status != domain.DomainStatusActive || row.TLSStatus != "ready" || row.TLSReadyAt == nil {
		t.Fatalf("not active %+v", row)
	}
}
func TestSaaSCannotActivateAfterRevocation(t *testing.T) {
	for _, change := range []string{"domain", "profile", "token", "mode", "request", "requeued"} {
		t.Run(change, func(t *testing.T) {
			s, db, _, uid, pid := connectTestService(t)
			cf := &fakeSaaS{ready: true, status: "active", ssl: "active"}
			setSaaS(s, cf)
			result, err := s.ConnectUserDomain(context.Background(), uid, "example.com", "manual", nil)
			if err != nil {
				t.Fatal(err)
			}
			row := result.Domains[0]
			db.Model(&domain.Domain{}).Where("id = ?", row.ID).Update("tls_status", "provisioned")
			row.TLSStatus = "provisioned"
			dns := fakeDomainDNS{txt: []string{row.VerificationToken}, ips: []net.IPAddr{{IP: net.ParseIP("104.18.0.1")}}}
			edge := func(context.Context, string, string) error {
				switch change {
				case "domain":
					db.Model(&domain.Domain{}).Where("id = ?", row.ID).Update("status", "disabled")
				case "profile":
					db.Model(&domain.Profile{}).Where("id = ?", pid).Update("status", "disabled")
				case "token":
					db.Model(&domain.Domain{}).Where("id = ?", row.ID).Update("verification_token", "changed")
				case "mode":
					db.Model(&domain.Domain{}).Where("id = ?", row.ID).Update("connect_mode", "legacy")
				case "request":
					db.Model(&domain.Domain{}).Where("id = ?", row.ID).Update("auto_connect_requested_at", nil)
				case "requeued":
					db.Model(&domain.Domain{}).Where("id = ?", row.ID).Update("auto_connect_requested_at", time.Now().Add(time.Minute))
				}
				return nil
			}
			if err = s.progressSaaSConnection(context.Background(), &row, dns, func(context.Context, string, string, string) error { return nil }, edge); err != nil {
				t.Fatal(err)
			}
			db.First(&row, row.ID)
			if row.Status == domain.DomainStatusActive || row.TLSReadyAt != nil {
				t.Fatal("revoked domain activated")
			}
		})
	}
}
func TestSaaSRejectsPrivateAndReservedProbeTargets(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.1.1.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "198.18.0.1", "240.1.1.1", "::1", "fd00::1", "2001:db8::1"} {
		if safePublicIP(net.ParseIP(raw)) {
			t.Errorf("allowed %s", raw)
		}
	}
}

func TestSaaSRestartPreservesIdentityAndIsExplicitIdempotent(t *testing.T) {
	s, db, dns, uid, _ := connectTestService(t)
	first, err := s.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", nil)
	if err != nil {
		t.Fatal(err)
	}
	old := first.Domains[0]
	now := time.Now()
	db.Model(&domain.Domain{}).Where("id = ?", old.ID).Updates(map[string]any{"status": domain.DomainStatusActive, "tls_status": "ready", "tls_ready_at": &now})
	cf := &fakeSaaS{ready: true}
	setSaaS(s, cf)
	info, err := s.InspectDomainConnect(context.Background(), uid, "example.com")
	if err != nil || !info.CanRestart || len(info.Conflicts) != 0 || info.RestartMessage == "" {
		t.Fatalf("restart unavailable %+v %v", info, err)
	}
	if _, err = s.ConnectUserDomain(context.Background(), uid, "example.com", "manual", nil); !errors.Is(err, contract.ErrDomainConflict) {
		t.Fatal("implicit migration allowed", err)
	}
	writes := dns.writes
	migrated, err := s.RestartUserDomainConnect(context.Background(), uid, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	row := migrated.Domains[0]
	if row.ID != old.ID || row.ResellerID != old.ResellerID || row.VerificationToken != old.VerificationToken || row.Status != domain.DomainStatusPendingReview || row.TLSReadyAt != nil || row.ConnectMode != contract.DomainConnectCloudflareSaaS || dns.writes != writes || cf.calls != 0 {
		t.Fatalf("bad migration %+v", row)
	}
	// Simulate subsequent worker completion. Replaying confirmation cannot reset it.
	db.Model(&domain.Domain{}).Where("id = ?", row.ID).Updates(map[string]any{"status": domain.DomainStatusActive, "tls_status": "ready", "tls_ready_at": &now})
	again, err := s.RestartUserDomainConnect(context.Background(), uid, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if again.Domains[0].Status != domain.DomainStatusActive || !again.Domains[0].AutoConnectRequestedAt.Equal(*row.AutoConnectRequestedAt) {
		t.Fatal("replay reset live shop")
	}
}

func TestSaaSRestartFailsWithoutMutatingClaims(t *testing.T) {
	for _, reason := range []string{"no_token", "platform_failed", "foreign", "disabled", "profile_disabled"} {
		t.Run(reason, func(t *testing.T) {
			s, db, _, uid, pid := connectTestService(t)
			first, err := s.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", nil)
			if err != nil {
				t.Fatal(err)
			}
			old := first.Domains[0]
			cf := &fakeSaaS{ready: true}
			setSaaS(s, cf)
			switch reason {
			case "no_token":
				cf.ready = false
			case "platform_failed":
				cf.platformErr = contract.ErrSaaSRequest
			case "foreign":
				db.Model(&domain.Domain{}).Where("id = ?", old.ID).Update("reseller_id", pid+1)
			case "disabled":
				db.Model(&domain.Domain{}).Where("id = ?", old.ID).Update("status", domain.DomainStatusDisabled)
			case "profile_disabled":
				db.Model(&domain.Profile{}).Where("id = ?", pid).Update("status", domain.ProfileStatusDisabled)
			}
			if _, err = s.RestartUserDomainConnect(context.Background(), uid, "example.com"); err == nil {
				t.Fatal("unauthorized restart")
			}
			var rows []domain.Domain
			db.Find(&rows)
			for _, row := range rows {
				if row.ConnectMode == contract.DomainConnectCloudflareSaaS {
					t.Fatal("failed restart partially changed pair")
				}
			}
		})
	}
}
