// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package integrationtest

import (
	"fmt"
	"testing"

	"github.com/dujiao-next/internal/constants"
	productwrite "github.com/dujiao-next/internal/modules/catalog/product/application/write"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

func TestGeneralProductEditsPreserveAPIPrice(t *testing.T) {
	for _, mode := range []string{"single", "sku_id", "sku_code"} {
		t.Run(mode, func(t *testing.T) {
			services, db := newProductServiceForTest(t)
			product := productdomain.Product{Slug: "api-preserved", TitleJSON: jsonmap.JSON{"en-US": "API"}, PriceAmount: money.FromDecimal(decimal.NewFromInt(124)), FulfillmentType: constants.FulfillmentTypeManual, IsActive: true}
			if err := db.Create(&product).Error; err != nil {
				t.Fatal(err)
			}
			sku := productdomain.ProductSKU{ProductID: product.ID, SKUCode: "DEFAULT", PriceAmount: money.FromDecimal(decimal.NewFromInt(124)), APISupplyPriceAmount: money.FromDecimal(decimal.NewFromInt(115)), ResellerSupplyPriceAmount: money.FromDecimal(decimal.NewFromInt(118)), IsActive: true}
			if err := db.Create(&sku).Error; err != nil {
				t.Fatal(err)
			}
			input := productwrite.CreateProductInput{Slug: product.Slug, TitleJSON: map[string]interface{}{"en-US": "API edited"}, PriceAmount: decimal.NewFromInt(130), FulfillmentType: constants.FulfillmentTypeManual}
			if mode != "single" {
				input.SKUs = []productwrite.ProductSKUInput{{SKUCode: sku.SKUCode, PriceAmount: decimal.NewFromInt(130)}}
				if mode == "sku_id" {
					input.SKUs[0].ID = sku.ID
				}
			}
			if _, err := services.Write.Update(fmt.Sprint(product.ID), input); err != nil {
				t.Fatal(err)
			}
			var got productdomain.ProductSKU
			if err := db.First(&got, sku.ID).Error; err != nil {
				t.Fatal(err)
			}
			if got.APISupplyPriceAmount.String() != "115.00" || got.ResellerSupplyPriceAmount.String() != "118.00" || got.PriceAmount.String() != "130.00" {
				t.Fatalf("independent terms overwritten: %+v", got)
			}
		})
	}
}
