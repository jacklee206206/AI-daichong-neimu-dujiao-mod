// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package architecture

import (
	"path/filepath"
	"testing"
)

func TestResellerModuleOwnsCompleteVerticalSlice(t *testing.T) {
	repositoryRoot := findRepositoryRoot(t)
	moduleRoot := filepath.Join(repositoryRoot, "internal", "modules", "reseller")

	production, total := countDirectGoFiles(t, moduleRoot)
	if production != 0 || total != 0 {
		t.Fatalf("reseller module root must remain structural only, got production=%d total=%d", production, total)
	}

	// Hosted storefront extensions add supply terms, DNS/TLS verification,
	// and delivery-based manual settlement. Keep explicit package bounds.
	domainRoot := filepath.Join(moduleRoot, "domain")
	assertFileDeclaresTypes(t, filepath.Join(domainRoot, "profile.go"), []string{"Profile"})
	assertFileDeclaresTypes(t, filepath.Join(domainRoot, "site.go"), []string{"Domain", "SiteConfig"})
	assertFileDeclaresTypes(t, filepath.Join(domainRoot, "accounting.go"), []string{"LedgerEntry", "WithdrawRequest", "BalanceAccount"})
	assertDirectoryGoFileBudget(t, domainRoot, 8)

	applicationRoot := filepath.Join(moduleRoot, "application")
	assertFileDeclaresTypes(t, filepath.Join(applicationRoot, "management.go"), []string{"ManagementService"})
	assertFileDeclaresTypes(t, filepath.Join(applicationRoot, "accounting_query.go"), []string{"AccountingQueryService"})
	assertFileDeclaresTypes(t, filepath.Join(applicationRoot, "accounting_withdraw.go"), []string{"AccountingWithdrawService"})
	assertFileDeclaresTypes(t, filepath.Join(applicationRoot, "accounting_profit.go"), []string{"AccountingLedgerService"})
	assertDirectoryGoFileBudget(t, applicationRoot, 23)

	storeRoot := filepath.Join(moduleRoot, "infrastructure", "gormstore")
	assertFileDeclaresTypes(t, filepath.Join(storeRoot, "store.go"), []string{"Store"})
	assertFileDeclaresFunctions(t, filepath.Join(storeRoot, "store.go"), []string{"New", "Migrate"})
	assertDirectoryGoFileBudget(t, storeRoot, 21)

	transportRoot := filepath.Join(moduleRoot, "transport", "http")
	assertDirectoryGoFileBudget(t, filepath.Join(transportRoot, "admin"), 10)
	assertDirectoryGoFileBudget(t, filepath.Join(transportRoot, "user"), 10)
	assertDirectoryGoFileBudget(t, filepath.Join(transportRoot, "presenter"), 4)
	assertDirectoryGoFileBudget(t, filepath.Join(transportRoot, "shared"), 1)
}
