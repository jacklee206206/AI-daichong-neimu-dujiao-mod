// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/dujiao-next/internal/config"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	contract "github.com/dujiao-next/internal/modules/reseller/contract"
	domain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type fakeConnectDNS struct {
	authErr, ensureErr     error
	records                []contract.DNSRecord
	writes, authorizations int
	onEnsure               func()
}

func (f *fakeConnectDNS) Detect(context.Context, string) (contract.DNSInspection, error) {
	return contract.DNSInspection{Provider: "cloudflare", ProviderLabel: "Cloudflare", Supported: true}, nil
}
func (f *fakeConnectDNS) Authorize(context.Context, string, string, contract.DNSCredentials) error {
	f.authorizations++
	return f.authErr
}
func (f *fakeConnectDNS) EnsureRecords(_ context.Context, _, _ string, _ contract.DNSCredentials, r []contract.DNSRecord) error {
	f.writes++
	f.records = r
	if f.onEnsure != nil {
		f.onEnsure()
	}
	return f.ensureErr
}

func connectTestService(t *testing.T) (*ManagementService, *gorm.DB, *fakeConnectDNS, uint, uint) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:connect-%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&userdomain.User{}, &domain.Profile{}, &domain.Domain{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	db.Exec("CREATE UNIQUE INDEX idx_connect_domain ON reseller_domains(domain) WHERE deleted_at IS NULL")
	u := userdomain.User{Email: "agent@example.com", PasswordHash: "test"}
	if err = db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	p := domain.Profile{UserID: u.ID, Status: domain.ProfileStatusActive}
	if err = db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	provider := &fakeConnectDNS{}
	svc := NewManagementService(gormstore.New(db), config.ResellerConfig{Enabled: true, DomainAutoConnectEnabled: true, DomainEntryIP: "8.8.4.4", MainHosts: []string{"legacy.example.org", "www.legacy.example.org", "next.legacy.example.org"}})
	svc.SetDNSProvider(provider)
	return svc, db, provider, u.ID, p.ID
}

func TestConnectRejectsInvalidOrPlatformRootsBeforeDNS(t *testing.T) {
	svc, db, dns, uid, _ := connectTestService(t)
	for _, host := range []string{"https://example.com", "www.example.com", "a.example.com", "example.invalid", "127.0.0.1", "example.com:443", "legacy.example.org", "next.legacy.example.org", "evil.com\nserver", "example.com@evil.com", "例子.com"} {
		if _, err := svc.ConnectUserDomain(context.Background(), uid, host, "cloudflare", contract.DNSCredentials{}); err == nil {
			t.Errorf("accepted %s", host)
		}
	}
	var count int64
	db.Model(&domain.Domain{}).Count(&count)
	if count != 0 || dns.authorizations != 0 {
		t.Fatal("invalid input triggered DNS/reservation")
	}
	for _, host := range []string{"example.com", "example.cn", "example.shop", "example.net", "example.vip", "example.cc", "example.top", "example.co.uk", "xn--fsqu00a.com"} {
		if _, err := validateConnectRoot(host, svc.cfg); err != nil {
			t.Errorf("valid root rejected %s: %v", host, err)
		}
	}
}

func TestConnectRequiresAuthorizationBeforeReservation(t *testing.T) {
	svc, db, dns, uid, _ := connectTestService(t)
	dns.authErr = contract.ErrDNSCredentialsInvalid
	credentials := contract.DNSCredentials{"api_token": "do-not-persist"}
	if _, err := svc.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", credentials); !errors.Is(err, contract.ErrDNSCredentialsInvalid) {
		t.Fatal(err)
	}
	var count int64
	db.Model(&domain.Domain{}).Count(&count)
	if count != 0 || dns.writes != 0 || len(credentials) != 0 {
		t.Fatal("unauthorized request reserved domain, wrote DNS or retained credentials")
	}
}

func TestConnectPairConflictIsAtomicAndPrivate(t *testing.T) {
	svc, db, dns, uid, pid := connectTestService(t)
	db.Create(&domain.Domain{Domain: "www.example.com", ResellerID: pid + 1, Type: domain.DomainTypeCustom, Status: domain.DomainStatusPendingReview, VerificationToken: "other-secret"})
	inspection, err := svc.InspectDomainConnect(context.Background(), uid, "example.com")
	if err != nil || len(inspection.Conflicts) != 1 {
		t.Fatalf("missing conflict: %+v %v", inspection, err)
	}
	if _, err = svc.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", contract.DNSCredentials{}); !errors.Is(err, contract.ErrDomainConflict) {
		t.Fatal(err)
	}
	var count int64
	db.Model(&domain.Domain{}).Count(&count)
	if count != 1 || dns.writes != 0 {
		t.Fatal("partial pair or foreign DNS write")
	}
	if _, err = svc.UserDomainConnectStatus(uid, "example.com"); err == nil {
		t.Fatal("foreign status disclosed")
	}
}

