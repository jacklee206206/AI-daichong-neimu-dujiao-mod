// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package contract

import (
	"time"

	procurementdomain "github.com/dujiao-next/internal/modules/procurement/domain"
)

// SubmissionRecoveryRepository never returns a purchase whose supplier outcome
// is unknown. Such purchases require reconciliation, not another purchase.
type SubmissionRecoveryRepository interface {
	ListDueSubmissions(now time.Time, afterID uint, limit int) ([]procurementdomain.Order, error)
	MarkStaleSubmissionsUnknown(before, now time.Time) (int64, error)
	ListPaidOrdersMissingProcurement(afterID uint, limit int) ([]uint, error)
}
