// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package contract

import "time"

// SubmissionRepository provides database-level guards around a chargeable
// upstream request. A process-local mutex cannot protect against other workers.
type SubmissionRepository interface {
	ClaimSubmission(id uint, now time.Time) (bool, error)
	CompareAndSwapStatus(id uint, expected []string, status string, updates map[string]interface{}) (bool, error)
	RecordSubmissionAccepted(id uint, result *CreateOrderResult, now time.Time) (bool, error)
	UpdateLocalStatusIf(id uint, expected, status string, now time.Time) (bool, error)
}