func TestConnectRejectsReadyActiveManualDomainBeforeReservingPair(t *testing.T) {
	for _, host := range []string{"example.com", "www.example.com"} {
		t.Run(host, func(t *testing.T) {
			svc, db, dns, uid, pid := connectTestService(t)
			now := time.Now().UTC()
			row := domain.Domain{Domain: host, ResellerID: pid, Type: domain.DomainTypeCustom,
				Status: domain.DomainStatusActive, VerificationStatus: domain.DomainVerificationVerified,
				VerificationToken: "existing-manual-proof", TLSStatus: "ready", TLSReadyAt: &now, IsPrimary: true}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			inspection, err := svc.InspectDomainConnect(context.Background(), uid, "example.com")
			if err != nil {
				t.Fatal(err)
			}
			if len(inspection.Conflicts) != 1 || inspection.Conflicts[0] != host+" 已手动启用，无需重复自动接入；如需改接，请联系管理员" {
				t.Fatalf("missing manual-domain explanation: %+v", inspection.Conflicts)
			}
			// Calling the write endpoint directly must enforce the same rule inside
			// the reservation transaction, including when only www already exists.
			if _, err := svc.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", contract.DNSCredentials{}); !errors.Is(err, contract.ErrDomainConflict) {
				t.Fatalf("active manual domain entered automatic connection: %v", err)
			}
			var count int64
			if err := db.Model(&domain.Domain{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1 || dns.writes != 0 {
				t.Fatal("rejected request created a partial pair or changed DNS")
			}
			var current domain.Domain
			if err := db.First(&current, row.ID).Error; err != nil {
				t.Fatal(err)
			}
			if current.Status != row.Status || current.VerificationStatus != row.VerificationStatus || current.VerificationToken != row.VerificationToken || current.TLSStatus != row.TLSStatus || current.TLSReadyAt == nil || !current.TLSReadyAt.Equal(now) || current.AutoConnectRequestedAt != nil || !current.IsPrimary {
				t.Fatalf("manual binding was changed by rejected automatic request: %+v", current)
			}
		})
	}
}

func TestConnectRecoversUnreadyManualDomainsOnlyAfterDNSWrite(t *testing.T) {
	for _, tc := range []struct {
		name      string
		hosts     []string
		tlsStatus string
		readyAt   bool
	}{
		{"root", []string{"example.com"}, "pending", false},
		{"www", []string{"www.example.com"}, "pending", false},
		{"both active", []string{"example.com", "www.example.com"}, "pending", false},
		{"ready without timestamp", []string{"example.com"}, "ready", false},
		{"timestamp without ready", []string{"example.com"}, "pending", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, db, dns, uid, pid := connectTestService(t)
			now := time.Now().UTC()
			for _, host := range tc.hosts {
				row := domain.Domain{Domain: host, ResellerID: pid, Type: domain.DomainTypeCustom,
					Status: domain.DomainStatusActive, VerificationStatus: domain.DomainVerificationVerified,
					VerificationToken: "manual-proof-" + host, VerifiedAt: &now, TLSStatus: tc.tlsStatus,
					LastDNSCheckAt: &now, LastDNSError: "old error", LastTLSError: "old TLS error", IsPrimary: true}
				if tc.readyAt {
					row.TLSReadyAt = &now
				}
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			inspection, err := svc.InspectDomainConnect(context.Background(), uid, "example.com")
			if err != nil || len(inspection.Conflicts) != 0 {
				t.Fatalf("unready manual site could not recover: %+v %v", inspection, err)
			}
			dns.onEnsure = func() {
				for _, host := range tc.hosts {
					var current domain.Domain
					if err := db.Where("domain = ?", host).First(&current).Error; err != nil {
						t.Fatal(err)
					}
					if current.Status != domain.DomainStatusActive || current.AutoConnectRequestedAt != nil {
						t.Fatal("manual site was downgraded before DNS completed")
					}
				}
			}
			result, err := svc.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", contract.DNSCredentials{})
			if err != nil {
				t.Fatal(err)
			}
			if dns.writes != 1 || len(dns.records) != 4 || result.Phase != "dns_pending" || len(result.Domains) != 2 {
				t.Fatalf("recovery bypassed DNS or did not queue both domains: %+v", result)
			}
			for _, row := range result.Domains {
				if row.Status != domain.DomainStatusPendingReview || row.VerificationStatus != domain.DomainVerificationPending || row.VerifiedAt != nil || row.TLSStatus != "pending" || row.TLSReadyAt != nil || row.AutoConnectRequestedAt == nil || row.DNSProvider != "cloudflare" || row.IsPrimary || row.LastDNSCheckAt != nil || row.LastDNSError != "" || row.LastTLSError != "" {
					t.Fatalf("recovery kept stale readiness or missed queue request: %+v", row)
				}
			}
			queued, err := gormstore.New(db).ListAutoConnectDomains(20)
			if err != nil || len(queued) != 2 {
				t.Fatalf("recovered rows are not in the normal worker queue: %d %v", len(queued), err)
			}
		})
	}
}

func TestConnectManualRecoveryFailurePreservesExistingActiveDomain(t *testing.T) {
	for _, host := range []string{"example.com", "www.example.com"} {
		for _, failure := range []string{"authorization", "DNS"} {
			t.Run(host+"/"+failure, func(t *testing.T) {
				svc, db, dns, uid, pid := connectTestService(t)
				now := time.Now().UTC()
				row := domain.Domain{Domain: host, ResellerID: pid, Type: domain.DomainTypeCustom,
					Status: domain.DomainStatusActive, VerificationStatus: domain.DomainVerificationVerified,
					VerificationToken: "manual-proof", VerifiedAt: &now, TLSStatus: "pending", LastDNSError: "old error", IsPrimary: true}
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
				wantErr := contract.ErrDNSCredentialsInvalid
				if failure == "authorization" {
					dns.authErr = wantErr
				} else {
					wantErr = contract.ErrDNSRecordConflict
					dns.ensureErr = wantErr
				}
				credentials := contract.DNSCredentials{"api_token": "fake-only"}
				if _, err := svc.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", credentials); !errors.Is(err, wantErr) {
					t.Fatalf("unexpected failure: %v", err)
				}
				var current domain.Domain
				if err := db.First(&current, row.ID).Error; err != nil {
					t.Fatal(err)
				}
				if current.Status != row.Status || current.VerificationStatus != row.VerificationStatus || current.VerifiedAt == nil || !current.VerifiedAt.Equal(now) || current.VerificationToken != row.VerificationToken || current.TLSStatus != row.TLSStatus || current.TLSReadyAt != nil || current.AutoConnectRequestedAt != nil || current.DNSProvider != "" || current.LastDNSError != row.LastDNSError || !current.IsPrimary || len(credentials) != 0 {
					t.Fatalf("failed authorization/DNS downgraded the old binding: %+v", current)
				}
				queued, err := gormstore.New(db).ListAutoConnectDomains(20)
				if err != nil || len(queued) != 0 {
					t.Fatal("failed recovery scheduled certificate work")
				}
			})
		}
	}
}

