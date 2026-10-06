// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package migrations

import (
	"fmt"

	"gorm.io/gorm"
)

// Refuse an ambiguous historical migration. Never choose a winning purchase or
// delete another purchase: both may represent a real debit at the supplier.
func ensureNoDuplicateProcurementOrders(db *gorm.DB) error {
	if !db.Migrator().HasTable("procurement_orders") {
		return nil
	}
	var duplicate struct {
		LocalOrderID uint
		Count        int64
	}
	if err := db.Table("procurement_orders").
		Select("local_order_id, COUNT(*) AS count").
		Group("local_order_id").Having("COUNT(*) > 1").
		Order("local_order_id ASC").Limit(1).Scan(&duplicate).Error; err != nil {
		return fmt.Errorf("check procurement uniqueness: %w", err)
	}
	if duplicate.Count > 1 {
		return fmt.Errorf("procurement uniqueness migration stopped: local order %d has %d procurement rows; reconcile them before retrying, no rows were removed", duplicate.LocalOrderID, duplicate.Count)
	}
	return nil
}
