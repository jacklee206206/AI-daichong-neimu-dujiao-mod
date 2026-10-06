// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"context"
	"testing"
	"time"

	dashboardcontract "github.com/dujiao-next/internal/modules/dashboard/contract"
	reportingdomain "github.com/dujiao-next/internal/modules/reporting/domain"
	settingsstorefront "github.com/dujiao-next/internal/modules/settings/schema/storefront"
)

type dashboardServiceRepoStub struct {
	overview       dashboardcontract.OverviewRow
	profitOverview dashboardcontract.ProfitOverviewRow
	profitTrends   []dashboardcontract.ProfitTrendRow
	stock          dashboardcontract.StockStatsRow
	products       []dashboardcontract.ProductRankingRow
}

func (s dashboardServiceRepoStub) GetOverview(startAt, endAt time.Time) (dashboardcontract.OverviewRow, error) {
	return s.overview, nil
}

func (s dashboardServiceRepoStub) GetPaymentOrderAlertCounts(startAt, endAt time.Time) (dashboardcontract.PaymentOrderAlertCountsRow, error) {
	return dashboardcontract.PaymentOrderAlertCountsRow{}, nil
}

func (s dashboardServiceRepoStub) GetOrderTrends(startAt, endAt time.Time) ([]dashboardcontract.OrderTrendRow, error) {
	return []dashboardcontract.OrderTrendRow{}, nil
}

func (s dashboardServiceRepoStub) GetPaymentTrends(startAt, endAt time.Time) ([]dashboardcontract.PaymentTrendRow, error) {
	return []dashboardcontract.PaymentTrendRow{}, nil
}

func (s dashboardServiceRepoStub) GetStockStats(lowStockThreshold int64) (dashboardcontract.StockStatsRow, error) {
	return s.stock, nil
}

func (s dashboardServiceRepoStub) GetInventoryAlertItems(lowStockThreshold int64) ([]dashboardcontract.InventoryAlertRow, error) {
	return []dashboardcontract.InventoryAlertRow{}, nil
}

func (s dashboardServiceRepoStub) GetTopProducts(startAt, endAt time.Time, limit int) ([]dashboardcontract.ProductRankingRow, error) {
	return s.products, nil
}

func (s dashboardServiceRepoStub) GetProfitOverview(startAt, endAt time.Time) (dashboardcontract.ProfitOverviewRow, error) {
	return s.profitOverview, nil
}

func (s dashboardServiceRepoStub) GetProfitTrends(startAt, endAt time.Time) ([]dashboardcontract.ProfitTrendRow, error) {
	return s.profitTrends, nil
}

func (s dashboardServiceRepoStub) GetTopChannels(startAt, endAt time.Time, limit int) ([]dashboardcontract.ChannelRankingRow, error) {
	return []dashboardcontract.ChannelRankingRow{}, nil
}

func (s dashboardServiceRepoStub) GetTotalUserBalance() (float64, error) {
	return 0, nil
}

type dashboardSettingReaderStub struct {
	setting settingsstorefront.DashboardSetting
}

func (s dashboardSettingReaderStub) GetDashboardSetting() (settingsstorefront.DashboardSetting, error) {
	return s.setting, nil
}