func TestConnectManualRecoveryDoesNotTakeOverConcurrentlyReadySite(t *testing.T) {
	svc, db, dns, uid, pid := connectTestService(t)
	row := domain.Domain{Domain: "www.example.com", ResellerID: pid, Type: domain.DomainTypeCustom,
		Status: domain.DomainStatusActive, VerificationStatus: domain.DomainVerificationVerified, VerificationToken: "manual-proof", TLSStatus: "pending"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	dns.onEnsure = func() {
		if err := db.Model(&domain.Domain{}).Where("id = ?", row.ID).Updates(map[string]any{"tls_status": "ready", "tls_ready_at": now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", contract.DNSCredentials{}); !errors.Is(err, contract.ErrDomainConflict) {
		t.Fatalf("concurrently ready manual site was taken over: %v", err)
	}
	var current domain.Domain
	if err := db.First(&current, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Status != domain.DomainStatusActive || current.TLSStatus != "ready" || current.TLSReadyAt == nil || current.AutoConnectRequestedAt != nil {
		t.Fatal("concurrently ready manual binding was changed")
	}
	var root domain.Domain
	if err := db.Where("domain = ?", "example.com").First(&root).Error; err != nil {
		t.Fatal(err)
	}
	if root.AutoConnectRequestedAt != nil || root.DNSProvider != "" {
		t.Fatal("transaction did not roll back the other domain's queue request")
	}
}

func TestDomainConnectStateDoesNotInventManualProgressOrTLSProof(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name      string
		automatic bool
		tlsStatus string
		readyAt   bool
		wantPhase string
	}{
		{"manual pending", false, "pending", false, "waiting_authorization"},
		{"manual missing timestamp", false, "ready", false, "waiting_authorization"},
		{"manual old failure", false, "failed", false, "waiting_authorization"},
		{"automatic missing timestamp", true, "ready", false, "tls_pending"},
		{"complete automatic proof", true, "ready", true, "active"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := domain.Domain{Status: domain.DomainStatusActive, VerificationStatus: domain.DomainVerificationVerified, TLSStatus: tc.tlsStatus}
			if tc.automatic {
				row.AutoConnectRequestedAt = &now
			}
			if tc.readyAt {
				row.TLSReadyAt = &now
			}
			if result := domainConnectState([]domain.Domain{row, row}); result.Phase != tc.wantPhase {
				t.Fatalf("wrong progress: %s, want %s", result.Phase, tc.wantPhase)
			}
		})
	}
	if result := domainConnectState([]domain.Domain{
		{Status: domain.DomainStatusActive, VerificationStatus: domain.DomainVerificationVerified, TLSStatus: "pending"},
		{Status: domain.DomainStatusDisabled},
	}); result.Phase != "failed" {
		t.Fatal("manual recovery prompt concealed a disabled paired domain")
	}
}

func TestConnectConflictRetryReusesTokensAndRequestsOnlyAfterDNSWrite(t *testing.T) {
	svc, db, dns, uid, _ := connectTestService(t)
	dns.ensureErr = contract.ErrDNSRecordConflict
	if _, err := svc.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", contract.DNSCredentials{}); !errors.Is(err, contract.ErrDNSRecordConflict) {
		t.Fatal(err)
	}
	pending, err := svc.UserDomainConnectStatus(uid, "example.com")
	if err != nil || pending.Phase != "waiting_authorization" {
		t.Fatalf("unexpected state %+v %v", pending, err)
	}
	first := pending.Domains[0].VerificationToken
	if first == pending.Domains[1].VerificationToken {
		t.Fatal("shared ownership token")
	}
	for _, row := range pending.Domains {
		if row.AutoConnectRequestedAt != nil {
			t.Fatal("failed preflight scheduled TLS")
		}
	}
	dns.ensureErr = nil
	result, err := svc.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", contract.DNSCredentials{"api_token": "transient"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Phase != "dns_pending" || len(dns.records) != 4 || result.Domains[0].VerificationToken != first {
		t.Fatalf("bad retry result %+v", result)
	}
	for _, row := range result.Domains {
		if row.AutoConnectRequestedAt == nil || row.Profile != nil || row.DNSProvider != "cloudflare" {
			t.Fatal("missing request or profile leak")
		}
	}
	var count int64
	db.Model(&domain.Domain{}).Count(&count)
	if count != 2 {
		t.Fatal("non-idempotent retry")
	}
}

