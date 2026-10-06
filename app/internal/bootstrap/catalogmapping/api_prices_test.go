// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package catalogmappingbootstrap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	mappingdomain "github.com/dujiao-next/internal/modules/catalog/mapping/domain"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/upstream"
)

func TestUpstreamSyncPreservesIndependentAPISupplyPrice(t *testing.T) {
	for _, action := range []string{"single", "bulk", "markup"} {
		t.Run(action, func(t *testing.T) {
			upstreamProduct := upstream.UpstreamProduct{ID: 101, Title: jsonmap.JSON{"en-US": "API"}, PriceAmount: "119.00", Currency: "CNY", FulfillmentType: constants.FulfillmentTypeAuto, IsActive: true, SKUs: []upstream.UpstreamSKU{{ID: 201, SKUCode: "SKU-A", PriceAmount: "119.00", IsActive: true, StockQuantity: 100}}}
			service, db, mapping, cleanup := setupMappingWithUpstreamHandler(t, fmt.Sprintf("file:api_preserved_%s_%d?mode=memory&cache=shared", action, time.Now().UnixNano()), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if action == "bulk" {
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "items": []upstream.UpstreamProduct{upstreamProduct}, "total": 1, "page": 1, "page_size": 50, "includes_inactive": true})
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "product": upstreamProduct})
				}
			})
			defer cleanup()
			if err := db.Model(&siteconnectiondomain.Connection{}).Where("id = ?", mapping.ConnectionID).Update("auto_sync_price", true).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&productdomain.ProductSKU{}).Where("product_id = ?", mapping.LocalProductID).Update("api_supply_price_amount", "115.00").Error; err != nil {
				t.Fatal(err)
			}
			switch action {
			case "single":
				if err := service.SyncProduct(mapping.ID); err != nil {
					t.Fatal(err)
				}
			case "bulk":
				if err := service.SyncConnectionStock(mapping.ConnectionID, []mappingdomain.Mapping{*mapping}, 50, 200); err != nil {
					t.Fatal(err)
				}
			case "markup":
				if _, err := service.ReapplyMarkup(mapping.ConnectionID); err != nil {
					t.Fatal(err)
				}
			}
			var sku productdomain.ProductSKU
			if err := db.Where("product_id = ?", mapping.LocalProductID).First(&sku).Error; err != nil {
				t.Fatal(err)
			}
			if sku.APISupplyPriceAmount.String() != "115.00" {
				t.Fatalf("sync changed API price: %+v", sku)
			}
		})
	}
}
