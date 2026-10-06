// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package integrationtest

import (
	"bytes"
	"testing"
	"time"
)

func TestAutomaticSettlementPreservesOriginalMetadataBytes(t *testing.T) {
	db, _, svc, _, row, completed := seedAutomaticProfit(t)
	// Legacy imports may use non-canonical whitespace/key order. Reading and
	// updating a deadline must not reserialize their immutable audit snapshot.
	raw := []byte(`{ "z" : "original", "payment_amount" : "125.00", "a" : 1 }`)
	if err := db.Exec("UPDATE reseller_ledger_entries SET metadata_json = ? WHERE id = ?", raw, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, offset := range []time.Duration{time.Hour, 92 * time.Hour} {
		if _, err := svc.ConfirmDueLedgerEntries(completed.Add(offset)); err != nil {
			t.Fatal(err)
		}
		var actual []byte
		if err := db.Raw("SELECT metadata_json FROM reseller_ledger_entries WHERE id = ?", row.ID).Row().Scan(&actual); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, actual) {
			t.Fatal("settlement rewrote the original metadata")
		}
	}
}
