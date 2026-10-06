// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package contract

import (
	"testing"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"

	"github.com/shopspring/decimal"
)

func TestApplySelfDealingRiskAuditsRelationshipWithoutExcludingProfit(t *testing.T) {
	for _, tt := range []struct {
		name         string
		buyerID      uint
		related      bool
		ownerMatch   bool
		relatedMatch bool
	}{
		{name: "owner", buyerID: 9, ownerMatch: true},
		{name: "related", buyerID: 10, related: true, relatedMatch: true},
		{name: "customer", buyerID: 11},
		{name: "guest"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &OrderPricingContext{ResellerUserID: 9, BuyerUserID: tt.buyerID, ProfitEligible: true}
			ApplySelfDealingRisk(ctx, &resellerdomain.Profile{UserID: 9}, tt.related)
			if !ctx.ProfitEligible || ctx.ProfitBlockReason != "" {
				t.Fatalf("relationship must not exclude profit: %+v", ctx)
			}
			relationship := ctx.RiskSnapshot["self_dealing"].(jsonmap.JSON)
			if relationship["owner_match"] != tt.ownerMatch || relationship["related_account_match"] != tt.relatedMatch {
				t.Fatalf("relationship audit mismatch: %+v", relationship)
			}
			if ctx.RiskSnapshot["self_purchase_policy"] != "same_margin_settlement" {
				t.Fatal("missing settlement policy audit")
			}
		})
	}
}

func TestApplySelfDealingRiskPreservesUnrelatedProfitBlock(t *testing.T) {
	ctx := &OrderPricingContext{ResellerUserID: 9, BuyerUserID: 9, ProfitEligible: false, ProfitBlockReason: "manual_review"}
	ApplySelfDealingRisk(ctx, &resellerdomain.Profile{UserID: 9}, false)
	if ctx.ProfitEligible || ctx.ProfitBlockReason != "manual_review" {
		t.Fatal("relationship audit must not override other settlement controls")
	}
}

func TestBuildSettingIndexes(t *testing.T) {
	byProduct, bySKU := BuildSettingIndexes([]resellerdomain.ProductSetting{
		{ProductID: 1, SKUID: 0, IsListed: true},
		{ProductID: 1, SKUID: 2, IsListed: false},
	})
	if byProduct[1] == nil || !byProduct[1].IsListed {
		t.Fatalf("expected product setting")
	}
	if bySKU[SettingKey{ProductID: 1, SKUID: 2}] == nil || bySKU[SettingKey{ProductID: 1, SKUID: 2}].IsListed {
		t.Fatalf("expected sku setting hidden")
	}
}

func TestMoneyString(t *testing.T) {
	if got := MoneyString(decimal.RequireFromString("1.2")); got != "1.20" {
		t.Fatalf("unexpected money string: %s", got)
	}
}
