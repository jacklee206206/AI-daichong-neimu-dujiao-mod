// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package adminhttp

import (
	"errors"
	"strings"
	"time"

	auditlogapp "github.com/dujiao-next/internal/modules/auditlog/application"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"

	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"

	resellermodule "github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// AdminFinanceQueryService 是后台分销财务查询端点所需的最小用例接口。
type AdminFinanceQueryService interface {
	ListAdminLedgerEntries(filter resellermodule.AdminLedgerListFilter) ([]resellerdomain.LedgerEntry, int64, error)
	ListAdminBalanceAccounts(filter resellermodule.AdminBalanceAccountListFilter) ([]resellerdomain.BalanceAccount, int64, error)
	ListAdminWithdrawRequests(filter resellermodule.AdminWithdrawListFilter) ([]resellerdomain.WithdrawRequest, int64, error)
}

// AdminWithdrawReviewService 是后台提现审核写操作所需的最小用例接口。
type AdminWithdrawReviewService interface {
	ReviewWithdraw(adminID, withdrawID uint, action, reason string) (*resellerdomain.WithdrawRequest, error)
}

// AdminFinanceHandler 处理后台分销财务请求。
type AdminProfitConfirmationService interface {
	ConfirmOrderProfit(adminID, entryID uint, reason string) (*resellerdomain.LedgerEntry, error)
}

type AdminFinanceHandler struct {
	confirmer AdminProfitConfirmationService
	audit     AuditRecorder
	query     AdminFinanceQueryService
	reviewer  AdminWithdrawReviewService
}

func NewAdminFinanceHandler(query AdminFinanceQueryService, reviewer AdminWithdrawReviewService) *AdminFinanceHandler {
	if query == nil {
		panic("reseller admin finance handler: query is nil")
	}
	if reviewer == nil {
		panic("reseller admin finance handler: reviewer is nil")
	}
	return &AdminFinanceHandler{query: query, reviewer: reviewer}
}

func NewAdminFinanceHandlerWithConfirmation(query AdminFinanceQueryService, reviewer AdminWithdrawReviewService, confirmer AdminProfitConfirmationService, audit AuditRecorder) *AdminFinanceHandler {
	h := NewAdminFinanceHandler(query, reviewer)
	if confirmer == nil {
		panic("reseller admin finance handler: confirmer is nil")
	}
	h.confirmer = confirmer
	h.audit = audit
	return h
}

func (h *AdminFinanceHandler) ConfirmLedgerEntry(c *gin.Context) {
	adminID, ok := ginutil.GetAdminID(c)
	if !ok {
		return
	}
	id, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}
	var req reviewWithdrawRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	if h.confirmer == nil {
		ginutil.RespondError(c, response.CodeInternal, "error.save_failed", nil)
		return
	}
	row, err := h.confirmer.ConfirmOrderProfit(adminID, id, req.Reason)
	if err != nil {
		switch {
		case errors.Is(err, resellermodule.ErrLedgerNotReady):
			ginutil.RespondError(c, response.CodeBadRequest, "error.reseller_profit_not_ready", nil)
		case errors.Is(err, resellermodule.ErrLedgerReviewReasonRequired):
			ginutil.RespondError(c, response.CodeBadRequest, "error.reseller_profit_reason_required", nil)
		case errors.Is(err, resellermodule.ErrLedgerReviewInvalid), errors.Is(err, resellermodule.ErrProfileInactive), errors.Is(err, resellermodule.ErrSettlementUnavailable), errors.Is(err, resellermodule.ErrBalanceAccountFrozen), errors.Is(err, resellermodule.ErrNotOpened):
			ginutil.RespondError(c, response.CodeBadRequest, "error.reseller_profit_review_invalid", nil)
		default:
			ginutil.RespondError(c, response.CodeInternal, "error.save_failed", err)
		}
		return
	}
	if h.audit != nil {
		_ = h.audit.Record(auditlogapp.AuthzRecord{
			OperatorAdminID: adminID, OperatorUsername: c.GetString("username"),
			Action: "reseller_profit_confirm", Object: "/admin/resellers/ledger-entries/:id/confirm", Method: "POST",
			RequestID: strings.TrimSpace(c.GetString("request_id")),
			Detail:    jsonmap.JSON{"ledger_entry_id": row.ID, "reseller_id": row.ResellerID, "order_id": row.OrderID, "reason": row.ConfirmationReason},
		})
	}
	response.Success(c, row)
}

type reviewWithdrawRequest struct {
	Reason string `json:"reason"`
}

