// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package contract

import (
	apicredentialdomain "github.com/dujiao-next/internal/modules/apicredential/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"time"
)

type ApprovalSettings interface {
	GetByKey(string) (jsonmap.JSON, error)
}

// AutoApprovalRepository atomically checks the account, approves only an eligible
// pending application (or creates its first application), and records the audit.
type AutoApprovalRepository interface {
	AutoApprove(userID uint, key, secret string, createIfMissing bool, now time.Time) (*apicredentialdomain.ApiCredential, bool, error)
	ListPendingUserIDs() ([]uint, error)
}

type PendingApprovalRepository interface {
	ApprovePending(id uint, key, secret string, now time.Time) (*apicredentialdomain.ApiCredential, bool, error)
}
