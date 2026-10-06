// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package userhttp

import (
	"context"
	"errors"
	"net/http"

	app "github.com/dujiao-next/internal/modules/reseller/application"
	contract "github.com/dujiao-next/internal/modules/reseller/contract"
	dto "github.com/dujiao-next/internal/modules/reseller/transport/http/presenter"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"
	"github.com/gin-gonic/gin"
)

type domainConnectService interface {
	InspectDomainConnect(context.Context, uint, string) (*app.DomainConnectInspection, error)
	ConnectUserDomain(context.Context, uint, string, string, contract.DNSCredentials) (*app.DomainConnectResult, error)
	UserDomainConnectStatus(uint, string) (*app.DomainConnectResult, error)
}

func (h *UserHandler) InspectDomainConnect(c *gin.Context) { h.domainConnect(c, "inspect") }
func (h *UserHandler) ConnectDomain(c *gin.Context)        { h.domainConnect(c, "connect") }
func (h *UserHandler) RestartDomainConnect(c *gin.Context) { h.domainConnect(c, "restart") }
func (h *UserHandler) DomainConnectStatus(c *gin.Context)  { h.domainConnect(c, "status") }
func (h *UserHandler) domainConnect(c *gin.Context, action string) {
	c.Header("Cache-Control", "no-store")
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	svc, ok := h.management.(domainConnectService)
	if !ok {
		ginutil.RespondError(c, response.CodeInternal, "error.save_failed", nil)
		return
	}
	input := struct {
		Domain         string                  `json:"domain"`
		ConfirmRestart bool                    `json:"confirm_restart"`
		Provider       string                  `json:"provider"`
		Credentials    contract.DNSCredentials `json:"credentials"`
	}{}
	if action == "status" {
		input.Domain = c.Query("domain")
	} else {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
		if c.ShouldBindJSON(&input) != nil {
			ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
			return
		}
	}
	defer clear(input.Credentials)
	if action == "inspect" {
		result, err := svc.InspectDomainConnect(c.Request.Context(), uid, input.Domain)
		if err != nil {
			respondDomainConnectError(c, err)
			return
		}
		response.Success(c, result)
		return
	}
	var result *app.DomainConnectResult
	var err error
	if action == "restart" {
		if !input.ConfirmRestart {
			ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
			return
		}
		restart, ok := h.management.(interface {
			RestartUserDomainConnect(context.Context, uint, string) (*app.DomainConnectResult, error)
		})
		if !ok {
			ginutil.RespondError(c, response.CodeInternal, "error.save_failed", nil)
			return
		}
		result, err = restart.RestartUserDomainConnect(c.Request.Context(), uid, input.Domain)
	} else if action == "connect" {
		result, err = svc.ConnectUserDomain(c.Request.Context(), uid, input.Domain, input.Provider, input.Credentials)
	} else {
		result, err = svc.UserDomainConnectStatus(uid, input.Domain)
	}
	if err != nil {
		respondDomainConnectError(c, err)
		return
	}
	response.Success(c, gin.H{"domains": dto.NewResellerDomainRespList(result.Domains), "phase": result.Phase, "message": result.Message})
}

func respondDomainConnectError(c *gin.Context, err error) {
	// Never include provider responses, request bodies or credentials in logs.
	for _, safe := range []error{app.ErrDomainAutoConnectUnavailable, app.ErrDomainRootRequired, app.ErrDomainConnectBusy, app.ErrDomainLimit,
		contract.ErrSaaSUnavailable, contract.ErrSaaSRequest, contract.ErrSaaSConflict, contract.ErrDNSProviderUnsupported, contract.ErrDNSCredentialsInvalid, contract.ErrDNSLookupFailed, contract.ErrDNSProviderMismatch, contract.ErrDNSRecordConflict, contract.ErrDNSProviderRequest, contract.ErrDNSRecordInvalid} {
		if errors.Is(err, safe) {
			ginutil.RespondErrorWithMsg(c, response.CodeBadRequest, safe.Error(), nil)
			return
		}
	}
	for _, rule := range userManagementErrorRules {
		if errors.Is(err, rule.target) {
			ginutil.RespondError(c, rule.code, rule.key, nil)
			return
		}
	}
	ginutil.RespondError(c, response.CodeInternal, "error.save_failed", nil)
}
