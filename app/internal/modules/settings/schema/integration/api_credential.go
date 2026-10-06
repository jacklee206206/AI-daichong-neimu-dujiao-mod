// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package settingsintegration

import (
	settingsvalue "github.com/dujiao-next/internal/modules/settings/schema/value"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// API credential approval is separate from reseller, domain and finance approval.
type ApiCredentialSetting struct {
	AutoApproveApplications bool `json:"auto_approve_applications"`
}

func DecodeApiCredentialSetting(value jsonmap.JSON) ApiCredentialSetting {
	return ApiCredentialSetting{AutoApproveApplications: settingsvalue.ReadBool(value, "auto_approve_applications", false)}
}

func NormalizeApiCredentialSettingJSON(value jsonmap.JSON) jsonmap.JSON {
	return jsonmap.JSON{"auto_approve_applications": DecodeApiCredentialSetting(value).AutoApproveApplications}
}
