// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package contract

import "errors"

var (
	ErrExists                 = errors.New("api credential already exists for this user")
	ErrNotFound               = errors.New("api credential not found")
	ErrNotApproved            = errors.New("api credential is not approved")
	ErrPendingExist           = errors.New("pending application already exists")
	ErrAutoApprovalIneligible = errors.New("account or credential is not eligible for automatic API approval")
)
