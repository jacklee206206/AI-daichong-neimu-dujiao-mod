// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package contract

import "github.com/dujiao-next/internal/shared/mailbrand"

// ApplyPublicConfigTenantMetadata refreshes request-scoped fields after reading a
// tenant-wide config cache. An alias must not publish a previous request's Host,
// and canonical links must follow the current verified primary domain.
func ApplyPublicConfigTenantMetadata(base map[string]interface{}, tenant TenantContext) map[string]interface{} {
	out := make(map[string]interface{}, len(base)+1)
	for key, value := range base {
		out[key] = value
	}
	if !tenant.IsReseller() {
		out["tenant"] = map[string]interface{}{"mode": "main", "host": tenant.Host}
		return out
	}
	out["tenant"] = map[string]interface{}{
		"mode": "reseller", "host": tenant.Host, "primary_domain": tenant.PrimaryDomain,
	}
	brand := map[string]interface{}{}
	if cachedBrand, ok := base["brand"].(map[string]interface{}); ok {
		for key, value := range cachedBrand {
			brand[key] = value
		}
	}
	canonicalHost := tenant.PrimaryDomain
	if canonicalHost == "" {
		canonicalHost = tenant.Host
	}
	brand["site_url"] = mailbrand.ResellerFallback(canonicalHost).SiteURL
	out["brand"] = brand
	return out
}
