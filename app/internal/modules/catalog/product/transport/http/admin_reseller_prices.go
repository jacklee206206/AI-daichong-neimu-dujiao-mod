// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package producthttp

import (
	"errors"
	"regexp"
	"strings"

	productwrite "github.com/dujiao-next/internal/modules/catalog/product/application/write"
	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

var resellerPriceAmountPattern = regexp.MustCompile(`^[0-9]{1,18}(\.[0-9]{1,2})?$`)

type updateResellerPricesRequest struct {
	SKUs []struct {
		ID      uint    `json:"id"`
		Supply  *string `json:"reseller_supply_price_amount"`
		Minimum *string `json:"reseller_min_price_amount"`
	} `json:"skus"`
}

func (h *AdminProductHandler) UpdateResellerPrices(c *gin.Context) {
	var req updateResellerPricesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	if len(req.SKUs) == 0 || len(req.SKUs) > 500 {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}
	inputs := make([]productwrite.ResellerPriceInput, 0, len(req.SKUs))
	for _, sku := range req.SKUs {
		if sku.Supply == nil || sku.Minimum == nil || !resellerPriceAmountPattern.MatchString(strings.TrimSpace(*sku.Supply)) || !resellerPriceAmountPattern.MatchString(strings.TrimSpace(*sku.Minimum)) {
			ginutil.RespondError(c, response.CodeBadRequest, "error.product_price_invalid", nil)
			return
		}
		supply, _ := decimal.NewFromString(strings.TrimSpace(*sku.Supply))
		minimum, _ := decimal.NewFromString(strings.TrimSpace(*sku.Minimum))
		inputs = append(inputs, productwrite.ResellerPriceInput{ID: sku.ID, SupplyPriceAmount: supply, MinPriceAmount: minimum})
	}
	if err := h.writer.UpdateResellerPrices(c.Param("id"), inputs); err != nil {
		switch {
		case errors.Is(err, productcontract.ErrNotFound):
			ginutil.RespondError(c, response.CodeNotFound, "error.product_not_found", nil)
		case errors.Is(err, productcontract.ErrProductPriceInvalid), errors.Is(err, productcontract.ErrProductSKUInvalid):
			ginutil.RespondError(c, response.CodeBadRequest, "error.product_price_invalid", nil)
		default:
			ginutil.RespondError(c, response.CodeInternal, "error.product_update_failed", err)
		}
		return
	}
	// Reuse the normal admin response, including calculated stock counts.
	h.GetAdminProduct(c)
}
