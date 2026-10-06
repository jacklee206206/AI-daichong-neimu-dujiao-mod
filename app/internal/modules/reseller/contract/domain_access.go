// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package contract

import resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

// DomainPublicEligible is shared by alias resolution and canonical selection.
// Historical manually activated domains retain their legacy behavior; managed
// connections must have the same TLS proof required by their activation flow.
func DomainPublicEligible(row *resellerdomain.Domain) bool {
	if row == nil || row.ResellerID == 0 || row.Domain == "" || row.DeletedAt != nil ||
		row.Status != resellerdomain.DomainStatusActive || row.VerificationStatus != resellerdomain.DomainVerificationVerified || row.TLSStatus == "failed" {
		return false
	}
	if row.ConnectMode == DomainConnectCloudflareSaaS || row.AutoConnectRequestedAt != nil {
		if row.TLSStatus != "ready" || row.TLSReadyAt == nil {
			return false
		}
	}
	return row.ConnectMode != DomainConnectCloudflareSaaS ||
		(row.CloudflareHostnameID != "" && row.CloudflareHostnameStatus == "active" && row.CloudflareSSLStatus == "active")
}
