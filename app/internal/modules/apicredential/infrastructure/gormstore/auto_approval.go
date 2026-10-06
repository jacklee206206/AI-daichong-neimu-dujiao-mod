// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package gormstore

import (
	"errors"
	"github.com/dujiao-next/internal/constants"
	apicredentialcontract "github.com/dujiao-next/internal/modules/apicredential/contract"
	apicredentialdomain "github.com/dujiao-next/internal/modules/apicredential/domain"
	auditdomain "github.com/dujiao-next/internal/modules/auditlog/domain"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (r *Store) AutoApprove(userID uint, key, secret string, createIfMissing bool, now time.Time) (*apicredentialdomain.ApiCredential, bool, error) {
	var result *apicredentialdomain.ApiCredential
	approved := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		// Lock the owning account even before a credential exists, so two first
		// applications cannot race to issue different keys for the same account.
		var user userdomain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND deleted_at IS NULL AND status = ?", userID, constants.UserStatusActive).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apicredentialcontract.ErrAutoApprovalIneligible
			}
			return err
		}
		var credential apicredentialdomain.ApiCredential
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&credential).Error
		previousStatus := "none"
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if !createIfMissing {
				return nil
			}
			credential = apicredentialdomain.ApiCredential{UserID: userID}
		case err != nil:
			return err
		default:
			if credential.DeletedAt != nil {
				return apicredentialcontract.ErrAutoApprovalIneligible
			}
			if credential.Status == constants.ApiCredentialStatusApproved {
				result = &credential
				return nil // Includes approved but manually switched off: preserve it.
			}
			if credential.Status != constants.ApiCredentialStatusPendingReview {
				return apicredentialcontract.ErrAutoApprovalIneligible
			}
			previousStatus = credential.Status
		}
		credential.ApiKey = key
		credential.ApiSecret = secret
		credential.Status = constants.ApiCredentialStatusApproved
		credential.IsActive = true
		credential.ApprovedAt = &now
		credential.RejectReason = ""
		credential.LastUsedAt = nil
		credential.UpdatedAt = now
		if err := tx.Omit("User").Save(&credential).Error; err != nil {
			return err
		}
		if err := tx.Create(&auditdomain.AuthzAuditLog{
			OperatorAdminID: 0, OperatorUsername: "system", Action: "api_credential_auto_approve",
			Object: "/api-credential/apply", Method: "SYSTEM", CreatedAt: now,
			DetailJSON: jsonmap.JSON{"source": "system", "policy": "auto_approve_applications", "user_id": userID, "credential_id": credential.ID, "previous_status": previousStatus},
		}).Error; err != nil {
			return err
		}
		result = &credential
		approved = true
		return nil
	})
	return result, approved && err == nil, err
}

func (r *Store) ListPendingUserIDs() ([]uint, error) {
	var ids []uint
	err := r.db.Model(&apicredentialdomain.ApiCredential{}).
		Joins("JOIN users ON users.id = api_credentials.user_id").
		Where("api_credentials.deleted_at IS NULL AND api_credentials.status = ? AND users.deleted_at IS NULL AND users.status = ?", constants.ApiCredentialStatusPendingReview, constants.UserStatusActive).
		Order("api_credentials.id").Pluck("api_credentials.user_id", &ids).Error
	return ids, err
}

var _ apicredentialcontract.AutoApprovalRepository = (*Store)(nil)

// ApprovePending keeps a late manual click from rotating an automatically
// approved credential. Approval is a state transition, not secret regeneration.
func (r *Store) ApprovePending(id uint, key, secret string, now time.Time) (*apicredentialdomain.ApiCredential, bool, error) {
	result := r.db.Model(&apicredentialdomain.ApiCredential{}).
		Where("id = ? AND deleted_at IS NULL AND status = ?", id, constants.ApiCredentialStatusPendingReview).
		Updates(map[string]interface{}{"api_key": key, "api_secret": secret, "status": constants.ApiCredentialStatusApproved, "is_active": true, "approved_at": now, "reject_reason": "", "updated_at": now})
	if result.Error != nil {
		return nil, false, result.Error
	}
	credential, err := r.GetByID(id)
	if err != nil {
		return nil, false, err
	}
	if credential == nil {
		return nil, false, apicredentialcontract.ErrNotFound
	}
	if credential.Status != constants.ApiCredentialStatusApproved {
		return nil, false, apicredentialcontract.ErrNotApproved
	}
	return credential, result.RowsAffected == 1, nil
}
