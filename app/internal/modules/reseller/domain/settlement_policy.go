// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package domain

import "time"

// SettlementHold starts after actual delivery of every order item.
const SettlementHold = 92 * time.Hour
