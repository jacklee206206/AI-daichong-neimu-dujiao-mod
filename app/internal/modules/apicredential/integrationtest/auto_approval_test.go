// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package integrationtest

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	apicredentialapp "github.com/dujiao-next/internal/modules/apicredential/application"
	apicredentialcontract "github.com/dujiao-next/internal/modules/apicredential/contract"
	apicredentialdomain "github.com/dujiao-next/internal/modules/apicredential/domain"
	apicredentialgormstore "github.com/dujiao-next/internal/modules/apicredential/infrastructure/gormstore"
	auditdomain "github.com/dujiao-next/internal/modules/auditlog/domain"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"gorm.io/gorm"
)

type approvalPolicy struct {
	enabled bool
	err     error
}

func (p *approvalPolicy) GetByKey(key string) (jsonmap.JSON, error) {
	if key != constants.SettingKeyApiCredentialConfig {
		return nil, fmt.Errorf("unexpected setting %s", key)
	}
	return jsonmap.JSON{"auto_approve_applications": p.enabled}, p.err
}

func setupAutomaticAPIApproval(t *testing.T) (*apicredentialapp.Service, apicredentialcontract.Repository, *gorm.DB, *approvalPolicy) {
	t.Helper()
	_, repo, db := setupApiCredentialServiceTest(t)
	if err := db.AutoMigrate(&userdomain.User{}, &auditdomain.AuthzAuditLog{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	policy := &approvalPolicy{enabled: true}
	return apicredentialapp.NewService(repo, policy), repo, db, policy
}

func seedAPIApprovalUser(t *testing.T, db *gorm.DB, id uint) {
	t.Helper()
	if err := db.Create(&userdomain.User{ID: id, Email: fmt.Sprintf("api%d@example.test", id), PasswordHash: "fixture", Status: constants.UserStatusActive}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestAPIApprovalAutomaticIsConcurrentIdempotentAndAudited(t *testing.T) {
	svc, repo, db, _ := setupAutomaticAPIApproval(t)
	seedAPIApprovalUser(t, db, 1)
	var wg sync.WaitGroup
	results := make(chan *apicredentialdomain.ApiCredential, 8)
	errors := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := svc.Apply(1)
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	stored, err := repo.GetByUserID(1)
	if err != nil || stored == nil {
		t.Fatalf("stored=%v err=%v", stored, err)
	}
	if stored.Status != constants.ApiCredentialStatusApproved || !stored.IsActive || stored.ApprovedAt == nil || len(stored.ApiKey) != 64 || len(stored.ApiSecret) != 128 {
		t.Fatalf("invalid automatic credential status=%s", stored.Status)
	}
	for result := range results {
		if result.ID != stored.ID || result.ApiKey != stored.ApiKey || result.ApiSecret != stored.ApiSecret {
			t.Fatal("repeat apply rotated a credential")
		}
	}
	var audits []auditdomain.AuthzAuditLog
	if err := db.Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 1 || audits[0].Action != "api_credential_auto_approve" || audits[0].OperatorAdminID != 0 || audits[0].DetailJSON["source"] != "system" {
		t.Fatalf("wrong system audit: %+v", audits)
	}
	if _, found := audits[0].DetailJSON["api_secret"]; found {
		t.Fatal("audit leaked secret")
	}
	_, secret, err := svc.Approve(stored.ID)
	if err != nil || secret != "" {
		t.Fatalf("repeat manual approval: secret returned=%t err=%v", secret != "", err)
	}
	after, _ := repo.GetByUserID(1)
	if after.ApiSecret != stored.ApiSecret || after.ApiKey != stored.ApiKey {
		t.Fatal("late manual approval rotated automatic credential")
	}
}

func TestAPIApprovalPendingBatchOnlyApprovesEligibleAccounts(t *testing.T) {
	svc, repo, db, _ := setupAutomaticAPIApproval(t)
	for id := uint(1); id <= 7; id++ {
		seedAPIApprovalUser(t, db, id)
		status := constants.ApiCredentialStatusPendingReview
		if id == 3 {
			status = constants.ApiCredentialStatusRejected
		}
		if id == 4 {
			status = constants.ApiCredentialStatusDisabled
		}
		if id == 7 {
			status = constants.ApiCredentialStatusApproved
		}
		cred := &apicredentialdomain.ApiCredential{UserID: id, ApiKey: fmt.Sprint("existing-key-", id), ApiSecret: "existing-secret", Status: status, IsActive: false}
		if err := repo.Create(cred); err != nil {
			t.Fatal(err)
		}
		if id == 5 {
			if err := repo.Delete(cred.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.Model(&userdomain.User{}).Where("id = ?", 2).Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&userdomain.User{}).Where("id = ?", 6).Update("deleted_at", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	count, err := svc.AutoApprovePending()
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	count, err = svc.AutoApprovePending()
	if err != nil || count != 0 {
		t.Fatalf("repeat count=%d err=%v", count, err)
	}
	for id := uint(2); id <= 7; id++ {
		_, err := svc.Apply(id)
		if id == 7 {
			if err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, apicredentialcontract.ErrAutoApprovalIneligible) {
			t.Fatalf("user %d should not be auto restored: %v", id, err)
		}
		cred, _ := repo.GetAnyByUserID(id)
		if cred.IsActive || cred.ApiKey != fmt.Sprint("existing-key-", id) || cred.ApiSecret != "existing-secret" {
			t.Fatalf("protected credential %d changed", id)
		}
	}
}

func TestAPIApprovalPolicySwitchAndLazyPendingRecovery(t *testing.T) {
	svc, repo, db, policy := setupAutomaticAPIApproval(t)
	seedAPIApprovalUser(t, db, 1)
	seedAPIApprovalUser(t, db, 2)
	policy.enabled = false
	pending, err := svc.Apply(1)
	if err != nil || pending.Status != constants.ApiCredentialStatusPendingReview {
		t.Fatalf("manual mode: %+v %v", pending, err)
	}
	policy.enabled = true
	approved, err := svc.GetByUserID(1)
	if err != nil || approved.ID != pending.ID || approved.Status != constants.ApiCredentialStatusApproved {
		t.Fatalf("pending recovery: %+v %v", approved, err)
	}
	policy.enabled = false
	other, err := svc.Apply(2)
	if err != nil || other.Status != constants.ApiCredentialStatusPendingReview {
		t.Fatalf("manual mode restored: %+v %v", other, err)
	}
	if n, err := svc.AutoApprovePending(); err != nil || n != 0 {
		t.Fatalf("disabled policy batch=%d %v", n, err)
	}
	stored, _ := repo.GetByUserID(1)
	if stored.ApiSecret != approved.ApiSecret || !stored.IsActive {
		t.Fatal("switching to manual changed previously granted credentials")
	}
}

func TestAPIApprovalFailsClosedOnPolicyOrAuditFailure(t *testing.T) {
	svc, repo, db, policy := setupAutomaticAPIApproval(t)
	seedAPIApprovalUser(t, db, 1)
	policy.err = errors.New("settings unavailable")
	if _, err := svc.Apply(1); err == nil {
		t.Fatal("settings failure should not grant permission")
	}
	policy.err = nil
	if err := db.Migrator().DropTable(&auditdomain.AuthzAuditLog{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(1); err == nil {
		t.Fatal("audit failure should roll back approval")
	}
	if stored, err := repo.GetByUserID(1); err != nil || stored != nil {
		t.Fatalf("partial permission grant survived rollback: %+v %v", stored, err)
	}
	if _, err := svc.Apply(0); !errors.Is(err, apicredentialcontract.ErrAutoApprovalIneligible) {
		t.Fatalf("invalid account: %v", err)
	}
}

func TestAPIApprovalManualCASCannotRotateAutomaticCredential(t *testing.T) {
	svc, repo, db, _ := setupAutomaticAPIApproval(t)
	seedAPIApprovalUser(t, db, 1)
	approved, err := svc.Apply(1)
	if err != nil {
		t.Fatal(err)
	}
	stored, changed, err := apicredentialgormstore.New(db).ApprovePending(approved.ID, "late-key", "late-secret", time.Now())
	if err != nil || changed || stored.ApiKey != approved.ApiKey || stored.ApiSecret != approved.ApiSecret {
		t.Fatal("stale manual approval rotated the active credential")
	}
	_ = repo
}
