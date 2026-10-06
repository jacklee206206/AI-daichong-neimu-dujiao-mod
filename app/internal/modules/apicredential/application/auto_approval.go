// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"errors"
	"github.com/dujiao-next/internal/constants"
	apicredentialcontract "github.com/dujiao-next/internal/modules/apicredential/contract"
	apicredentialdomain "github.com/dujiao-next/internal/modules/apicredential/domain"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
	"time"
)

func (s *Service) automaticApprovalEnabled() (bool, error) {
	if s.approvalSettings == nil {
		return false, nil
	}
	value, err := s.approvalSettings.GetByKey(constants.SettingKeyApiCredentialConfig)
	if err != nil {
		return false, err
	}
	return settingsintegration.DecodeApiCredentialSetting(value).AutoApproveApplications, nil
}

func (s *Service) autoApprove(userID uint, createIfMissing bool) (*apicredentialdomain.ApiCredential, bool, error) {
	repo, ok := s.credRepo.(apicredentialcontract.AutoApprovalRepository)
	if !ok {
		return nil, false, errors.New("API approval transaction capability unavailable")
	}
	if userID == 0 {
		return nil, false, apicredentialcontract.ErrAutoApprovalIneligible
	}
	key, err := generateRandomHex(32)
	if err != nil {
		return nil, false, err
	}
	secret, err := generateRandomHex(64)
	if err != nil {
		return nil, false, err
	}
	return repo.AutoApprove(userID, key, secret, createIfMissing, time.Now())
}

// AutoApprovePending is run after enabling the policy. Repeating it is safe:
// only active accounts' pending rows can transition, and each transition is audited.
func (s *Service) AutoApprovePending() (int, error) {
	automatic, err := s.automaticApprovalEnabled()
	if err != nil || !automatic {
		return 0, err
	}
	repo, ok := s.credRepo.(apicredentialcontract.AutoApprovalRepository)
	if !ok {
		return 0, errors.New("API approval transaction capability unavailable")
	}
	userIDs, err := repo.ListPendingUserIDs()
	if err != nil {
		return 0, err
	}
	approvedCount := 0
	for _, userID := range userIDs {
		_, approved, err := s.autoApprove(userID, false)
		if errors.Is(err, apicredentialcontract.ErrAutoApprovalIneligible) {
			continue
		}
		if err != nil {
			return approvedCount, err
		}
		if approved {
			approvedCount++
		}
	}
	return approvedCount, nil
}
