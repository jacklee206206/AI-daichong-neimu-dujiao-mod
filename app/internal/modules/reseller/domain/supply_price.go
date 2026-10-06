// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package domain

import (
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/shopspring/decimal"
)

// ResolveSupplyTerms separates the reseller supply agreement from the main store
// retail price and private procurement cost. Zero means inherit, never free stock.
func ResolveSupplyTerms(sku *productdomain.ProductSKU, productSetting, skuSetting *ProductSetting) (decimal.Decimal, decimal.Decimal) {
	supply, floor := decimal.Zero, decimal.Zero
	if sku != nil {
		supply = sku.PriceAmount.Decimal
		if sku.ResellerSupplyPriceAmount.Decimal.IsPositive() {
			supply = sku.ResellerSupplyPriceAmount.Decimal
		}
		floor = sku.ResellerMinPriceAmount.Decimal
	}
	for _, setting := range []*ProductSetting{productSetting, skuSetting} {
		if setting == nil {
			continue
		}
		if setting.SupplyPriceAmount.Decimal.IsPositive() {
			supply = setting.SupplyPriceAmount.Decimal
		}
		if setting.MinPriceAmount.Decimal.IsPositive() {
			floor = setting.MinPriceAmount.Decimal
		}
	}
	supply, floor = supply.Round(2), floor.Round(2)
	if floor.LessThan(supply) {
		floor = supply
	}
	return supply, floor
}
