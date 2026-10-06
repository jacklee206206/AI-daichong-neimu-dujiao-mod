// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package integrationtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dujiao-next/internal/constants"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	producthttp "github.com/dujiao-next/internal/modules/catalog/product/transport/http"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func TestAdminAPIPricesNarrowAtomicUpdate(t *testing.T) {
	h, db := setupAdminProductHandlerTest(t)
	product := productdomain.Product{Slug: "price-controls", TitleJSON: jsonmap.JSON{"en-US": "Controls"}, PriceAmount: money.FromDecimal(decimal.NewFromInt(124)), FulfillmentType: constants.FulfillmentTypeManual, IsActive: true}
	other := productdomain.Product{Slug: "other-controls", TitleJSON: jsonmap.JSON{"en-US": "Other"}, PriceAmount: money.FromDecimal(decimal.NewFromInt(300)), IsActive: true}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	rows := []productdomain.ProductSKU{
		{ProductID: product.ID, SKUCode: "A", PriceAmount: money.FromDecimal(decimal.NewFromInt(124)), CostPriceAmount: money.FromDecimal(decimal.NewFromInt(87)), ResellerSupplyPriceAmount: money.FromDecimal(decimal.NewFromInt(118)), ResellerMinPriceAmount: money.FromDecimal(decimal.NewFromInt(125)), ManualStockTotal: 9, ManualStockLocked: 2, ManualStockSold: 1, IsActive: true},
		{ProductID: product.ID, SKUCode: "B", PriceAmount: money.FromDecimal(decimal.NewFromInt(200)), ResellerMinPriceAmount: money.FromDecimal(decimal.NewFromInt(199)), IsActive: true},
		{ProductID: other.ID, SKUCode: "OTHER", PriceAmount: money.FromDecimal(decimal.NewFromInt(300)), IsActive: true},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	producthttp.RegisterAdminRoutes(router.Group("/api/v1/admin"), h)
	call := func(body string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/admin/products/%d/api-prices", product.ID), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("transport status=%d", w.Code)
		}
		var envelope struct {
			StatusCode int             `json:"status_code"`
			Data       json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.StatusCode == 0 {
			var returned productdomain.Product
			if err := json.Unmarshal(envelope.Data, &returned); err != nil || returned.ID != product.ID || len(returned.SKUs) != 2 {
				t.Fatalf("admin product response invalid: %s %v", envelope.Data, err)
			}
			if !strings.Contains(string(envelope.Data), "api_supply_price_amount") {
				t.Fatal("admin response omitted API price")
			}
		}
		return envelope.StatusCode
	}
	item := func(id uint, supply, minimum string) string {
		return fmt.Sprintf(`{"id":%d,"api_supply_price_amount":"%s","reseller_min_price_amount":"%s"}`, id, supply, minimum)
	}
	if status := call(`{"skus":[` + item(rows[0].ID, "115.00", "999.00") + `],"price_amount":1,"manual_stock_total":0,"reseller_supply_price_amount":"1"}`); status != 0 {
		t.Fatalf("update status %d", status)
	}
	var updated productdomain.Product
	if err := db.First(&updated, product.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !updated.UpdatedAt.After(product.UpdatedAt) || updated.PriceAmount.String() != "124.00" {
		t.Fatalf("product sync timestamp or retail price incorrect: %+v", updated)
	}
	var got productdomain.ProductSKU
	if err := db.First(&got, rows[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.APISupplyPriceAmount.String() != "115.00" || got.ResellerSupplyPriceAmount.String() != "118.00" || got.ResellerMinPriceAmount.String() != "125.00" || got.PriceAmount.String() != "124.00" || got.CostPriceAmount.String() != "87.00" || got.ManualStockTotal != 9 || got.ManualStockLocked != 2 || got.ManualStockSold != 1 {
		t.Fatalf("narrow update overwrote unrelated values: %+v", got)
	}
	var untouched productdomain.ProductSKU
	db.First(&untouched, rows[1].ID)
	if untouched.ResellerMinPriceAmount.String() != "199.00" {
		t.Fatal("omitted SKU changed")
	}
	for _, tc := range []struct{ name, body string }{
		{"foreign_sku", `{"skus":[` + item(rows[0].ID, "1", "2") + `,` + item(rows[2].ID, "1", "2") + `]}`},
		{"duplicate", `{"skus":[` + item(rows[0].ID, "1", "2") + `,` + item(rows[0].ID, "3", "4") + `]}`},
		{"missing_price", fmt.Sprintf(`{"skus":[{"id":%d}]}`, rows[0].ID)},
		{"negative", `{"skus":[` + item(rows[0].ID, "-1", "125") + `]}`},
		{"fraction_precision", `{"skus":[` + item(rows[0].ID, "118.001", "125") + `]}`},
		{"exponent", `{"skus":[` + item(rows[0].ID, "1e5", "125") + `]}`},
		{"empty", `{"skus":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if status := call(tc.body); status != http.StatusBadRequest {
				t.Fatalf("status=%d", status)
			}
			db.First(&got, rows[0].ID)
			if got.APISupplyPriceAmount.String() != "115.00" || got.ResellerMinPriceAmount.String() != "125.00" {
				t.Fatalf("failed batch partially changed values: %+v", got)
			}
		})
	}
	// A storage failure on the second valid SKU rolls back the first update.
	trigger := fmt.Sprintf("CREATE TRIGGER reject_second_api_price BEFORE UPDATE OF api_supply_price_amount ON product_skus WHEN NEW.id = %d BEGIN SELECT RAISE(ABORT, 'test update rejected'); END", rows[1].ID)
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
	if status := call(`{"skus":[` + item(rows[0].ID, "117", "130") + `,` + item(rows[1].ID, "190", "205") + `]}`); status != http.StatusInternalServerError {
		t.Fatalf("rollback status=%d", status)
	}
	db.First(&got, rows[0].ID)
	if got.APISupplyPriceAmount.String() != "115.00" || got.ResellerMinPriceAmount.String() != "125.00" {
		t.Fatal("transaction did not roll back")
	}
	if status := call(`{"skus":[` + item(rows[0].ID, "0", "0") + `]}`); status != 0 {
		t.Fatalf("explicit reset status=%d", status)
	}
	db.First(&got, rows[0].ID)
	if !got.APISupplyPriceAmount.Decimal.IsZero() || got.ResellerMinPriceAmount.String() != "125.00" || got.ResellerSupplyPriceAmount.String() != "118.00" {
		t.Fatal("explicit inheritance reset failed")
	}
}
