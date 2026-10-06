// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package producthttp

import (
	"errors"
	productwrite "github.com/dujiao-next/internal/modules/catalog/product/application/write"
	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"strings"
)

type updateAPIPricesRequest struct {
	SKUs []struct {
		ID     uint    `json:"id"`
		Supply *string `json:"api_supply_price_amount"`
	} `json:"skus"`
}

func (h *AdminProductHandler) UpdateAPIPrices(c *gin.Context) {
	var req updateAPIPricesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	if len(req.SKUs) == 0 || len(req.SKUs) > 500 {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}
	inputs := make([]productwrite.APIPriceInput, 0, len(req.SKUs))
	for _, sku := range req.SKUs {
		if sku.Supply == nil || !resellerPriceAmountPattern.MatchString(strings.TrimSpace(*sku.Supply)) {
			ginutil.RespondError(c, response.CodeBadRequest, "error.product_price_invalid", nil)
			return
		}
		amount, _ := decimal.NewFromString(strings.TrimSpace(*sku.Supply))
		inputs = append(inputs, productwrite.APIPriceInput{ID: sku.ID, SupplyPriceAmount: amount})
	}
	if err := h.writer.UpdateAPIPrices(c.Param("id"), inputs); err != nil {
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
	h.GetAdminProduct(c)
}