func TestAutoConnectRequiresLiveDNSAndTrustedTLSBeforeActivation(t *testing.T) {
	svc, db, _, uid, _ := connectTestService(t)
	result, err := svc.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", contract.DNSCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Domains[0]
	dns := fakeDomainDNS{txt: []string{row.VerificationToken}, ips: []net.IPAddr{{IP: net.ParseIP(svc.cfg.DomainEntryIP)}}}
	called := 0
	probe := func(context.Context, string, string, string) error { called++; return errors.New("HTTPS not ready") }
	// A forged stored ready flag must never activate a pending binding.
	db.Model(&domain.Domain{}).Where("id = ?", row.ID).Update("tls_status", "ready")
	row.TLSStatus = "ready"
	if err = svc.progressDomainConnection(context.Background(), &row, dns, probe); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatal("incorrect readiness state")
	}
	db.Model(&domain.Domain{}).Where("id = ?", row.ID).Update("tls_status", "provisioned")
	row.TLSStatus = "provisioned"
	if err = svc.progressDomainConnection(context.Background(), &row, dns, probe); err != nil {
		t.Fatal(err)
	}
	var current domain.Domain
	db.First(&current, row.ID)
	if current.Status != domain.DomainStatusPendingReview || current.TLSReadyAt != nil || current.LastTLSError == "" {
		t.Fatal("failed TLS became active")
	}
	probe = func(_ context.Context, host, ip, token string) error {
		if host != row.Domain || ip != svc.cfg.DomainEntryIP || token != row.VerificationToken {
			t.Fatal("probe inputs not pinned")
		}
		return nil
	}
	wrongDNS := dns
	wrongDNS.txt = []string{"wrong"}
	if err = svc.progressDomainConnection(context.Background(), &row, wrongDNS, probe); err != nil {
		t.Fatal(err)
	}
	db.First(&current, row.ID)
	if current.Status == domain.DomainStatusActive {
		t.Fatal("TLS bypassed DNS proof")
	}
	if err = svc.progressDomainConnection(context.Background(), &row, dns, probe); err != nil {
		t.Fatal(err)
	}
	db.First(&current, row.ID)
	if current.Status != domain.DomainStatusActive || current.TLSStatus != "ready" || current.TLSReadyAt == nil || !current.IsPrimary {
		t.Fatalf("not activated %+v", current)
	}
	if state := domainConnectState([]domain.Domain{current, result.Domains[1]}); state.Phase == "active" {
		t.Fatal("one domain incorrectly marked whole pair active")
	}
}