func TestDashboardOverviewUsesPaidOrdersForPaymentConversionRate(t *testing.T) {
	service := NewService(dashboardServiceRepoStub{
		overview: dashboardcontract.OverviewRow{
			OrdersTotal:     10,
			PaidOrders:      6,
			CompletedOrders: 3,
			PaymentsTotal:   5,
			PaymentsSuccess: 4,
			Currency:        "cny",
			GMVPaid:         120,
		},
		stock: dashboardcontract.StockStatsRow{},
	}, nil)

	response, err := service.GetOverview(context.Background(), reportingdomain.Query{
		Range:    "today",
		Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("get overview failed: %v", err)
	}
	if response.Currency != "CNY" {
		t.Fatalf("currency want CNY got %s", response.Currency)
	}
	if response.Funnel.PaymentConversionRate != "60.00" {
		t.Fatalf("payment conversion rate want 60.00 got %s", response.Funnel.PaymentConversionRate)
	}
	if response.KPI.PaymentSuccessRate != "80.00" {
		t.Fatalf("payment success rate want 80.00 got %s", response.KPI.PaymentSuccessRate)
	}
}

func TestDashboardOverviewBuildsInventoryAlertsFromStockStats(t *testing.T) {
	service := NewService(dashboardServiceRepoStub{
		overview: dashboardcontract.OverviewRow{
			PendingPaymentOrders: 25,
			PaymentsFailed:       12,
		},
		stock: dashboardcontract.StockStatsRow{
			OutOfStockProducts: 2,
			LowStockProducts:   1,
		},
	}, nil)

	response, err := service.GetOverview(context.Background(), reportingdomain.Query{
		Range:    "today",
		Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("get overview failed: %v", err)
	}
	if len(response.Alerts) != 4 {
		t.Fatalf("alerts len want 4 got %d", len(response.Alerts))
	}
	if response.Alerts[0].Type != "out_of_stock_products" || response.Alerts[0].Value != 2 {
		t.Fatalf("unexpected first alert: %+v", response.Alerts[0])
	}
}

func TestDashboardOverviewAppliesRefundCostPolicy(t *testing.T) {
	repo := dashboardServiceRepoStub{
		profitOverview: dashboardcontract.ProfitOverviewRow{
			TotalRevenue: 0,
			TotalCost:    3,
			RefundedCost: 3,
		},
	}
	query := reportingdomain.Query{
		Range:        "today",
		Timezone:     "Asia/Shanghai",
		ForceRefresh: true,
	}

	withoutReversal, err := NewService(repo, nil).GetOverview(context.Background(), query)
	if err != nil {
		t.Fatalf("get overview without cost reversal failed: %v", err)
	}
	if withoutReversal.KPI.TotalCost != "3.00" || withoutReversal.KPI.TotalProfit != "-3.00" {
		t.Fatalf("unexpected metrics without cost reversal: %+v", withoutReversal.KPI)
	}

	setting := settingsstorefront.DefaultDashboardSetting()
	setting.Accounting.RefundReversesCost = true
	withReversal, err := NewService(repo, dashboardSettingReaderStub{setting: setting}).GetOverview(context.Background(), query)
	if err != nil {
		t.Fatalf("get overview with cost reversal failed: %v", err)
	}
	if withReversal.KPI.TotalCost != "0.00" || withReversal.KPI.TotalProfit != "0.00" {
		t.Fatalf("unexpected metrics with cost reversal: %+v", withReversal.KPI)
	}
}

func TestEffectiveDashboardCostSupportsCrossPeriodRefundAdjustment(t *testing.T) {
	if got := effectiveDashboardCost(20, 12, false); got != 20 {
		t.Fatalf("disabled cost reversal want 20 got %.2f", got)
	}
	if got := effectiveDashboardCost(20, 12, true); got != 8 {
		t.Fatalf("enabled cost reversal want 8 got %.2f", got)
	}
	if got := effectiveDashboardCost(0, 12, true); got != -12 {
		t.Fatalf("cross-period cost reversal want -12 got %.2f", got)
	}
}

func TestDashboardProfitDeductsPaymentFees(t *testing.T) {
	service := NewService(dashboardServiceRepoStub{
		overview: dashboardcontract.OverviewRow{Currency: "CNY"},
		profitOverview: dashboardcontract.ProfitOverviewRow{
			TotalRevenue: 100,
			TotalCost:    40,
			PaymentFee:   3,
		},
	}, nil)

	response, err := service.GetOverview(context.Background(), reportingdomain.Query{
		Range:    "today",
		Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("get overview failed: %v", err)
	}
	if response.KPI.TotalCost != "43.00" || response.KPI.PaymentFee != "3.00" || response.KPI.TotalProfit != "57.00" || response.KPI.ProfitMargin != "57.00" {
		t.Fatalf("unexpected profit KPI: %+v", response.KPI)
	}
}

func TestDashboardProfitCombinesRefundCostReversalAndPaymentFees(t *testing.T) {
	setting := settingsstorefront.DefaultDashboardSetting()
	setting.Accounting.RefundReversesCost = true
	service := NewService(dashboardServiceRepoStub{
		overview: dashboardcontract.OverviewRow{Currency: "CNY"},
		profitOverview: dashboardcontract.ProfitOverviewRow{
			TotalRevenue: 100,
			TotalCost:    40,
			RefundedCost: 10,
			PaymentFee:   3,
		},
	}, dashboardSettingReaderStub{setting: setting})

	response, err := service.GetOverview(context.Background(), reportingdomain.Query{
		Range:    "today",
		Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("get overview failed: %v", err)
	}
	if response.KPI.TotalCost != "33.00" || response.KPI.PaymentFee != "3.00" || response.KPI.TotalProfit != "67.00" || response.KPI.ProfitMargin != "67.00" {
		t.Fatalf("unexpected combined profit KPI: %+v", response.KPI)
	}
}

var _ dashboardcontract.Repository = dashboardServiceRepoStub{}
var _ dashboardcontract.SettingReader = dashboardSettingReaderStub{}

func TestDashboardOwnerProfitUsesAgreedCostAndDeductsResellerMargin(t *testing.T) {
	// All product names and amounts below are fictional accounting fixtures.
	for _, tc := range []struct {
		name                                                        string
		revenue, cost, commission, refundedCost, refundedCommission float64
		reverseCost                                                 bool
		wantProfit, wantCost, wantCommission                        string
	}{
		{"main_fictional_sample_a", 54, 40, 0, 0, 0, false, "14.00", "40.00", "0.00"},
		{"main_fictional_sample_a_coupon", 50, 40, 0, 0, 0, false, "10.00", "40.00", "0.00"},
		{"reseller_fictional_sample_a", 55, 40, 6, 0, 0, false, "9.00", "46.00", "6.00"},
		{"reseller_fictional_sample_b", 208, 140, 28, 0, 0, false, "40.00", "168.00", "28.00"},
		{"reseller_fictional_sample_c", 410, 240, 100, 0, 0, false, "70.00", "340.00", "100.00"},
		{"half_refund_cost_returned", 27.5, 40, 6, 20, 3, true, "4.50", "23.00", "3.00"},
		{"half_refund_cost_not_returned", 27.5, 40, 6, 20, 3, false, "-15.50", "43.00", "3.00"},
		{"refund_in_later_period", -27.5, 0, 0, 20, 3, true, "-4.50", "-23.00", "-3.00"},
		{"full_refund", 0, 40, 6, 40, 6, true, "0.00", "0.00", "0.00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setting := settingsstorefront.DefaultDashboardSetting()
			setting.Accounting.RefundReversesCost = tc.reverseCost
			day := time.Now().UTC().Format("2006-01-02")
			service := NewService(dashboardServiceRepoStub{
				overview:       dashboardcontract.OverviewRow{Currency: "CNY"},
				profitOverview: dashboardcontract.ProfitOverviewRow{TotalRevenue: tc.revenue, TotalCost: tc.cost, ResellerProfit: tc.commission, RefundedCost: tc.refundedCost, RefundedResellerProfit: tc.refundedCommission},
				profitTrends:   []dashboardcontract.ProfitTrendRow{{Day: day, Revenue: tc.revenue, Cost: tc.cost, ResellerProfit: tc.commission, RefundedCost: tc.refundedCost, RefundedResellerProfit: tc.refundedCommission}},
				products:       []dashboardcontract.ProductRankingRow{{ProductID: 1, PaidAmount: tc.revenue, TotalCost: tc.cost, RefundedCost: tc.refundedCost, ResellerProfit: tc.commission - tc.refundedCommission}},
			}, dashboardSettingReaderStub{setting: setting})
			query := reportingdomain.Query{Range: "today", Timezone: "UTC", ForceRefresh: true}
			overview, err := service.GetOverview(context.Background(), query)
			if err != nil {
				t.Fatal(err)
			}
			if overview.KPI.TotalProfit != tc.wantProfit || overview.KPI.TotalCost != tc.wantCost || overview.KPI.ResellerProfit != tc.wantCommission {
				t.Fatalf("unexpected KPI: %+v", overview.KPI)
			}
			trend, err := service.GetTrends(context.Background(), query)
			if err != nil {
				t.Fatal(err)
			}
			if len(trend.Points) != 1 || trend.Points[0].Profit != tc.wantProfit {
				t.Fatalf("unexpected trends: %+v", trend.Points)
			}
			rankings, err := service.GetRankings(context.Background(), query)
			if err != nil {
				t.Fatal(err)
			}
			if len(rankings.TopProducts) != 1 || rankings.TopProducts[0].Profit != tc.wantProfit || rankings.TopProducts[0].ResellerProfit != tc.wantCommission {
				t.Fatalf("unexpected ranking: %+v", rankings.TopProducts)
			}
		})
	}
}
