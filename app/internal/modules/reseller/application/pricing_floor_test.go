// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"errors"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
	"testing"
)

func TestResellerMinimumPriceEnforcedForEveryPricingMode(t *testing.T) {
	sku := &productdomain.ProductSKU{PriceAmount: money.FromDecimal(decimal.NewFromInt(124)), ResellerSupplyPriceAmount: money.FromDecimal(decimal.NewFromInt(118)), ResellerMinPriceAmount: money.FromDecimal(decimal.NewFromInt(125))}
	for _, tc := range []struct {
		name, mode, amount, want string
		fail                     bool
	}{
		{"inherit", resellerdomain.PricingModeInherit, "0", "125.00", false},
		{"fixed_below", resellerdomain.PricingModeFixedPrice, "124.99", "", true},
		{"fixed_at_floor", resellerdomain.PricingModeFixedPrice, "125", "125.00", false},
		{"markup_below", resellerdomain.PricingModeFixedMarkup, "6.99", "", true},
		{"markup_at_floor", resellerdomain.PricingModeFixedMarkup, "7", "125.00", false},
		{"percent_below", resellerdomain.PricingModeMarkupPercent, "5", "", true},
		{"percent_above", resellerdomain.PricingModeMarkupPercent, "6", "125.08", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			amount := money.FromDecimal(decimal.RequireFromString(tc.amount))
			rule := &resellerdomain.ProductSetting{PricingMode: tc.mode, FixedPriceAmount: amount, FixedMarkupAmount: amount, MarkupPercent: amount}
			price, _, supply, floor, err := ResolveSKUPrice(nil, sku, nil, rule)
			if tc.fail {
				if !errors.Is(err, resellercontract.ErrPriceBelowBase) {
					t.Fatalf("low price accepted: %v", err)
				}
				return
			}
			if err != nil || price.StringFixed(2) != tc.want || supply.StringFixed(2) != "118.00" || floor.StringFixed(2) != "125.00" {
				t.Fatalf("wrong terms %s %s %s %v", price, supply, floor, err)
			}
		})
	}
	// Existing administrator-specific agreements intentionally override defaults.
	special := &resellerdomain.ProductSetting{PricingMode: resellerdomain.PricingModeInherit, MinPriceAmount: money.FromDecimal(decimal.NewFromInt(120))}
	price, _, _, floor, err := ResolveSKUPrice(nil, sku, special, nil)
	if err != nil || price.StringFixed(2) != "120.00" || floor.StringFixed(2) != "120.00" {
		t.Fatalf("existing specific agreement changed: %s %s %v", price, floor, err)
	}
}
