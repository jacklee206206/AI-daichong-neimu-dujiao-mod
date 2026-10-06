// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package productwrite

import (
	"errors"
	"strconv"
	"time"

	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	"github.com/shopspring/decimal"
)

type APIPriceInput struct {
	ID                uint
	SupplyPriceAmount decimal.Decimal
}

// UpdateAPIPrices changes independent API supply prices atomically.
func (s *WriteService) UpdateAPIPrices(id string, inputs []APIPriceInput) error {
	productID, err := strconv.ParseUint(id, 10, 32)
	if err != nil || productID == 0 || len(inputs) == 0 || len(inputs) > 500 {
		return productcontract.ErrProductSKUInvalid
	}
	seen := map[uint]bool{}
	limit := decimal.New(1, 18)
	for _, input := range inputs {
		if input.ID == 0 || seen[input.ID] {
			return productcontract.ErrProductSKUInvalid
		}
		seen[input.ID] = true
		for _, amount := range []decimal.Decimal{input.SupplyPriceAmount} {
			if amount.IsNegative() || !amount.Equal(amount.Round(2)) || amount.GreaterThanOrEqual(limit) {
				return productcontract.ErrProductPriceInvalid
			}
		}
	}
	if s == nil || s.transactions == nil {
		return errors.New("product write transaction is unavailable")
	}
	return s.transactions.WithinTransaction(func(repositories TransactionRepositories) error {
		product, err := repositories.Products.GetByID(id)
		if err != nil {
			return err
		}
		if product == nil {
			return productcontract.ErrNotFound
		}
		if repositories.SKUs == nil {
			return productcontract.ErrProductSKUInvalid
		}
		rows, err := repositories.SKUs.ListByProduct(uint(productID), false)
		if err != nil {
			return err
		}
		owned := make(map[uint]bool, len(rows))
		for _, row := range rows {
			owned[row.ID] = true
		}
		// Validate the entire batch before the first mutation; omitted SKUs stay intact.
		for _, input := range inputs {
			if !owned[input.ID] {
				return productcontract.ErrProductSKUInvalid
			}
		}
		for _, input := range inputs {
			if err := repositories.SKUs.UpdateAPIPrice(uint(productID), input.ID, input.SupplyPriceAmount); err != nil {
				return err
			}
		}
		// Incremental API catalog sync watches product timestamps.
		return repositories.Products.QuickUpdate(id, map[string]interface{}{"updated_at": time.Now()})
	})
}
