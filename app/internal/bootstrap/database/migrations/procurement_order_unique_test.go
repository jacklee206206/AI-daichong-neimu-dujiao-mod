// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package migrations

import (
	"strings"
	"testing"
)

func TestProcurementDuplicateMigrationFailsWithoutDeletingRows(t *testing.T) {
	db := setupSKUMigrationTestDB(t)
	if err := db.Exec("CREATE TABLE procurement_orders (id INTEGER PRIMARY KEY, local_order_id INTEGER, deleted_at datetime)").Error; err != nil {
		t.Fatal(err)
	}
	// Include a deleted row: deleting a local record never undoes a supplier debit.
	if err := db.Exec("INSERT INTO procurement_orders(id, local_order_id, deleted_at) VALUES (1, 77, NULL), (2, 77, CURRENT_TIMESTAMP)").Error; err != nil {
		t.Fatal(err)
	}
	err := AutoMigrate()
	if err == nil || !strings.Contains(err.Error(), "local order 77 has 2 procurement rows") {
		t.Fatalf("expected descriptive duplicate refusal, got %v", err)
	}
	var count int64
	if err := db.Table("procurement_orders").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("migration removed historical purchases: count=%d", count)
	}
	if db.Migrator().HasTable("admins") {
		t.Fatal("duplicate preflight should run before schema writes")
	}
}