// ListLedgerEntries 管理端分销账务流水列表。
func (h *AdminFinanceHandler) ListLedgerEntries(c *gin.Context) {
	page, pageSize := ginutil.ParsePagination(c)
	resellerID, _ := ginutil.ParseQueryUint(c.Query("reseller_id"), false)
	userID, _ := ginutil.ParseQueryUint(c.Query("user_id"), false)
	orderID, _ := ginutil.ParseQueryUint(c.Query("order_id"), false)
	rows, total, err := h.query.ListAdminLedgerEntries(resellermodule.AdminLedgerListFilter{
		Page:        page,
		PageSize:    pageSize,
		ResellerID:  resellerID,
		UserID:      userID,
		Keyword:     strings.TrimSpace(c.Query("keyword")),
		Currency:    strings.TrimSpace(c.Query("currency")),
		Type:        strings.TrimSpace(c.Query("type")),
		Status:      strings.TrimSpace(c.Query("status")),
		OrderID:     orderID,
		OrderNo:     strings.TrimSpace(c.Query("order_no")),
		CreatedFrom: parseFinanceTimePointer(c.Query("created_from")),
		CreatedTo:   parseFinanceTimePointer(c.Query("created_to")),
	})
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.user_fetch_failed", err)
		return
	}
	response.SuccessWithPage(c, rows, response.BuildPagination(page, pageSize, total))
}

// ListBalanceAccounts 管理端分销余额账户列表。
func (h *AdminFinanceHandler) ListBalanceAccounts(c *gin.Context) {
	page, pageSize := ginutil.ParsePagination(c)
	resellerID, _ := ginutil.ParseQueryUint(c.Query("reseller_id"), false)
	userID, _ := ginutil.ParseQueryUint(c.Query("user_id"), false)
	rows, total, err := h.query.ListAdminBalanceAccounts(resellermodule.AdminBalanceAccountListFilter{
		Page:       page,
		PageSize:   pageSize,
		ResellerID: resellerID,
		UserID:     userID,
		Keyword:    strings.TrimSpace(c.Query("keyword")),
		Currency:   strings.TrimSpace(c.Query("currency")),
		Status:     strings.TrimSpace(c.Query("status")),
	})
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.user_fetch_failed", err)
		return
	}
	response.SuccessWithPage(c, rows, response.BuildPagination(page, pageSize, total))
}

// ListWithdraws 管理端分销提现申请列表。
func (h *AdminFinanceHandler) ListWithdraws(c *gin.Context) {
	page, pageSize := ginutil.ParsePagination(c)
	resellerID, _ := ginutil.ParseQueryUint(c.Query("reseller_id"), false)
	userID, _ := ginutil.ParseQueryUint(c.Query("user_id"), false)
	rows, total, err := h.query.ListAdminWithdrawRequests(resellermodule.AdminWithdrawListFilter{
		Page:        page,
		PageSize:    pageSize,
		ResellerID:  resellerID,
		UserID:      userID,
		Keyword:     strings.TrimSpace(c.Query("keyword")),
		Currency:    strings.TrimSpace(c.Query("currency")),
		Status:      strings.TrimSpace(c.Query("status")),
		CreatedFrom: parseFinanceTimePointer(c.Query("created_from")),
		CreatedTo:   parseFinanceTimePointer(c.Query("created_to")),
	})
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.user_fetch_failed", err)
		return
	}
	response.SuccessWithPage(c, rows, response.BuildPagination(page, pageSize, total))
}

// RejectWithdraw 拒绝分销提现申请。
func (h *AdminFinanceHandler) RejectWithdraw(c *gin.Context) {
	adminID, ok := ginutil.GetAdminID(c)
	if !ok {
		return
	}
	id, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}
	var req reviewWithdrawRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	row, err := h.reviewer.ReviewWithdraw(adminID, id, "reject", req.Reason)
	if err != nil {
		respondAdminWithdrawReviewError(c, err)
		return
	}
	response.Success(c, row)
}

// PayWithdraw 标记分销提现已打款。
func (h *AdminFinanceHandler) PayWithdraw(c *gin.Context) {
	adminID, ok := ginutil.GetAdminID(c)
	if !ok {
		return
	}
	id, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}
	row, err := h.reviewer.ReviewWithdraw(adminID, id, "pay", "")
	if err != nil {
		respondAdminWithdrawReviewError(c, err)
		return
	}
	response.Success(c, row)
}

func respondAdminWithdrawReviewError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, productcontract.ErrNotFound):
		ginutil.RespondError(c, response.CodeNotFound, "error.bad_request", nil)
	case errors.Is(err, resellermodule.ErrWithdrawStatusInvalid), errors.Is(err, resellermodule.ErrBalanceAccountFrozen), errors.Is(err, resellermodule.ErrWithdrawInsufficient), errors.Is(err, resellermodule.ErrProfileInactive), errors.Is(err, resellermodule.ErrSettlementUnavailable):
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
	default:
		ginutil.RespondError(c, response.CodeInternal, "error.save_failed", err)
	}
}

func parseFinanceTimePointer(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t
	}
	return nil
}