func TestAutoConnectCancelsActivationAfterDisableOrOwnershipChange(t *testing.T) {
	for _, change := range []string{"disabled", "profile_disabled", "rotated_token", "request_cancelled"} {
		t.Run(change, func(t *testing.T) {
			svc, db, _, uid, pid := connectTestService(t)
			result, err := svc.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", contract.DNSCredentials{})
			if err != nil {
				t.Fatal(err)
			}
			row := result.Domains[0]
			row.TLSStatus = "provisioned"
			db.Model(&domain.Domain{}).Where("id = ?", row.ID).Update("tls_status", "provisioned")
			dns := fakeDomainDNS{txt: []string{row.VerificationToken}, ips: []net.IPAddr{{IP: net.ParseIP(svc.cfg.DomainEntryIP)}}}
			probe := func(context.Context, string, string, string) error {
				switch change {
				case "disabled":
					db.Model(&domain.Domain{}).Where("id = ?", row.ID).Update("status", domain.DomainStatusDisabled)
				case "profile_disabled":
					db.Model(&domain.Profile{}).Where("id = ?", pid).Update("status", domain.ProfileStatusDisabled)
				case "rotated_token":
					db.Model(&domain.Domain{}).Where("id = ?", row.ID).Update("verification_token", "rotated")
				case "request_cancelled":
					db.Model(&domain.Domain{}).Where("id = ?", row.ID).Update("auto_connect_requested_at", nil)
				}
				return nil
			}
			if err = svc.progressDomainConnection(context.Background(), &row, dns, probe); err != nil {
				t.Fatal(err)
			}
			var current domain.Domain
			db.First(&current, row.ID)
			if current.Status == domain.DomainStatusActive || current.TLSReadyAt != nil {
				t.Fatal("race activated revoked domain")
			}
		})
	}
}

func TestAutoConnectQueueOnlySelectsExplicitEligibleRequests(t *testing.T) {
	_, db, _, _, pid := connectTestService(t)
	now := time.Now()
	old := now.Add(-time.Hour)
	for _, row := range []domain.Domain{
		{Domain: "old.example.com", ResellerID: pid, Type: domain.DomainTypeCustom, Status: domain.DomainStatusPendingReview, AutoConnectRequestedAt: &now, LastDNSCheckAt: &old},
		{Domain: "new.example.com", ResellerID: pid, Type: domain.DomainTypeCustom, Status: domain.DomainStatusPendingReview, AutoConnectRequestedAt: &now},
		{Domain: "manual.example.com", ResellerID: pid, Type: domain.DomainTypeCustom, Status: domain.DomainStatusPendingReview},
		{Domain: "disabled.example.com", ResellerID: pid, Type: domain.DomainTypeCustom, Status: domain.DomainStatusDisabled, AutoConnectRequestedAt: &now},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := gormstore.New(db).ListAutoConnectDomains(1)
	if err != nil || len(rows) != 1 || rows[0].Domain != "new.example.com" {
		t.Fatalf("wrong queue selection %+v %v", rows, err)
	}
}
