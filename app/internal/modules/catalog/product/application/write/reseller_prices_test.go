// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package productwrite

import (
	"github.com/dujiao-next/internal/constants"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
	"testing"
)

func TestGeneralSKUUpdatePreservesOmittedResellerTerms(t *testing.T) {
	svc := NewWriteService(Options{})
	old := productdomain.ProductSKU{ID: 7, SKUCode: "A", ResellerSupplyPriceAmount: money.FromDecimal(decimal.NewFromInt(118)), ResellerMinPriceAmount: money.FromDecimal(decimal.NewFromInt(125))}
	for _, id := range []uint{7, 0} {
		input := ProductSKUInput{ID: id, SKUCode: "A", PriceAmount: decimal.NewFromInt(124)}
		rows, _, _, err := svc.normalizeProductSKUInputs([]ProductSKUInput{input}, constants.FulfillmentTypeManual, map[uint]productdomain.ProductSKU{7: old})
		if err != nil || rows[0].ResellerSupplyPriceAmount.String() != "118.00" || rows[0].ResellerMinPriceAmount.String() != "125.00" {
			t.Fatalf("id=%d omitted agreement lost: %+v %v", id, rows, err)
		}
		zero := decimal.Zero
		input.ResellerMinPriceAmount = &zero
		rows, _, _, err = svc.normalizeProductSKUInputs([]ProductSKUInput{input}, constants.FulfillmentTypeManual, map[uint]productdomain.ProductSKU{7: old})
		if err != nil || !rows[0].ResellerMinPriceAmount.Decimal.IsZero() || rows[0].ResellerSupplyPriceAmount.String() != "118.00" {
			t.Fatalf("explicit reset or omission incorrect: %+v %v", rows, err)
		}
	}
}
