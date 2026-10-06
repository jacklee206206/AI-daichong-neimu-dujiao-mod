// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package domain

import (
	"regexp"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// CryptoTokenSnapshotKey is local display metadata, never a gateway instruction.
const CryptoTokenSnapshotKey = "_dujiao_next_crypto_token_id"

var cryptoTokenIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)+$`)

func normalizeCryptoTokenID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > 128 || !cryptoTokenIDPattern.MatchString(value) {
		return ""
	}
	return value
}

// CryptoTokenID exposes only an unambiguous, configured DujiaoPay payment method.
// An unrestricted or multi-method cashier must not be described as one network.
func (channel *PaymentChannel) CryptoTokenID() string {
	if channel == nil || !strings.EqualFold(strings.TrimSpace(channel.ProviderType), constants.PaymentProviderDujiaoPay) {
		return ""
	}
	read := func(key string) string {
		value, _ := channel.ConfigJSON[key].(string)
		return strings.ToLower(strings.TrimSpace(value))
	}
	switch read("order_mode") {
	case constants.PaymentDujiaoPayOrderModeCashier:
		var unique string
		// Match the gateway's normalization and de-duplication of this list.
		for _, value := range strings.Split(strings.ReplaceAll(read("allowed_methods"), " ", ""), ",") {
			if strings.TrimSpace(value) == "" {
				continue
			}
			tokenID := normalizeCryptoTokenID(value)
			if tokenID == "" || (unique != "" && unique != tokenID) {
				return ""
			}
			unique = tokenID
		}
		return unique
	case "", constants.PaymentDujiaoPayOrderModeTransaction:
		tokenID := read("token_id")
		if tokenID == "" {
			tokenID = channel.ChannelType
		}
		return normalizeCryptoTokenID(tokenID)
	default:
		return ""
	}
}

// SnapshotCryptoTokenID records the method restriction used for this new payment.
// Current channel configuration must never be used to label historical payments.
func (payment *Payment) SnapshotCryptoTokenID(channel *PaymentChannel) {
	if payment == nil {
		return
	}
	delete(payment.ProviderPayload, CryptoTokenSnapshotKey)
	if !strings.EqualFold(strings.TrimSpace(payment.ProviderType), constants.PaymentProviderDujiaoPay) {
		return
	}
	if tokenID := channel.CryptoTokenID(); tokenID != "" {
		if payment.ProviderPayload == nil {
			payment.ProviderPayload = jsonmap.JSON{}
		}
		payment.ProviderPayload[CryptoTokenSnapshotKey] = tokenID
	}
}

// CryptoTokenID reads the creation snapshot only; actual chain data is separate.
func (payment *Payment) CryptoTokenID() string {
	if payment == nil || !strings.EqualFold(strings.TrimSpace(payment.ProviderType), constants.PaymentProviderDujiaoPay) {
		return ""
	}
	value, _ := payment.ProviderPayload[CryptoTokenSnapshotKey].(string)
	return normalizeCryptoTokenID(value)
}
