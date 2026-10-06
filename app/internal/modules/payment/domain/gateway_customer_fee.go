// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package domain

import (
	"fmt"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

// GatewayCustomerFeeConfig is display-only: these fees are added by the gateway,
// not included in the merchant request, callback amount, refund or reseller ledger.
func (channel *PaymentChannel) GatewayCustomerFeeConfig() (rate, fixed money.Amount, err error) {
	if channel == nil || !strings.EqualFold(strings.TrimSpace(channel.ProviderType), constants.PaymentProviderEpay) {
		return rate, fixed, nil
	}
	parse := func(key string, maximum decimal.Decimal, inclusive bool) (money.Amount, error) {
		raw, exists := channel.ConfigJSON[key]
		if !exists || raw == nil || raw == "" {
			return money.FromDecimal(decimal.Zero), nil
		}
		value, parseErr := decimal.NewFromString(strings.TrimSpace(fmt.Sprint(raw)))
		if parseErr != nil || value.IsNegative() || !value.Equal(value.Round(2)) || value.GreaterThan(maximum) || (!inclusive && value.Equal(maximum)) {
			return money.Amount{}, fmt.Errorf("invalid %s", key)
		}
		return money.FromDecimal(value), nil
	}
	rate, err = parse("gateway_customer_fee_rate", decimal.NewFromInt(100), true)
	if err != nil {
		return rate, fixed, err
	}
	fixed, err = parse("gateway_customer_fixed_fee", decimal.NewFromInt(10000), false)
	return rate, fixed, err
}

// SnapshotGatewayCustomerFee preserves the fee disclosed for this payment even
// if the administrator later changes the channel rate. Nil means no snapshot.
func (payment *Payment) SnapshotGatewayCustomerFee(channel *PaymentChannel) error {
	if payment == nil || channel == nil || !strings.EqualFold(strings.TrimSpace(channel.ProviderType), constants.PaymentProviderEpay) {
		return nil
	}
	rate, fixed, err := channel.GatewayCustomerFeeConfig()
	if err != nil {
		return err
	}
	payment.GatewayCustomerFeeRate = &rate
	payment.GatewayCustomerFixedFee = &fixed
	payment.RefreshGatewayCustomerFeeEstimate()
	return nil
}

// RefreshGatewayCustomerFeeEstimate also handles gateways that convert currency
// during creation. It uses only the immutable rate snapshot and final Amount.
func (payment *Payment) RefreshGatewayCustomerFeeEstimate() {
	if payment == nil || payment.GatewayCustomerFeeRate == nil || payment.GatewayCustomerFixedFee == nil {
		return
	}
	fee := money.FromDecimal(payment.Amount.Decimal.Mul(payment.GatewayCustomerFeeRate.Decimal).Div(decimal.NewFromInt(100)).Add(payment.GatewayCustomerFixedFee.Decimal))
	total := money.FromDecimal(payment.Amount.Decimal.Add(fee.Decimal))
	payment.GatewayCustomerFeeAmount = &fee
	payment.GatewayCustomerPayableAmount = &total
}
