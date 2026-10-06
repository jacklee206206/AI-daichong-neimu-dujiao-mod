// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package upstreamhttp

import (
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
	"testing"
)

func TestAPICatalogMixedSKUsUsesActualMinimumAndApplicableWholesaleTiers(t *testing.T) {
	amount := func(value int64) money.Amount { return money.FromDecimal(decimal.NewFromInt(value)) }
	product := productdomain.Product{ID: 1, PriceAmount: amount(10), SKUs: []productdomain.ProductSKU{
		{ID: 1, SKUCode: "FIXED", PriceAmount: amount(150), APISupplyPriceAmount: amount(115), IsActive: true},
		{ID: 2, SKUCode: "LEGACY", PriceAmount: amount(100), IsActive: true},
		{ID: 3, SKUCode: "OFF", PriceAmount: amount(1), APISupplyPriceAmount: amount(1), IsActive: false},
	}, WholesalePrices: productdomain.WholesalePriceTiers{
		{MinQuantity: 2, UnitPrice: amount(80)},
		{SKUID: 1, MinQuantity: 2, UnitPrice: amount(50)},
		{SKUCode: " legacy ", MinQuantity: 5, UnitPrice: amount(90)},
	}}
	result := (&Handler{}).toUpstreamProductWithMemberPrice(product, 0, nil)
	if result.PriceAmount != "100.00" || len(result.SKUs) != 2 || result.SKUs[0].PriceAmount != "115.00" {
		t.Fatalf("wrong mixed catalog: %+v", result)
	}
	if len(result.WholesalePrices) != 1 || result.WholesalePrices[0].SKUID != 2 || result.WholesalePrices[0].MinQuantity != 5 {
		t.Fatalf("fixed SKU discount or overridden global tier leaked: %+v", result.WholesalePrices)
	}
	// Without a SKU-specific legacy tier, the global tier is advertised only for legacy SKU 2.
	product.WholesalePrices = product.WholesalePrices[:2]
	result = (&Handler{}).toUpstreamProductWithMemberPrice(product, 0, nil)
	if len(result.WholesalePrices) != 1 || result.WholesalePrices[0].SKUID != 2 || result.WholesalePrices[0].UnitPrice.String() != "80.00" {
		t.Fatalf("global tier not scoped to legacy SKU: %+v", result.WholesalePrices)
	}
	// No configured API price preserves the original legacy wholesale contract.
	product.SKUs[0].APISupplyPriceAmount = amount(0)
	result = (&Handler{}).toUpstreamProductWithMemberPrice(product, 0, nil)
	if len(result.WholesalePrices) != 2 || result.WholesalePrices[0].SKUID != 0 {
		t.Fatalf("zero override changed legacy wholesale metadata: %+v", result.WholesalePrices)
	}
}
