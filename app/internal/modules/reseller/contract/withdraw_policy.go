// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package contract

import "errors"

// MinimumWithdrawBalanceCNY is a net available wallet balance threshold,
// not a minimum amount for each withdrawal and not an FX conversion rule.
const MinimumWithdrawBalanceCNY = 50

var ErrWithdrawBalanceBelowMinimum = errors.New("reseller CNY available balance below 50")
