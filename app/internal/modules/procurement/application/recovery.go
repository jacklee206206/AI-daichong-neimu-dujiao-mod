// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"errors"
	"time"

	"github.com/dujiao-next/internal/logger"
	procurementcontract "github.com/dujiao-next/internal/modules/procurement/contract"
)

// recoverSubmissionTasks repairs a lost Redis enqueue from durable DB state.
// Claims older than the 30-second HTTP timeout are quarantined, never retried.
func (s *Service) recoverSubmissionTasks() {
	recovery, ok := s.procRepo.(procurementcontract.SubmissionRecoveryRepository)
	if !ok {
		return
	}
	now := time.Now()
	count, err := recovery.MarkStaleSubmissionsUnknown(now.Add(-5*time.Minute), now)
	if err != nil {
		logger.Warnw("procurement_recovery_stale_claim_failed", "error", err)
	} else if count > 0 {
		logger.Warnw("procurement_recovery_requires_reconciliation", "count", count)
	}
	if s.queue == nil {
		return
	}
	var afterID uint
	for {
		orders, err := recovery.ListDueSubmissions(now, afterID, 200)
		if err != nil {
			logger.Warnw("procurement_recovery_list_failed", "error", err)
			return
		}
		for i := range orders {
			if err := s.queue.EnqueueSubmit(orders[i].ID); err != nil {
				logger.Warnw("procurement_recovery_enqueue_failed", "procurement_order_id", orders[i].ID, "error", err)
				// Leave the durable pending/failed row for the next recovery run.
				return
			}
			afterID = orders[i].ID
		}
		if len(orders) < 200 {
			break
		}
	}
	s.recoverMissingProcurements(recovery)
}

func (s *Service) recoverMissingProcurements(recovery procurementcontract.SubmissionRecoveryRepository) {
	var afterID uint
	for {
		ids, err := recovery.ListPaidOrdersMissingProcurement(afterID, 200)
		if err != nil {
			logger.Warnw("procurement_missing_list_failed", "error", err)
			return
		}
		for _, id := range ids {
			// Creation retains the unique local_order_id barrier. Submission then
			// atomically rechecks the current order state before calling upstream.
			if err := s.CreateForOrder(id); err != nil && !errors.Is(err, procurementcontract.ErrExists) {
				logger.Warnw("procurement_missing_create_failed", "local_order_id", id, "error", err)
			}
			afterID = id
		}
		if len(ids) < 200 {
			return
		}
	}
}
