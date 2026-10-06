// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package userhttp

import (
	"context"
	resellerapp "github.com/dujiao-next/internal/modules/reseller/application"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"
	"github.com/gin-gonic/gin"
)

type domainVerificationService interface {
	DomainConnectionSetupForUser(uint, uint) (*resellerapp.DomainConnectionSetup, error)
	VerifyUserDomain(context.Context, uint, uint) (*resellerapp.DomainConnectionSetup, error)
}

func (h *UserHandler) DomainSetup(c *gin.Context)  { h.domainConnection(c, false) }
func (h *UserHandler) VerifyDomain(c *gin.Context) { h.domainConnection(c, true) }
func (h *UserHandler) domainConnection(c *gin.Context, verify bool) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	id, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}
	svc, ok := h.management.(domainVerificationService)
	if !ok {
		ginutil.RespondError(c, response.CodeInternal, "error.save_failed", nil)
		return
	}
	var result *resellerapp.DomainConnectionSetup
	if verify {
		result, err = svc.VerifyUserDomain(c.Request.Context(), uid, id)
	} else {
		result, err = svc.DomainConnectionSetupForUser(uid, id)
	}
	if err != nil {
		respondUserManagementError(c, err, "error.save_failed")
		return
	}
	response.Success(c, result)
}
