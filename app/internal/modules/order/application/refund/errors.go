// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package refund

import orderapp "github.com/dujiao-next/internal/modules/order/application"

var (
	ErrOrderFetchFailed         = orderapp.ErrOrderFetchFailed
	ErrOrderNotFound            = orderapp.ErrOrderNotFound
	ErrOrderRefundExpired       = orderapp.ErrOrderRefundExpired
	ErrOrderRefundScopeConflict = orderapp.ErrOrderRefundScopeConflict
	ErrOrderStatusInvalid       = orderapp.ErrOrderStatusInvalid
	ErrOrderUpdateFailed        = orderapp.ErrOrderUpdateFailed
	ErrRefundRecordCreateFailed = orderapp.ErrRefundRecordCreateFailed
)
