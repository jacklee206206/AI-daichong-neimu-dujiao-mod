// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application_test

import (
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/shopspring/decimal"
	"testing"
)

func TestAPIPriceDoesNotChangeHostedResellerOrders(t *testing.T) {
	fixture := newOrderResellerSnapshotFixture(t)
	if err := fixture.db.Model(&productdomain.ProductSKU{}).Where("id = ?", fixture.sku.ID).Update("api_supply_price_amount", "115.00").Error; err != nil {
		t.Fatal(err)
	}
	input := fixture.createInput(fixture.buyer.ID)
	// Even an internal caller cannot combine API terms with a hosted reseller context.
	input.UseAPISupplyPrice = true
	order, err := fixture.svc.CreateOrder(input)
	if err != nil {
		t.Fatal(err)
	}
	if !order.TotalAmount.Decimal.Equal(decimal.NewFromInt(130)) {
		t.Fatalf("API price leaked into reseller order: %s", order.TotalAmount.String())
	}
}
