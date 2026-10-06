// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"context"
	"github.com/dujiao-next/internal/constants"
	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
	"testing"
)

func TestGatewayCustomerFeeDisclosureDoesNotIncreaseGatewayCharge(t *testing.T) {
	svc, db := setupPaymentServiceWalletTest(t)
	channel, order := createFeePolicyOrderFixture(t, db, "External customer fee", "DJ-EXTERNAL-FEE")
	channel.ProviderType = constants.PaymentProviderEpay
	channel.ChannelType = constants.PaymentChannelTypeAlipay
	channel.FeeRate = money.FromDecimal(decimal.Zero)
	channel.FixedFee = money.FromDecimal(decimal.Zero)
	channel.ConfigJSON = jsonmap.JSON{"gateway_customer_fee_rate": "3.20", "gateway_customer_fixed_fee": "0"}
	if err := db.Save(channel).Error; err != nil {
		t.Fatal(err)
	}
	var gatewayAmount money.Amount
	registerTestGateway(t, svc, channel.ProviderType, channel.ChannelType, emptyProviderRefProvider{onCreate: func(input paymentcontract.GatewayCreateInput) { gatewayAmount = input.Amount }})
	result, err := svc.CreatePayment(CreatePaymentInput{OrderID: order.ID, ChannelID: channel.ID, Context: context.Background()})
	if err != nil {
		t.Fatal(err)
	}
	if gatewayAmount.StringFixed(2) != "100.00" || result.Payment.Amount.StringFixed(2) != "100.00" {
		t.Fatal("external display fee was charged by merchant")
	}
	var saved paymentdomain.Payment
	if err := db.First(&saved, result.Payment.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.GatewayCustomerFeeAmount == nil || saved.GatewayCustomerFeeAmount.StringFixed(2) != "3.20" || saved.GatewayCustomerPayableAmount.StringFixed(2) != "103.20" {
		t.Fatal("fee snapshot missing or incorrect")
	}
	channel.ConfigJSON["gateway_customer_fee_rate"] = "1.00"
	if err := db.Save(channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&saved, result.Payment.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.GatewayCustomerFeeRate.StringFixed(2) != "3.20" {
		t.Fatal("historical fee changed")
	}
}

func TestGatewayCustomerFeeValidationAndRounding(t *testing.T) {
	channel := &paymentdomain.PaymentChannel{ProviderType: constants.PaymentProviderEpay, ConfigJSON: jsonmap.JSON{"gateway_customer_fee_rate": "3.20"}}
	p := &paymentdomain.Payment{Amount: money.FromDecimal(decimal.RequireFromString("124"))}
	if err := p.SnapshotGatewayCustomerFee(channel); err != nil {
		t.Fatal(err)
	}
	if p.GatewayCustomerFeeAmount.StringFixed(2) != "3.97" || p.GatewayCustomerPayableAmount.StringFixed(2) != "127.97" {
		t.Fatal("incorrect cent rounding")
	}
	for _, invalid := range []string{"-1", "NaN", "101", "1.234"} {
		channel.ConfigJSON["gateway_customer_fee_rate"] = invalid
		if _, _, err := channel.GatewayCustomerFeeConfig(); err == nil {
			t.Fatalf("accepted invalid rate %s", invalid)
		}
	}
}
