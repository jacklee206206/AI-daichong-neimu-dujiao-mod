// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package gormstore

import (
	"fmt"
	"math"
	"testing"
	"time"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	"github.com/dujiao-next/internal/constants"
	dashboard "github.com/dujiao-next/internal/modules/dashboard/contract"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

func TestAgreedCostResellerMarginAndCustomerSurchargeRefundAccounting(t *testing.T) {
	for _, parentRefund := range []bool{false, true} {
		for _, refunded := range []string{"0", "62.50", "125"} {
			t.Run(fmt.Sprintf("parent_%t_refund_%s", parentRefund, refunded), func(t *testing.T) {
				repo, db := setupDashboardRepositoryTest(t)
				base := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)
				refundAt := base.Add(24 * time.Hour)
				product := &productdomain.Product{CategoryID: createDashboardCategory(t, db, "agreed-cost").ID, Slug: "fictional-sample-a", TitleJSON: jsonmap.JSON{"zh-CN": "示例商品 A（虚构）"}, IsActive: true}
				if err := db.Create(product).Error; err != nil {
					t.Fatal(err)
				}
				leaf := createDashboardProfitOrderWithItem(t, db, product, "LEAF", constants.OrderStatusCompleted, 125, 110, "示例商品 A（虚构）", base)
				if err := db.Model(leaf).Updates(map[string]any{"reseller_id": 1, "reseller_profit_amount": 6}).Error; err != nil {
					t.Fatal(err)
				}
				root := &orderdomain.Order{OrderNo: "ROOT", UserID: 1, Currency: "CNY", Status: constants.OrderStatusCompleted, TotalAmount: money.FromDecimal(decimal.NewFromInt(125)), ResellerID: leaf.ResellerID, ResellerProfitAmount: money.FromDecimal(decimal.NewFromInt(6)), CreatedAt: base}
				if err := db.Create(root).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Model(leaf).Update("parent_id", root.ID).Error; err != nil {
					t.Fatal(err)
				}
				payment := &paymentdomain.Payment{OrderID: root.ID, ProviderType: constants.PaymentProviderOfficial, ChannelType: constants.PaymentChannelTypeAlipay, InteractionMode: constants.PaymentInteractionRedirect, Amount: money.FromDecimal(decimal.RequireFromString("128.75")), FeeAmount: money.FromDecimal(decimal.RequireFromString("3.75")), FeePolicy: constants.PaymentFeePolicyLegacyCustomerSurcharge, Currency: "CNY", Status: constants.PaymentStatusSuccess, CreatedAt: base}
				if err := db.Create(payment).Error; err != nil {
					t.Fatal(err)
				}
				refund := decimal.RequireFromString(refunded)
				if refund.IsPositive() {
					id := leaf.ID
					if parentRefund {
						id = root.ID
					}
					record := &orderdomain.OrderRefundRecord{UserID: 1, OrderID: id, Type: constants.OrderRefundTypeManual, Amount: money.FromDecimal(refund), Currency: "CNY", PaymentFeeRefundedAmount: money.FromDecimal(refund.Mul(decimal.RequireFromString("0.03"))), CreatedAt: refundAt}
					if err := db.Create(record).Error; err != nil {
						t.Fatal(err)
					}
					deduct := decimal.NewFromInt(6).Mul(refund).Div(decimal.NewFromInt(125)).Round(2)
					entry := &resellerdomain.LedgerEntry{ResellerID: 1, OrderID: &root.ID, Type: resellerdomain.LedgerTypeRefundDeduct, Amount: money.FromDecimal(deduct.Neg()), Currency: "CNY", IdempotencyKey: fmt.Sprintf("refund_deduct:%d", record.ID), Status: resellerdomain.LedgerStatusPendingConfirm, CreatedAt: refundAt}
					if err := db.Create(entry).Error; err != nil {
						t.Fatal(err)
					}
					status := constants.OrderStatusPartiallyRefunded
					if refunded == "125" {
						status = constants.OrderStatusRefunded
					}
					if err := db.Model(&orderdomain.Order{}).Where("id IN ?", []uint{root.ID, leaf.ID}).Update("status", status).Error; err != nil {
						t.Fatal(err)
					}
				}
				amount, _ := refund.Float64()
				overview, err := repo.GetProfitOverview(base.Add(-time.Hour), refundAt.Add(time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				if math.Abs(overview.TotalRevenue-(125-amount)) > 1e-6 || overview.TotalCost != 110 || math.Abs(overview.RefundedCost-110*amount/125) > 1e-6 || overview.ResellerProfit != 6 || math.Abs(overview.RefundedResellerProfit-6*amount/125) > 1e-6 || overview.PaymentFee != 0 {
					t.Fatalf("bad accounting/double counted parent: %+v", overview)
				}
				trends, err := repo.GetProfitTrends(base.Add(-time.Hour), refundAt.Add(time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				if len(trends) == 0 || trends[0].Revenue != 125 || trends[0].Cost != 110 || trends[0].ResellerProfit != 6 || trends[0].PaymentFee != 0 {
					t.Fatalf("bad sales day: %+v", trends)
				}
				if refund.IsPositive() && (len(trends) != 2 || trends[1].Revenue != -amount || math.Abs(trends[1].RefundedResellerProfit-6*amount/125) > 1e-6 || trends[1].PaymentFee != 0) {
					t.Fatalf("bad refund day: %+v", trends)
				}
				rankings, err := repo.GetTopProducts(base.Add(-time.Hour), refundAt.Add(time.Hour), 5)
				if err != nil {
					t.Fatal(err)
				}
				if len(rankings) != 1 || math.Abs(rankings[0].PaidAmount-(125-amount)) > 1e-6 || rankings[0].TotalCost != 110 || math.Abs(rankings[0].RefundedCost-110*amount/125) > 1e-6 || math.Abs(rankings[0].ResellerProfit-(6-6*amount/125)) > 1e-6 {
					t.Fatalf("bad product rankings: %+v", rankings)
				}
				if refund.IsPositive() {
					rankings, err = repo.GetTopProducts(refundAt.Add(-time.Hour), refundAt.Add(time.Hour), 5)
					if err != nil {
						t.Fatal(err)
					}
					if len(rankings) != 1 || rankings[0].PaidAmount != -amount || rankings[0].TotalCost != 0 || math.Abs(rankings[0].ResellerProfit+6*amount/125) > 1e-6 {
						t.Fatalf("bad refund-period ranking: %+v", rankings)
					}
				}
			})
		}
	}
}

func TestProfitReportsUseGoodsRevenueAfterCoupon(t *testing.T) {
	repo, db := setupDashboardRepositoryTest(t)
	now := time.Now().UTC()
	product := &productdomain.Product{CategoryID: createDashboardCategory(t, db, "coupon-cost").ID, Slug: "fictional-sample-a-coupon", TitleJSON: jsonmap.JSON{"zh-CN": "示例商品 A（虚构）"}, IsActive: true}
	if err := db.Create(product).Error; err != nil {
		t.Fatal(err)
	}
	order := createDashboardProfitOrderWithItem(t, db, product, "COUPON", constants.OrderStatusCompleted, 124, 110, "示例商品 A（虚构）", now)
	if err := db.Model(&orderdomain.OrderItem{}).Where("order_id = ?", order.ID).Update("coupon_discount", 4).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(order).Update("total_amount", 120).Error; err != nil {
		t.Fatal(err)
	}
	overview, err := repo.GetProfitOverview(now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if overview.TotalRevenue != 120 || overview.TotalCost != 110 || overview.ResellerProfit != 0 {
		t.Fatalf("bad coupon snapshot accounting: %+v", overview)
	}
}

func TestRefundCommissionReportsFollowActualWalletCentDeductions(t *testing.T) {
	repo, db := setupDashboardRepositoryTest(t)
	base := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)
	product := &productdomain.Product{CategoryID: createDashboardCategory(t, db, "cent-refund").ID, Slug: "fictional-sample-a-cent-refund", TitleJSON: jsonmap.JSON{"zh-CN": "示例商品 A（虚构）"}, IsActive: true}
	if err := db.Create(product).Error; err != nil {
		t.Fatal(err)
	}
	order := createDashboardProfitOrderWithItem(t, db, product, "CENTS", constants.OrderStatusRefunded, 125, 110, "示例商品 A（虚构）", base)
	if err := db.Model(order).Updates(map[string]any{"reseller_id": 1, "reseller_profit_amount": 7}).Error; err != nil {
		t.Fatal(err)
	}
	var item orderdomain.OrderItem
	if err := db.Where("order_id = ?", order.ID).First(&item).Error; err != nil {
		t.Fatal(err)
	}
	// The real wallet rounds each 1 CNY refund to 0.06 and clears the
	// remaining 6.88 on the final refund, not another proportional estimate.
	for i, tc := range []struct {
		refund, debit string
		day           int
	}{{"1", "0.06", 1}, {"1", "0.06", 1}, {"123", "6.88", 2}} {
		at := base.Add(time.Duration(tc.day)*24*time.Hour + time.Duration(i)*time.Minute)
		record := &orderdomain.OrderRefundRecord{UserID: 1, OrderID: order.ID, Type: constants.OrderRefundTypeManual, Amount: money.FromDecimal(decimal.RequireFromString(tc.refund)), Currency: "CNY", CreatedAt: at}
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
		entry := &resellerdomain.LedgerEntry{ResellerID: 1, OrderID: &order.ID, Type: resellerdomain.LedgerTypeRefundDeduct, Amount: money.FromDecimal(decimal.RequireFromString(tc.debit).Neg()), Currency: "CNY", IdempotencyKey: fmt.Sprintf("refund_deduct:%d", record.ID), Status: resellerdomain.LedgerStatusPendingConfirm, CreatedAt: at.Add(time.Second), MetadataJSON: jsonmap.JSON{"refund_allocation_json": map[string]any{"items": []map[string]any{{"order_item_id": fmt.Sprint(item.ID), "deduct_amount": tc.debit}}}}}
		if err := db.Create(entry).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name                                            string
		start, end                                      time.Time
		revenue, originalCommission, refundedCommission float64
	}{
		{"two_small_refunds", base.Add(23 * time.Hour), base.Add(47 * time.Hour), -2, 0, .12},
		{"final_remainder", base.Add(47 * time.Hour), base.Add(71 * time.Hour), -123, 0, 6.88},
		{"entire_order", base.Add(-time.Hour), base.Add(71 * time.Hour), 0, 7, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			overview, err := repo.GetProfitOverview(tc.start, tc.end)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(overview.TotalRevenue-tc.revenue) > 1e-6 || math.Abs(overview.ResellerProfit-tc.originalCommission) > 1e-6 || math.Abs(overview.RefundedResellerProfit-tc.refundedCommission) > 1e-6 {
				t.Fatalf("overview must match wallet, got %+v", overview)
			}
			trends, err := repo.GetProfitTrends(tc.start, tc.end)
			if err != nil {
				t.Fatal(err)
			}
			total := 0.0
			for _, day := range trends {
				total += day.RefundedResellerProfit
			}
			if math.Abs(total-tc.refundedCommission) > 1e-6 {
				t.Fatalf("trend commission %.8f differs from wallet %.2f", total, tc.refundedCommission)
			}
			rankings, err := repo.GetTopProducts(tc.start, tc.end, 5)
			if err != nil {
				t.Fatal(err)
			}
			if len(rankings) != 1 || math.Abs(rankings[0].ResellerProfit-(tc.originalCommission-tc.refundedCommission)) > 1e-6 {
				t.Fatalf("ranking commission must match wallet, got %+v", rankings)
			}
		})
	}
}

func TestRefundWithoutWalletDeductionDoesNotInventCommissionReversal(t *testing.T) {
	repo, db := setupDashboardRepositoryTest(t)
	now := time.Now().UTC()
	product := &productdomain.Product{CategoryID: createDashboardCategory(t, db, "no-refund-ledger").ID, Slug: "no-refund-ledger", TitleJSON: jsonmap.JSON{"zh-CN": "示例商品 A（虚构）"}, IsActive: true}
	if err := db.Create(product).Error; err != nil {
		t.Fatal(err)
	}
	order := createDashboardProfitOrderWithItem(t, db, product, "NO-LEDGER", constants.OrderStatusPartiallyRefunded, 125, 110, "示例商品 A（虚构）", now)
	if err := db.Model(order).Updates(map[string]any{"reseller_id": 1, "reseller_profit_amount": 7}).Error; err != nil {
		t.Fatal(err)
	}
	record := &orderdomain.OrderRefundRecord{UserID: 1, OrderID: order.ID, Type: constants.OrderRefundTypeManual, Amount: money.FromDecimal(decimal.NewFromInt(1)), Currency: "CNY", CreatedAt: now}
	if err := db.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	overview, err := repo.GetProfitOverview(now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if overview.RefundedResellerProfit != 0 {
		t.Fatalf("no ledger debit exists, got %.2f reversal", overview.RefundedResellerProfit)
	}
	rankings, err := repo.GetTopProducts(now.Add(-time.Hour), now.Add(time.Hour), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rankings) != 1 || rankings[0].ResellerProfit != 7 {
		t.Fatalf("no ledger debit exists, got %+v", rankings)
	}
}

func TestProductRefundAllocationPreservesActualDebitWhenMetadataHasRoundingRemainder(t *testing.T) {
	lines := []productRefundRow{
		{OrderItemID: 1, RefundLedgerAmount: money.FromDecimal(decimal.RequireFromString("-0.05")), RefundLedgerMetadata: jsonmap.JSON{"refund_allocation_json": map[string]any{"items": []map[string]any{{"order_item_id": "1", "deduct_amount": "0.02"}, {"order_item_id": "2", "deduct_amount": "0.02"}, {"order_item_id": "3", "deduct_amount": "0.02"}}}}},
		{OrderItemID: 2}, {OrderItemID: 3},
	}
	allocateRefundLedgerAmount(lines)
	for i, want := range []float64{-.02, -.02, -.01} {
		if math.Abs(lines[i].ResellerProfit-want) > 1e-6 {
			t.Fatalf("line %d got %.4f want %.2f", i, lines[i].ResellerProfit, want)
		}
	}
}

func TestGetProfitOverviewDeductsRefundRecords(t *testing.T) {
	repo, db := setupDashboardRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Second)

	category := createDashboardCategory(t, db, "dashboard-profit-refund-category")
	product := &productdomain.Product{
		CategoryID:      category.ID,
		Slug:            "dashboard-profit-refund-product",
		TitleJSON:       jsonmap.JSON{"zh-CN": "利润测试商品"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(100)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeManual,
		IsActive:        true,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	manualRefundedOrder := createDashboardProfitOrderWithItem(t, db, product, "DJ-PROFIT-MANUAL", constants.OrderStatusRefunded, 100, 40, "利润测试商品", now)
	walletRefundedOrder := createDashboardProfitOrderWithItem(t, db, product, "DJ-PROFIT-WALLET", constants.OrderStatusPartiallyRefunded, 120, 50, "利润测试商品", now)

	records := []orderdomain.OrderRefundRecord{
		{
			UserID:    1,
			OrderID:   manualRefundedOrder.ID,
			Type:      constants.OrderRefundTypeManual,
			Amount:    money.FromDecimal(decimal.NewFromInt(100)),
			Currency:  "CNY",
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			UserID:    1,
			OrderID:   walletRefundedOrder.ID,
			Type:      constants.OrderRefundTypeWallet,
			Amount:    money.FromDecimal(decimal.NewFromInt(20)),
			Currency:  "CNY",
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			UserID:    1,
			OrderID:   walletRefundedOrder.ID,
			Type:      constants.OrderRefundTypeManual,
			Amount:    money.FromDecimal(decimal.NewFromInt(10)),
			Currency:  "CNY",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	for idx := range records {
		if err := db.Create(&records[idx]).Error; err != nil {
			t.Fatalf("create refund record failed: %v", err)
		}
	}

	result, err := repo.GetProfitOverview(now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("get profit overview failed: %v", err)
	}
	if math.Abs(result.TotalRevenue-90) > 0.000001 {
		t.Fatalf("total revenue want 90 got %.2f", result.TotalRevenue)
	}
	if math.Abs(result.TotalCost-90) > 0.000001 {
		t.Fatalf("total cost want 90 got %.2f", result.TotalCost)
	}
	if math.Abs(result.RefundedCost-52.5) > 0.000001 {
		t.Fatalf("refunded cost want 52.5 got %.2f", result.RefundedCost)
	}
}

func TestGetProfitTrendsDeductsRefundRecords(t *testing.T) {
	repo, db := setupDashboardRepositoryTest(t)
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

	category := createDashboardCategory(t, db, "dashboard-profit-trend-refund-category")
	product := &productdomain.Product{
		CategoryID:      category.ID,
		Slug:            "dashboard-profit-trend-refund-product",
		TitleJSON:       jsonmap.JSON{"zh-CN": "利润趋势测试商品"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(100)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeManual,
		IsActive:        true,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	day1Order := createDashboardProfitOrderWithItem(t, db, product, "DJ-PROFIT-TREND-DAY1", constants.OrderStatusRefunded, 80, 30, "利润趋势测试商品", base)
	day2Order := createDashboardProfitOrderWithItem(t, db, product, "DJ-PROFIT-TREND-DAY2", constants.OrderStatusRefunded, 100, 40, "利润趋势测试商品", base.Add(24*time.Hour))

	records := []orderdomain.OrderRefundRecord{
		{
			UserID:    1,
			OrderID:   day1Order.ID,
			Type:      constants.OrderRefundTypeManual,
			Amount:    money.FromDecimal(decimal.NewFromInt(80)),
			Currency:  "CNY",
			CreatedAt: base,
			UpdatedAt: base,
		},
		{
			UserID:    1,
			OrderID:   day2Order.ID,
			Type:      constants.OrderRefundTypeWallet,
			Amount:    money.FromDecimal(decimal.NewFromInt(30)),
			Currency:  "CNY",
			CreatedAt: base.Add(24 * time.Hour),
			UpdatedAt: base.Add(24 * time.Hour),
		},
		{
			UserID:    1,
			OrderID:   day2Order.ID,
			Type:      constants.OrderRefundTypeManual,
			Amount:    money.FromDecimal(decimal.NewFromInt(10)),
			Currency:  "CNY",
			CreatedAt: base.Add(24 * time.Hour),
			UpdatedAt: base.Add(24 * time.Hour),
		},
	}
	for idx := range records {
		if err := db.Create(&records[idx]).Error; err != nil {
			t.Fatalf("create refund record failed: %v", err)
		}
	}

	rows, err := repo.GetProfitTrends(base.Add(-time.Hour), base.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("get profit trends failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("profit trend rows want 2 got %d", len(rows))
	}
	rowMap := make(map[string]dashboard.ProfitTrendRow, len(rows))
	for _, row := range rows {
		rowMap[row.Day] = row
	}
	day1 := "2026-03-01"
	day2 := "2026-03-02"
	if math.Abs(rowMap[day1].Revenue-0) > 0.000001 || math.Abs(rowMap[day1].Cost-30) > 0.000001 || math.Abs(rowMap[day1].RefundedCost-30) > 0.000001 {
		t.Fatalf("unexpected day1 row: %+v", rowMap[day1])
	}
	if math.Abs(rowMap[day2].Revenue-60) > 0.000001 || math.Abs(rowMap[day2].Cost-40) > 0.000001 || math.Abs(rowMap[day2].RefundedCost-16) > 0.000001 {
		t.Fatalf("unexpected day2 row: %+v", rowMap[day2])
	}
}

func TestProfitStatisticsDeductSuccessfulExternalPaymentFees(t *testing.T) {
	repo, db := setupDashboardRepositoryTest(t)
	base := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)

	category := createDashboardCategory(t, db, "dashboard-profit-payment-fee-category")
	product := &productdomain.Product{
		CategoryID:      category.ID,
		Slug:            "dashboard-profit-payment-fee-product",
		TitleJSON:       jsonmap.JSON{"zh-CN": "手续费利润测试商品"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(100)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeManual,
		IsActive:        true,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}
	order := createDashboardProfitOrderWithItem(t, db, product, "DJ-PROFIT-PAYMENT-FEE", constants.OrderStatusPaid, 100, 40, "手续费利润测试商品", base)

	payments := []paymentdomain.Payment{
		{
			OrderID: order.ID, ProviderType: constants.PaymentProviderOfficial,
			ChannelType: constants.PaymentChannelTypeAlipay, InteractionMode: constants.PaymentInteractionRedirect,
			Amount: money.FromDecimal(decimal.NewFromInt(100)), FeeAmount: money.FromDecimal(decimal.RequireFromString("3.00")), FeePolicy: constants.PaymentFeePolicyMerchantAbsorbed,
			Currency: "CNY", Status: constants.PaymentStatusSuccess, CreatedAt: base, UpdatedAt: base,
		},
		{
			OrderID: order.ID, ProviderType: constants.PaymentProviderOfficial,
			ChannelType: constants.PaymentChannelTypeWechat, InteractionMode: constants.PaymentInteractionQR,
			Amount: money.FromDecimal(decimal.NewFromInt(100)), FeeAmount: money.FromDecimal(decimal.RequireFromString("9.00")), FeePolicy: constants.PaymentFeePolicyMerchantAbsorbed,
			Currency: "CNY", Status: constants.PaymentStatusFailed, CreatedAt: base, UpdatedAt: base,
		},
		{
			OrderID: 0, ProviderType: constants.PaymentProviderOfficial,
			ChannelType: constants.PaymentChannelTypeAlipay, InteractionMode: constants.PaymentInteractionRedirect,
			Amount: money.FromDecimal(decimal.NewFromInt(50)), FeeAmount: money.FromDecimal(decimal.RequireFromString("2.00")), FeePolicy: constants.PaymentFeePolicyMerchantAbsorbed,
			Currency: "CNY", Status: constants.PaymentStatusSuccess, CreatedAt: base, UpdatedAt: base,
		},
		{
			OrderID: order.ID, ProviderType: constants.PaymentProviderWallet,
			ChannelType: constants.PaymentChannelTypeBalance, InteractionMode: constants.PaymentInteractionBalance,
			Amount: money.FromDecimal(decimal.NewFromInt(100)), FeeAmount: money.FromDecimal(decimal.RequireFromString("99.00")), FeePolicy: constants.PaymentFeePolicyMerchantAbsorbed,
			Currency: "CNY", Status: constants.PaymentStatusSuccess, CreatedAt: base, UpdatedAt: base,
		},
		{
			OrderID: order.ID, ProviderType: constants.PaymentProviderOfficial,
			ChannelType: constants.PaymentChannelTypeAlipay, InteractionMode: constants.PaymentInteractionRedirect,
			Amount: money.FromDecimal(decimal.NewFromInt(103)), FeeAmount: money.FromDecimal(decimal.RequireFromString("11.00")), FeePolicy: constants.PaymentFeePolicyLegacyCustomerSurcharge,
			Currency: "CNY", Status: constants.PaymentStatusSuccess, CreatedAt: base, UpdatedAt: base,
		},
		{
			OrderID: order.ID, ProviderType: constants.PaymentProviderOfficial,
			ChannelType: constants.PaymentChannelTypeAlipay, InteractionMode: constants.PaymentInteractionRedirect,
			Amount: money.FromDecimal(decimal.NewFromInt(100)), FeeAmount: money.FromDecimal(decimal.RequireFromString("7.00")), FeePolicy: constants.PaymentFeePolicyMerchantAbsorbed,
			Currency: "CNY", Status: constants.PaymentStatusSuccess, CreatedAt: base.Add(48 * time.Hour), UpdatedAt: base.Add(48 * time.Hour),
		},
	}
	if err := db.Create(&payments).Error; err != nil {
		t.Fatalf("create payments failed: %v", err)
	}

	startAt := base.Add(-time.Hour)
	endAt := base.Add(24 * time.Hour)
	overview, err := repo.GetProfitOverview(startAt, endAt)
	if err != nil {
		t.Fatalf("get profit overview failed: %v", err)
	}
	if math.Abs(overview.TotalRevenue-100) > 0.000001 || math.Abs(overview.TotalCost-40) > 0.000001 || math.Abs(overview.PaymentFee-5) > 0.000001 {
		t.Fatalf("unexpected profit overview: %+v", overview)
	}

	rows, err := repo.GetProfitTrends(startAt, endAt)
	if err != nil {
		t.Fatalf("get profit trends failed: %v", err)
	}
	if len(rows) != 1 || math.Abs(rows[0].PaymentFee-5) > 0.000001 {
		t.Fatalf("unexpected profit trends: %+v", rows)
	}
}

func TestProfitStatisticsReverseReturnedPaymentFeeOnRefundDay(t *testing.T) {
	repo, db := setupDashboardRepositoryTest(t)
	base := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	refundAt := base.Add(24 * time.Hour)

	category := createDashboardCategory(t, db, "dashboard-profit-refunded-payment-fee-category")
	product := &productdomain.Product{
		CategoryID:      category.ID,
		Slug:            "dashboard-profit-refunded-payment-fee-product",
		TitleJSON:       jsonmap.JSON{"zh-CN": "退款手续费冲回测试商品"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(100)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeManual,
		IsActive:        true,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}
	order := createDashboardProfitOrderWithItem(t, db, product, "DJ-PROFIT-REFUNDED-PAYMENT-FEE", constants.OrderStatusRefunded, 100, 40, "退款手续费冲回测试商品", base)
	if err := db.Create(&paymentdomain.Payment{
		OrderID: order.ID, ProviderType: constants.PaymentProviderOfficial,
		ChannelType: constants.PaymentChannelTypeAlipay, InteractionMode: constants.PaymentInteractionRedirect,
		Amount: money.FromDecimal(decimal.NewFromInt(100)), FeeAmount: money.FromDecimal(decimal.RequireFromString("3.00")), FeePolicy: constants.PaymentFeePolicyMerchantAbsorbed,
		Currency: "CNY", Status: constants.PaymentStatusSuccess, CreatedAt: base, UpdatedAt: base,
	}).Error; err != nil {
		t.Fatalf("create payment failed: %v", err)
	}
	if err := db.Create(&orderdomain.OrderRefundRecord{
		OrderID: order.ID, Type: constants.OrderRefundTypeManual,
		Amount:                   money.FromDecimal(decimal.NewFromInt(100)),
		PaymentFeeRefunded:       true,
		PaymentFeeRefundedAmount: money.FromDecimal(decimal.RequireFromString("3.00")),
		Currency:                 "CNY",
		CreatedAt:                refundAt,
		UpdatedAt:                refundAt,
	}).Error; err != nil {
		t.Fatalf("create refund record failed: %v", err)
	}

	overview, err := repo.GetProfitOverview(base.Add(-time.Hour), refundAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("get profit overview failed: %v", err)
	}
	if math.Abs(overview.PaymentFee) > 0.000001 {
		t.Fatalf("net payment fee = %v, want 0", overview.PaymentFee)
	}

	rows, err := repo.GetProfitTrends(base.Add(-time.Hour), refundAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("get profit trends failed: %v", err)
	}
	rowMap := make(map[string]dashboard.ProfitTrendRow, len(rows))
	for _, row := range rows {
		rowMap[row.Day] = row
	}
	if math.Abs(rowMap["2026-08-11"].PaymentFee-3) > 0.000001 {
		t.Fatalf("payment-day fee = %v, want 3", rowMap["2026-08-11"].PaymentFee)
	}
	if math.Abs(rowMap["2026-08-12"].PaymentFee+3) > 0.000001 {
		t.Fatalf("refund-day fee = %v, want -3", rowMap["2026-08-12"].PaymentFee)
	}

	refundOnly, err := repo.GetProfitOverview(refundAt.Add(-time.Hour), refundAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("get refund-only overview failed: %v", err)
	}
	if math.Abs(refundOnly.PaymentFee+3) > 0.000001 {
		t.Fatalf("refund-only payment fee = %v, want -3", refundOnly.PaymentFee)
	}
}

func TestGetProfitOverviewDeductsInWindowRefundForOutOfWindowOrder(t *testing.T) {
	repo, db := setupDashboardRepositoryTest(t)
	now := time.Now().UTC().Truncate(time.Second)
	startAt := now.AddDate(0, 0, -7)
	endAt := now.Add(time.Hour)

	category := createDashboardCategory(t, db, "dashboard-profit-period-refund-category")
	product := &productdomain.Product{
		CategoryID:      category.ID,
		Slug:            "dashboard-profit-period-refund-product",
		TitleJSON:       jsonmap.JSON{"zh-CN": "周期退款测试商品"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(100)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeManual,
		IsActive:        true,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	outsideOrder := &orderdomain.Order{
		OrderNo:        "DJ-PROFIT-OUTSIDE-ORDER",
		UserID:         1,
		Status:         constants.OrderStatusRefunded,
		Currency:       "CNY",
		OriginalAmount: money.FromDecimal(decimal.NewFromInt(100)),
		DiscountAmount: money.FromDecimal(decimal.Zero),
		TotalAmount:    money.FromDecimal(decimal.NewFromInt(100)),
		CreatedAt:      startAt.Add(-24 * time.Hour),
		UpdatedAt:      startAt.Add(-24 * time.Hour),
	}
	if err := db.Create(outsideOrder).Error; err != nil {
		t.Fatalf("create outside order failed: %v", err)
	}
	if err := db.Create(&orderdomain.OrderItem{
		OrderID:         outsideOrder.ID,
		ProductID:       product.ID,
		TitleJSON:       jsonmap.JSON{"zh-CN": "周期退款测试商品"},
		UnitPrice:       money.FromDecimal(decimal.NewFromInt(100)),
		CostPrice:       money.FromDecimal(decimal.NewFromInt(40)),
		Quantity:        1,
		TotalPrice:      money.FromDecimal(decimal.NewFromInt(100)),
		CouponDiscount:  money.FromDecimal(decimal.Zero),
		FulfillmentType: constants.FulfillmentTypeManual,
		CreatedAt:       startAt.Add(-24 * time.Hour),
		UpdatedAt:       startAt.Add(-24 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("create outside order item failed: %v", err)
	}

	inWindowOrder := &orderdomain.Order{
		OrderNo:        "DJ-PROFIT-IN-WINDOW",
		UserID:         1,
		Status:         constants.OrderStatusCompleted,
		Currency:       "CNY",
		OriginalAmount: money.FromDecimal(decimal.NewFromInt(60)),
		DiscountAmount: money.FromDecimal(decimal.Zero),
		TotalAmount:    money.FromDecimal(decimal.NewFromInt(60)),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := db.Create(inWindowOrder).Error; err != nil {
		t.Fatalf("create in-window order failed: %v", err)
	}
	if err := db.Create(&orderdomain.OrderItem{
		OrderID:         inWindowOrder.ID,
		ProductID:       product.ID,
		TitleJSON:       jsonmap.JSON{"zh-CN": "周期退款测试商品"},
		UnitPrice:       money.FromDecimal(decimal.NewFromInt(60)),
		CostPrice:       money.FromDecimal(decimal.NewFromInt(20)),
		Quantity:        1,
		TotalPrice:      money.FromDecimal(decimal.NewFromInt(60)),
		CouponDiscount:  money.FromDecimal(decimal.Zero),
		FulfillmentType: constants.FulfillmentTypeManual,
		CreatedAt:       now,
		UpdatedAt:       now,
	}).Error; err != nil {
		t.Fatalf("create in-window order item failed: %v", err)
	}

	if err := db.Create(&orderdomain.OrderRefundRecord{
		UserID:    1,
		OrderID:   outsideOrder.ID,
		Type:      constants.OrderRefundTypeManual,
		Amount:    money.FromDecimal(decimal.NewFromInt(50)),
		Currency:  "CNY",
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create refund record failed: %v", err)
	}

	result, err := repo.GetProfitOverview(startAt, endAt)
	if err != nil {
		t.Fatalf("get profit overview failed: %v", err)
	}
	if math.Abs(result.TotalRevenue-10) > 0.000001 {
		t.Fatalf("total revenue want 10 got %.2f", result.TotalRevenue)
	}
	if math.Abs(result.TotalCost-20) > 0.000001 {
		t.Fatalf("total cost want 20 got %.2f", result.TotalCost)
	}
	if math.Abs(result.RefundedCost-20) > 0.000001 {
		t.Fatalf("refunded cost want 20 got %.2f", result.RefundedCost)
	}
}

func TestGetProfitTrendsIncludesRefundOnlyDayInWindow(t *testing.T) {
	repo, db := setupDashboardRepositoryTest(t)
	startAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	endAt := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	day1 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 3, 2, 11, 0, 0, 0, time.UTC)

	category := createDashboardCategory(t, db, "dashboard-profit-refund-only-day-category")
	product := &productdomain.Product{
		CategoryID:      category.ID,
		Slug:            "dashboard-profit-refund-only-day-product",
		TitleJSON:       jsonmap.JSON{"zh-CN": "退款单日测试商品"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(100)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeManual,
		IsActive:        true,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	inWindowOrder := &orderdomain.Order{
		OrderNo:        "DJ-PROFIT-TREND-IN-WINDOW",
		UserID:         1,
		Status:         constants.OrderStatusCompleted,
		Currency:       "CNY",
		OriginalAmount: money.FromDecimal(decimal.NewFromInt(80)),
		DiscountAmount: money.FromDecimal(decimal.Zero),
		TotalAmount:    money.FromDecimal(decimal.NewFromInt(80)),
		CreatedAt:      day1,
		UpdatedAt:      day1,
	}
	if err := db.Create(inWindowOrder).Error; err != nil {
		t.Fatalf("create in-window order failed: %v", err)
	}
	if err := db.Create(&orderdomain.OrderItem{
		OrderID:         inWindowOrder.ID,
		ProductID:       product.ID,
		TitleJSON:       jsonmap.JSON{"zh-CN": "退款单日测试商品"},
		UnitPrice:       money.FromDecimal(decimal.NewFromInt(80)),
		CostPrice:       money.FromDecimal(decimal.NewFromInt(30)),
		Quantity:        1,
		TotalPrice:      money.FromDecimal(decimal.NewFromInt(80)),
		CouponDiscount:  money.FromDecimal(decimal.Zero),
		FulfillmentType: constants.FulfillmentTypeManual,
		CreatedAt:       day1,
		UpdatedAt:       day1,
	}).Error; err != nil {
		t.Fatalf("create in-window order item failed: %v", err)
	}

	outsideOrder := &orderdomain.Order{
		OrderNo:        "DJ-PROFIT-TREND-OUTSIDE-ORDER",
		UserID:         1,
		Status:         constants.OrderStatusRefunded,
		Currency:       "CNY",
		OriginalAmount: money.FromDecimal(decimal.NewFromInt(100)),
		DiscountAmount: money.FromDecimal(decimal.Zero),
		TotalAmount:    money.FromDecimal(decimal.NewFromInt(100)),
		CreatedAt:      startAt.Add(-48 * time.Hour),
		UpdatedAt:      startAt.Add(-48 * time.Hour),
	}
	if err := db.Create(outsideOrder).Error; err != nil {
		t.Fatalf("create outside order failed: %v", err)
	}
	if err := db.Create(&orderdomain.OrderItem{
		OrderID:         outsideOrder.ID,
		ProductID:       product.ID,
		TitleJSON:       jsonmap.JSON{"zh-CN": "退款单日测试商品"},
		UnitPrice:       money.FromDecimal(decimal.NewFromInt(100)),
		CostPrice:       money.FromDecimal(decimal.NewFromInt(40)),
		Quantity:        1,
		TotalPrice:      money.FromDecimal(decimal.NewFromInt(100)),
		CouponDiscount:  money.FromDecimal(decimal.Zero),
		FulfillmentType: constants.FulfillmentTypeManual,
		CreatedAt:       startAt.Add(-48 * time.Hour),
		UpdatedAt:       startAt.Add(-48 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("create outside order item failed: %v", err)
	}

	if err := db.Create(&orderdomain.OrderRefundRecord{
		UserID:    1,
		OrderID:   outsideOrder.ID,
		Type:      constants.OrderRefundTypeManual,
		Amount:    money.FromDecimal(decimal.NewFromInt(30)),
		Currency:  "CNY",
		CreatedAt: day2,
		UpdatedAt: day2,
	}).Error; err != nil {
		t.Fatalf("create refund record failed: %v", err)
	}

	rows, err := repo.GetProfitTrends(startAt, endAt)
	if err != nil {
		t.Fatalf("get profit trends failed: %v", err)
	}
	rowMap := make(map[string]dashboard.ProfitTrendRow, len(rows))
	for _, row := range rows {
		rowMap[row.Day] = row
	}
	if math.Abs(rowMap["2026-03-01"].Revenue-80) > 0.000001 || math.Abs(rowMap["2026-03-01"].Cost-30) > 0.000001 {
		t.Fatalf("unexpected 2026-03-01 row: %+v", rowMap["2026-03-01"])
	}
	if math.Abs(rowMap["2026-03-02"].Revenue-(-30)) > 0.000001 || math.Abs(rowMap["2026-03-02"].Cost-0) > 0.000001 || math.Abs(rowMap["2026-03-02"].RefundedCost-12) > 0.000001 {
		t.Fatalf("unexpected 2026-03-02 row: %+v", rowMap["2026-03-02"])
	}
}

func TestProfitMetricsIncludeZeroCostRevenueAndCalculateRefundedCost(t *testing.T) {
	repo, db := setupDashboardRepositoryTest(t)
	now := time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)

	category := createDashboardCategory(t, db, "dashboard-zero-cost-refund-category")
	product := &productdomain.Product{
		CategoryID:      category.ID,
		Slug:            "dashboard-zero-cost-refund-product",
		TitleJSON:       jsonmap.JSON{"zh-CN": "零成本退款商品"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(49)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeManual,
		IsActive:        true,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	orders := make([]*orderdomain.Order, 0, 4)
	for index := 0; index < 3; index++ {
		orders = append(orders, createDashboardProfitOrderWithItem(
			t,
			db,
			product,
			fmt.Sprintf("DJ-PROFIT-COST-ONE-%d", index+1),
			constants.OrderStatusRefunded,
			1,
			1,
			"成本一元商品",
			now,
		))
	}
	orders = append(orders, createDashboardProfitOrderWithItem(
		t,
		db,
		product,
		"DJ-PROFIT-ZERO-COST",
		constants.OrderStatusRefunded,
		49,
		0,
		"零成本商品",
		now,
	))

	for _, order := range orders {
		if err := db.Create(&orderdomain.OrderRefundRecord{
			UserID:    1,
			OrderID:   order.ID,
			Type:      constants.OrderRefundTypeManual,
			Amount:    order.TotalAmount,
			Currency:  "CNY",
			CreatedAt: now,
			UpdatedAt: now,
		}).Error; err != nil {
			t.Fatalf("create refund record failed: %v", err)
		}
	}

	startAt := now.Add(-time.Hour)
	endAt := now.Add(time.Hour)
	overview, err := repo.GetProfitOverview(startAt, endAt)
	if err != nil {
		t.Fatalf("get profit overview failed: %v", err)
	}
	if math.Abs(overview.TotalRevenue) > 0.000001 || math.Abs(overview.TotalCost-3) > 0.000001 || math.Abs(overview.RefundedCost-3) > 0.000001 {
		t.Fatalf("unexpected zero-cost refund overview: %+v", overview)
	}

	trends, err := repo.GetProfitTrends(startAt, endAt)
	if err != nil {
		t.Fatalf("get profit trends failed: %v", err)
	}
	if len(trends) != 1 {
		t.Fatalf("profit trend rows want 1 got %d", len(trends))
	}
	if math.Abs(trends[0].Revenue) > 0.000001 || math.Abs(trends[0].Cost-3) > 0.000001 || math.Abs(trends[0].RefundedCost-3) > 0.000001 {
		t.Fatalf("unexpected zero-cost refund trend: %+v", trends[0])
	}
}

func TestRefundedCostUsesParentOrderChildrenCostBasis(t *testing.T) {
	repo, db := setupDashboardRepositoryTest(t)
	now := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)

	category := createDashboardCategory(t, db, "dashboard-parent-refund-cost-category")
	product := &productdomain.Product{
		CategoryID:      category.ID,
		Slug:            "dashboard-parent-refund-cost-product",
		TitleJSON:       jsonmap.JSON{"zh-CN": "父订单退款成本商品"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(100)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeManual,
		IsActive:        true,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	parent := &orderdomain.Order{
		OrderNo:        "DJ-PROFIT-PARENT-REFUND",
		UserID:         1,
		Status:         constants.OrderStatusPartiallyRefunded,
		Currency:       "CNY",
		OriginalAmount: money.FromDecimal(decimal.NewFromInt(100)),
		TotalAmount:    money.FromDecimal(decimal.NewFromInt(100)),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := db.Create(parent).Error; err != nil {
		t.Fatalf("create parent order failed: %v", err)
	}
	children := []*orderdomain.Order{
		createDashboardProfitOrderWithItem(t, db, product, "DJ-PROFIT-PARENT-CHILD-1", constants.OrderStatusPartiallyRefunded, 40, 10, "子订单一", now),
		createDashboardProfitOrderWithItem(t, db, product, "DJ-PROFIT-PARENT-CHILD-2", constants.OrderStatusPartiallyRefunded, 60, 30, "子订单二", now),
	}
	for _, child := range children {
		if err := db.Model(&orderdomain.Order{}).Where("id = ?", child.ID).Update("parent_id", parent.ID).Error; err != nil {
			t.Fatalf("attach child order failed: %v", err)
		}
	}
	if err := db.Create(&orderdomain.OrderRefundRecord{
		UserID:    1,
		OrderID:   parent.ID,
		Type:      constants.OrderRefundTypeManual,
		Amount:    money.FromDecimal(decimal.NewFromInt(50)),
		Currency:  "CNY",
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create parent refund record failed: %v", err)
	}

	result, err := repo.GetProfitOverview(now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("get parent refund profit overview failed: %v", err)
	}
	if math.Abs(result.TotalRevenue-50) > 0.000001 || math.Abs(result.TotalCost-40) > 0.000001 || math.Abs(result.RefundedCost-20) > 0.000001 {
		t.Fatalf("unexpected parent refund metrics: %+v", result)
	}
}
