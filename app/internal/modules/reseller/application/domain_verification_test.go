// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dujiao-next/internal/config"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	resellerstore "github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type fakeDomainDNS struct {
	txt []string
	ips []net.IPAddr
	err error
}

func (f fakeDomainDNS) LookupTXT(context.Context, string) ([]string, error) { return f.txt, f.err }
func (f fakeDomainDNS) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return f.ips, f.err
}
func TestDomainDNSRequiresOwnershipAndOnlyConfiguredEntry(t *testing.T) {
	row := &resellerdomain.Domain{Domain: "store.example.com", VerificationToken: "opengpt-ownership"}
	ip := net.IPAddr{IP: net.ParseIP("8.8.4.4")}
	tests := []struct {
		name     string
		resolver fakeDomainDNS
		entry    string
		ok       bool
	}{
		{"valid", fakeDomainDNS{txt: []string{row.VerificationToken}, ips: []net.IPAddr{ip}}, ip.IP.String(), true},
		{"wrong token", fakeDomainDNS{txt: []string{"other"}, ips: []net.IPAddr{ip}}, ip.IP.String(), false},
		{"wrong A", fakeDomainDNS{txt: []string{row.VerificationToken}, ips: []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}}, ip.IP.String(), false},
		{"extra AAAA", fakeDomainDNS{txt: []string{row.VerificationToken}, ips: []net.IPAddr{ip, {IP: net.ParseIP("2606:4700:4700::1111")}}}, ip.IP.String(), false},
		{"missing A", fakeDomainDNS{txt: []string{row.VerificationToken}}, ip.IP.String(), false},
		{"resolver failure", fakeDomainDNS{err: errors.New("DNS offline")}, ip.IP.String(), false},
		{"private entry", fakeDomainDNS{txt: []string{row.VerificationToken}, ips: []net.IPAddr{ip}}, "127.0.0.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyDomainDNS(context.Background(), row, tt.entry, tt.resolver)
			if (err == nil) != tt.ok {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}
func TestDomainHTTPSProbeRequiresTrustedTLSAndExactToken(t *testing.T) {
	body := "opengpt-correct"
	status := http.StatusOK
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != domainProbePath {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "https://")
	if err := probeDomainHTTPSWithClient(context.Background(), server.Client(), host, "opengpt-correct"); err != nil {
		t.Fatal(err)
	}
	body = "another-domain-token"
	if err := probeDomainHTTPSWithClient(context.Background(), server.Client(), host, "opengpt-correct"); err == nil {
		t.Fatal("wrong token accepted")
	}
	body = "opengpt-correct"
	status = http.StatusNotFound
	if err := probeDomainHTTPSWithClient(context.Background(), server.Client(), host, body); err == nil {
		t.Fatal("wrong HTTP status accepted")
	}
	status = http.StatusOK
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}}
	if err := probeDomainHTTPSWithClient(context.Background(), client, host, body); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
}
func TestUserDomainCheckPersistsDNSOnlyAndIsolatesOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:domain-check?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&userdomain.User{}, &resellerdomain.Profile{}, &resellerdomain.Domain{}); err != nil {
		t.Fatal(err)
	}
	user := userdomain.User{Email: "domain@example.test", PasswordHash: "test"}
	db.Create(&user)
	profile := resellerdomain.Profile{UserID: user.ID, Status: resellerdomain.ProfileStatusActive}
	db.Create(&profile)
	row := resellerdomain.Domain{Domain: "store.example.com", ResellerID: profile.ID, Type: resellerdomain.DomainTypeCustom, Status: resellerdomain.DomainStatusPendingReview, VerificationStatus: resellerdomain.DomainVerificationPending, VerificationToken: "opengpt-proof", TLSStatus: "pending"}
	db.Create(&row)
	svc := NewManagementService(resellerstore.New(db), config.ResellerConfig{DomainEntryIP: "8.8.4.4", DomainVerificationRequired: true})
	if _, err = svc.DomainConnectionSetupForUser(user.ID+1, row.ID); err == nil {
		t.Fatal("other owner saw domain")
	}
	setup, err := svc.verifyUserDomain(context.Background(), user.ID, row.ID, fakeDomainDNS{txt: []string{row.VerificationToken}, ips: []net.IPAddr{{IP: net.ParseIP("8.8.4.4")}}})
	if err != nil {
		t.Fatal(err)
	}
	if setup.Domain.VerificationStatus != resellerdomain.DomainVerificationVerified || setup.Domain.TLSStatus != "pending" || setup.Domain.Status != resellerdomain.DomainStatusPendingReview || setup.Domain.Profile != nil {
		t.Fatalf("DNS check activated or leaked domain: %+v", setup.Domain)
	}
	if len(setup.Records) != 2 || len(setup.Instructions) == 0 {
		t.Fatal("missing executable setup guidance")
	}
	// A forged stored TLS status cannot bypass the live approval check. Invalid
	// configured IP fails before any external DNS/network request in this test.
	now := time.Now().UTC()
	if err := db.Model(&resellerdomain.Domain{}).Where("id = ?", row.ID).Updates(map[string]any{"tls_status": "ready", "tls_ready_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	svc.cfg.DomainEntryIP = "127.0.0.1"
	if _, err := svc.ApproveDomain(context.Background(), 9, row.ID); err == nil {
		t.Fatal("stored ready status bypassed live proof")
	}
	var rejected resellerdomain.Domain
	if err := db.First(&rejected, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if rejected.Status != resellerdomain.DomainStatusPendingReview || rejected.TLSStatus != "pending" || rejected.TLSReadyAt != nil {
		t.Fatalf("unverified domain enabled: %+v", rejected)
	}
}
func TestCustomDomainValidationRejectsTemplateAndURLInjection(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "*.example.com", "x.example.com;", "x.example.com\nserver", "x.example.com@evil.test", "-x.example.com", "x..example.com"} {
		if validCustomDomainHost(host) {
			t.Errorf("accepted %q", host)
		}
	}
	for _, host := range []string{"example.com", "www.example.com", "xn--fiqs8s.example.com"} {
		if !validCustomDomainHost(host) {
			t.Errorf("rejected %q", host)
		}
	}
}

func TestDomainApprovalPreservesManualReviewWithoutTXT(t *testing.T) {
	svc, _, _, uid, _ := connectTestService(t)
	// An invalid entry IP ensures an accidental mandatory verification fails
	// locally instead of making any external DNS or HTTPS request.
	svc.cfg.DomainEntryIP = "127.0.0.1"
	svc.cfg.DomainVerificationRequired = false
	row, err := svc.SubmitUserCustomDomain(uid, "manual.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if row.AutoConnectRequestedAt != nil || row.VerificationStatus != resellerdomain.DomainVerificationPending {
		t.Fatal("manual submission unexpectedly requested automatic connection")
	}
	approved, err := svc.ApproveDomain(context.Background(), 9, row.ID)
	if err != nil {
		t.Fatalf("native manual review required automatic verification: %v", err)
	}
	if approved.Status != resellerdomain.DomainStatusActive || approved.VerificationStatus != resellerdomain.DomainVerificationVerified || approved.VerifiedAt == nil {
		t.Fatalf("manual approval did not activate the domain: %+v", approved)
	}
	if approved.AutoConnectRequestedAt != nil || approved.TLSReadyAt != nil || approved.TLSStatus == "ready" {
		t.Fatal("manual approval fabricated automatic connection or TLS evidence")
	}
}

func TestAutoDomainApprovalRechecksProofWithGlobalVerificationDisabled(t *testing.T) {
	svc, db, _, uid, _ := connectTestService(t)
	svc.cfg.DomainVerificationRequired = false
	result, err := svc.ConnectUserDomain(context.Background(), uid, "example.com", "cloudflare", resellercontract.DNSCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Domains[0]
	now := time.Now().UTC()
	// Stored readiness alone must not let an administrator accidentally bypass
	// the automatic workflow's live proof. The private IP fails without network.
	if err := db.Model(&resellerdomain.Domain{}).Where("id = ?", row.ID).Updates(map[string]any{
		"verification_status": resellerdomain.DomainVerificationVerified,
		"tls_status":          "ready", "tls_ready_at": now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc.cfg.DomainEntryIP = "127.0.0.1"
	if _, err := svc.ApproveDomain(context.Background(), 9, row.ID); !errors.Is(err, resellercontract.ErrDomainStatusInvalid) {
		t.Fatalf("automatic request bypassed live proof: %v", err)
	}
	var current resellerdomain.Domain
	if err := db.First(&current, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Status != resellerdomain.DomainStatusPendingReview || current.TLSStatus != "pending" || current.TLSReadyAt != nil || current.AutoConnectRequestedAt == nil {
		t.Fatalf("unverified automatic request became active: %+v", current)
	}
}

func TestDomainStatusUpdateEnforcesReadinessOnlyWhenRequired(t *testing.T) {
	for _, tc := range []struct {
		name           string
		automatic      bool
		globalRequired bool
		verified       bool
		tlsReady       bool
		readyAt        bool
		allowed        bool
	}{
		{name: "manual native review", allowed: true},
		{name: "manual with global verification", globalRequired: true},
		{name: "auto missing DNS", automatic: true, tlsReady: true, readyAt: true},
		{name: "auto missing TLS", automatic: true, verified: true, readyAt: true},
		{name: "auto missing timestamp", automatic: true, verified: true, tlsReady: true},
		{name: "auto verified and ready", automatic: true, verified: true, tlsReady: true, readyAt: true, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, db, _, _, pid := connectTestService(t)
			svc.cfg.DomainVerificationRequired = tc.globalRequired
			row := resellerdomain.Domain{Domain: "review.example.com", ResellerID: pid, Type: resellerdomain.DomainTypeCustom,
				Status: resellerdomain.DomainStatusPendingReview, VerificationStatus: resellerdomain.DomainVerificationPending, TLSStatus: "pending"}
			now := time.Now().UTC()
			if tc.automatic {
				row.AutoConnectRequestedAt = &now
			}
			if tc.verified {
				row.VerificationStatus = resellerdomain.DomainVerificationVerified
			}
			if tc.tlsReady {
				row.TLSStatus = "ready"
			}
			if tc.readyAt {
				row.TLSReadyAt = &now
			}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			_, err := svc.updateDomainStatus(context.Background(), 9, row.ID, resellerdomain.DomainStatusActive)
			if tc.allowed && err != nil {
				t.Fatalf("permitted approval failed: %v", err)
			}
			if !tc.allowed && !errors.Is(err, resellercontract.ErrDomainStatusInvalid) {
				t.Fatalf("unready domain was not blocked by transactional guard: %v", err)
			}
			var current resellerdomain.Domain
			if err := db.First(&current, row.ID).Error; err != nil {
				t.Fatal(err)
			}
			if (current.Status == resellerdomain.DomainStatusActive) != tc.allowed {
				t.Fatalf("unexpected persisted status: %s", current.Status)
			}
		})
	}
}
