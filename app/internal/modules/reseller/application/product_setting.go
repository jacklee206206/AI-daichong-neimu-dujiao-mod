// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"errors"
	"strconv"
	"strings"

	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	"github.com/dujiao-next/internal/shared/jsonslice"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

// ProductSettingService 分销商品改价/上架配置用例。
type ProductSettingService struct {
	store       resellercontract.ProductSettingStore
	productRepo productcontract.Repository
}

func NewProductSettingService(store resellercontract.ProductSettingStore, productRepo productcontract.Repository) *ProductSettingService {
	return &ProductSettingService{store: store, productRepo: productRepo}
}

type ProductSettingInput struct {
	SupplyPriceAmount *decimal.Decimal
	MinPriceAmount    *decimal.Decimal
	SKUID             uint
	IsListed          bool
	PricingMode       string
	MarkupPercent     decimal.Decimal
	FixedMarkupAmount decimal.Decimal
	FixedPriceAmount  decimal.Decimal
	SortOrder         int
}

type ProductSettingSaveInput struct {
	Settings []ProductSettingInput
}

type ProductSettingUserListInput struct {
	Page       int
	PageSize   int
	Keyword    string
	CategoryID uint
	Configured string
	Listed     string
}

type ProductSettingAdminListInput struct {
	Page        int
	PageSize    int
	ResellerID  uint
	UserID      uint
	ProductID   uint
	Keyword     string
	PricingMode string
	Configured  string
	Listed      string
}

type ProductSettingDetail struct {
	Profile          *resellerdomain.Profile
	Product          productdomain.Product
	Settings         []resellerdomain.ProductSetting
	EffectiveBySKUID map[uint]decimal.Decimal
	RuleBySKUID      map[uint]string
}

type ProductSettingListRow struct {
	Profile          *resellerdomain.Profile
	Product          productdomain.Product
	Settings         []resellerdomain.ProductSetting
	EffectiveBySKUID map[uint]decimal.Decimal
	RuleBySKUID      map[uint]string
}

// ProductSettingPreviewItem 表示某个商品级（SKUID=0）或 SKU 级规则在「拟用配置」下的预览结果。
// 复用与保存/下单完全一致的 ResolveUnitAmount + ValidateUnitAmount，确保预览价与实际成交价零分歧。
type ProductSettingPreviewItem struct {
	SKUID          uint
	IsListed       bool
	BasePrice      decimal.Decimal
	MinPrice       decimal.Decimal
	EffectivePrice decimal.Decimal
	Valid          bool
	ErrorCode      string
}

func (s *ProductSettingService) ListUserProductSettings(userID uint, input ProductSettingUserListInput) ([]ProductSettingListRow, int64, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, 0, err
	}
	rows, total, err := s.store.ListProductsWithSettings(resellercontract.ProductSettingListFilter{
		Page:       input.Page,
		PageSize:   input.PageSize,
		ResellerID: profile.ID,
		CategoryID: input.CategoryID,
		Keyword:    input.Keyword,
		Configured: input.Configured,
		Listed:     input.Listed,
		OnlyActive: true,
	})
	if err != nil {
		return nil, 0, err
	}
	return s.decorateRows(profile, rows), total, nil
}

func (s *ProductSettingService) GetUserProductSetting(userID, productID uint) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

// PreviewUserProductSettings 在不落库的前提下，按用户拟用的定价规则计算各 SKU 的预计生效价与校验结果。
func (s *ProductSettingService) PreviewUserProductSettings(userID, productID uint, input ProductSettingSaveInput) ([]ProductSettingPreviewItem, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, err
	}
	return s.previewSettings(profile, productID, input, false)
}

func (s *ProductSettingService) SaveUserProductSettings(userID, productID uint, input ProductSettingSaveInput) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, err
	}
	if err := s.saveSettings(profile, productID, input, false); err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

func (s *ProductSettingService) ResetUserProductSetting(userID, productID, skuID uint) error {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return err
	}
	return s.resetSetting(profile, productID, skuID, false)
}

func (s *ProductSettingService) ListAdminSettings(input ProductSettingAdminListInput) ([]resellerdomain.ProductSetting, int64, error) {
	return s.store.ListAdminSettings(resellercontract.ProductSettingAdminListFilter(input))
}

func (s *ProductSettingService) SummarizeAdminSettings(resellerID uint) (resellercontract.ProductSettingSummary, error) {
	if s == nil || s.store == nil || resellerID == 0 {
		return resellercontract.ProductSettingSummary{}, nil
	}
	return s.store.SummarizeByResellerID(resellerID)
}

func (s *ProductSettingService) GetAdminProductSetting(resellerID, productID uint) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

// PreviewAdminProductSettings 在不落库的前提下，按管理员拟用的定价规则计算各 SKU 的预计生效价与校验结果。
func (s *ProductSettingService) PreviewAdminProductSettings(resellerID, productID uint, input ProductSettingSaveInput) ([]ProductSettingPreviewItem, error) {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	return s.previewSettings(profile, productID, input, true)
}

func (s *ProductSettingService) SaveAdminProductSettings(resellerID, productID uint, input ProductSettingSaveInput) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	if err := s.saveSettings(profile, productID, input, true); err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

func (s *ProductSettingService) ResetAdminProductSetting(resellerID, productID, skuID uint) error {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return err
	}
	return s.resetSetting(profile, productID, skuID, true)
}

func (s *ProductSettingService) requireActiveProfileByUser(userID uint) (*resellerdomain.Profile, error) {
	if s == nil || s.store == nil || s.productRepo == nil || userID == 0 {
		return nil, productcontract.ErrNotFound
	}
	profile, err := s.store.GetProfileByUserID(userID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, resellercontract.ErrNotOpened
	}
	if profile.Status != resellerdomain.ProfileStatusActive {
		return nil, resellercontract.ErrProfileInactive
	}
	return profile, nil
}

func (s *ProductSettingService) requireActiveProfileByID(resellerID uint) (*resellerdomain.Profile, error) {
	if s == nil || s.store == nil || s.productRepo == nil || resellerID == 0 {
		return nil, productcontract.ErrNotFound
	}
	profile, err := s.store.GetProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, productcontract.ErrNotFound
	}
	if profile.Status != resellerdomain.ProfileStatusActive {
		return nil, resellercontract.ErrProfileInactive
	}
	return profile, nil
}

func (s *ProductSettingService) getDetail(profile *resellerdomain.Profile, productID uint) (*ProductSettingDetail, error) {
	if profile == nil || productID == 0 {
		return nil, productcontract.ErrNotFound
	}
	row, err := s.store.GetProductWithSettings(profile.ID, productID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, productcontract.ErrNotFound
	}
	effective, rules, err := computeProductEffectivePrices(profile, row.Product, row.Settings)
	if err != nil {
		return nil, err
	}
	return &ProductSettingDetail{Profile: profile, Product: row.Product, Settings: row.Settings, EffectiveBySKUID: effective, RuleBySKUID: rules}, nil
}

func (s *ProductSettingService) decorateRows(profile *resellerdomain.Profile, rows []resellercontract.ProductSettingProductRow) []ProductSettingListRow {
	out := make([]ProductSettingListRow, 0, len(rows))
	for _, row := range rows {
		effective, rules, _ := computeProductEffectivePrices(profile, row.Product, row.Settings)
		out = append(out, ProductSettingListRow{Profile: profile, Product: row.Product, Settings: row.Settings, EffectiveBySKUID: effective, RuleBySKUID: rules})
	}
	return out
}

// mergeSettings retains administrator supply agreements and image overrides when
// a reseller updates only its retail rule. All candidate rules are validated
// together, so a product default and SKU exception use the same checkout path.
func mergeSettings(profile *resellerdomain.Profile, product *productdomain.Product, existing []resellerdomain.ProductSetting, input ProductSettingSaveInput, admin bool) ([]resellerdomain.ProductSetting, error) {
	bySKU := map[uint]resellerdomain.ProductSetting{}
	for _, row := range existing {
		bySKU[row.SKUID] = row
	}
	seen := map[uint]bool{}
	for _, item := range input.Settings {
		if seen[item.SKUID] {
			return nil, resellercontract.ErrPricingModeInvalid
		}
		seen[item.SKUID] = true
		if !admin && (item.SupplyPriceAmount != nil || item.MinPriceAmount != nil) {
			return nil, resellercontract.ErrSupplyPriceForbidden
		}
		if item.SKUID > 0 && findProductSKU(product.SKUs, item.SKUID) == nil {
			return nil, productcontract.ErrProductSKUInvalid
		}
		row, err := buildPreviewSetting(item)
		if err != nil {
			return nil, err
		}
		old := bySKU[item.SKUID]
		row.ID, row.ResellerID, row.ProductID = old.ID, profile.ID, product.ID
		row.SupplyPriceAmount, row.MinPriceAmount, row.ImagesJSON = old.SupplyPriceAmount, old.MinPriceAmount, old.ImagesJSON
		for _, amount := range []*decimal.Decimal{item.SupplyPriceAmount, item.MinPriceAmount} {
			if amount != nil && amount.IsNegative() {
				return nil, resellercontract.ErrPriceBelowBase
			}
		}
		if item.SupplyPriceAmount != nil {
			row.SupplyPriceAmount = money.FromDecimal(item.SupplyPriceAmount.Round(2))
		}
		if item.MinPriceAmount != nil {
			row.MinPriceAmount = money.FromDecimal(item.MinPriceAmount.Round(2))
		}
		bySKU[item.SKUID] = row
	}
	rows := make([]resellerdomain.ProductSetting, 0, len(bySKU))
	for _, row := range bySKU {
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *ProductSettingService) saveSettings(profile *resellerdomain.Profile, productID uint, input ProductSettingSaveInput, admin bool) error {
	if profile == nil || productID == 0 {
		return productcontract.ErrNotFound
	}
	product, err := s.productRepo.GetAdminByID(strconv.FormatUint(uint64(productID), 10))
	if err != nil {
		return err
	}
	if product == nil {
		return productcontract.ErrNotFound
	}
	return s.store.WithinProductSettingTransaction(func(store resellercontract.ProductSettingStore) error {
		if err := store.LockProductSettings(profile.ID); err != nil {
			return err
		}
		existing, err := store.GetProductWithSettings(profile.ID, productID)
		if err != nil {
			return err
		}
		if existing == nil {
			return productcontract.ErrNotFound
		}
		rows, err := mergeSettings(profile, product, existing.Settings, input, admin)
		if err != nil {
			return err
		}
		previews := previewMergedSettings(profile, *product, rows)
		for _, item := range previews {
			if item.IsListed && !item.Valid {
				if item.ErrorCode == "markup_exceeded" {
					return resellercontract.ErrMarkupExceeded
				}
				return resellercontract.ErrPriceBelowBase
			}
		}
		changed := map[uint]bool{}
		for _, item := range input.Settings {
			changed[item.SKUID] = true
		}
		for _, row := range rows {
			if changed[row.SKUID] {
				if _, err := store.UpsertSetting(row); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *ProductSettingService) resetSetting(profile *resellerdomain.Profile, productID, skuID uint, admin bool) error {
	return s.store.WithinProductSettingTransaction(func(store resellercontract.ProductSettingStore) error {
		if err := store.LockProductSettings(profile.ID); err != nil {
			return err
		}
		row, err := store.GetProductWithSettings(profile.ID, productID)
		if err != nil {
			return err
		}
		if row == nil {
			return productcontract.ErrNotFound
		}
		for _, setting := range row.Settings {
			if setting.SKUID == skuID {
				setting.PricingMode = resellerdomain.PricingModeInherit
				setting.IsListed = true
				setting.MarkupPercent, setting.FixedMarkupAmount, setting.FixedPriceAmount = money.FromDecimal(decimal.Zero), money.FromDecimal(decimal.Zero), money.FromDecimal(decimal.Zero)
				if admin {
					setting.SupplyPriceAmount, setting.MinPriceAmount = money.FromDecimal(decimal.Zero), money.FromDecimal(decimal.Zero)
				}
				if setting.SupplyPriceAmount.Decimal.IsZero() && setting.MinPriceAmount.Decimal.IsZero() && len(setting.ImagesJSON) == 0 {
					return store.DeleteSetting(profile.ID, productID, skuID)
				}
				_, err = store.UpsertSetting(setting)
				return err
			}
		}
		return nil
	})
}

// SaveUserProductImages changes only this reseller's product artwork. An empty
// list restores platform images while retaining price and supply agreements.
func (s *ProductSettingService) SaveUserProductImages(userID, productID uint, images []string) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, err
	}
	if len(images) > 20 {
		return nil, resellercontract.ErrProductImagesInvalid
	}
	clean := make(jsonslice.Strings, 0, len(images))
	seen := map[string]bool{}
	for _, image := range images {
		if len(image) > 500 {
			return nil, resellercontract.ErrProductImagesInvalid
		}
		value, err := validateHTTPOrUploadPath(image)
		if err != nil || value == "" || strings.ContainsAny(value, "\r\n\\") {
			return nil, resellercontract.ErrProductImagesInvalid
		}
		if !seen[value] {
			clean = append(clean, value)
			seen[value] = true
		}
	}
	err = s.store.WithinProductSettingTransaction(func(store resellercontract.ProductSettingStore) error {
		if err := store.LockProductSettings(profile.ID); err != nil {
			return err
		}
		row, err := store.GetProductWithSettings(profile.ID, productID)
		if err != nil {
			return err
		}
		if row == nil || !row.Product.IsActive {
			return productcontract.ErrNotFound
		}
		setting := resellerdomain.ProductSetting{ResellerID: profile.ID, ProductID: productID, SKUID: 0, IsListed: true, PricingMode: resellerdomain.PricingModeInherit}
		for _, current := range row.Settings {
			if current.SKUID == 0 {
				setting = current
				break
			}
		}
		setting.ImagesJSON = clean
		_, err = store.UpsertSetting(setting)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

func normalizeProductSettingInput(profile *resellerdomain.Profile, product *productdomain.Product, input ProductSettingInput) (resellerdomain.ProductSetting, error) {
	if product == nil {
		return resellerdomain.ProductSetting{}, productcontract.ErrNotFound
	}
	mode := strings.TrimSpace(input.PricingMode)
	if mode == "" {
		mode = resellerdomain.PricingModeInherit
	}
	switch mode {
	case resellerdomain.PricingModeInherit, resellerdomain.PricingModeMarkupPercent, resellerdomain.PricingModeFixedMarkup, resellerdomain.PricingModeFixedPrice:
	default:
		return resellerdomain.ProductSetting{}, resellercontract.ErrPricingModeInvalid
	}
	setting := resellerdomain.ProductSetting{
		SKUID:             input.SKUID,
		IsListed:          input.IsListed,
		PricingMode:       mode,
		MarkupPercent:     money.FromDecimal(input.MarkupPercent.Round(2)),
		FixedMarkupAmount: money.FromDecimal(input.FixedMarkupAmount.Round(2)),
		FixedPriceAmount:  money.FromDecimal(input.FixedPriceAmount.Round(2)),
		SortOrder:         input.SortOrder,
	}
	if !setting.IsListed {
		return setting, nil
	}
	if input.SKUID > 0 {
		sku := findProductSKU(product.SKUs, input.SKUID)
		if sku == nil || !sku.IsActive {
			return resellerdomain.ProductSetting{}, productcontract.ErrProductSKUInvalid
		}
		price, _, err := ResolveUnitAmount(profile, nil, &setting, sku.PriceAmount.Decimal.Round(2))
		if err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		if err := ValidateUnitAmount(profile, sku, sku.PriceAmount.Decimal.Round(2), price); err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		return setting, nil
	}
	if len(product.SKUs) == 0 {
		basePrice := product.PriceAmount.Decimal.Round(2)
		price, _, err := ResolveUnitAmount(profile, &setting, nil, basePrice)
		if err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		if err := ValidateUnitAmount(profile, nil, basePrice, price); err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		costPrice := product.CostPriceAmount.Decimal.Round(2)
		if costPrice.GreaterThan(decimal.Zero) && price.LessThan(costPrice) {
			return resellerdomain.ProductSetting{}, resellercontract.ErrPriceBelowBase
		}
		return setting, nil
	}
	for i := range product.SKUs {
		sku := &product.SKUs[i]
		if !sku.IsActive {
			continue
		}
		price, _, err := ResolveUnitAmount(profile, &setting, nil, sku.PriceAmount.Decimal.Round(2))
		if err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		if err := ValidateUnitAmount(profile, sku, sku.PriceAmount.Decimal.Round(2), price); err != nil {
			return resellerdomain.ProductSetting{}, err
		}
	}
	return setting, nil
}

func (s *ProductSettingService) previewSettings(profile *resellerdomain.Profile, productID uint, input ProductSettingSaveInput, admin bool) ([]ProductSettingPreviewItem, error) {
	if profile == nil || productID == 0 {
		return nil, productcontract.ErrNotFound
	}
	row, err := s.store.GetProductWithSettings(profile.ID, productID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, productcontract.ErrNotFound
	}
	rows, err := mergeSettings(profile, &row.Product, row.Settings, input, admin)
	if err != nil {
		return nil, err
	}
	return previewMergedSettings(profile, row.Product, rows), nil
}

func previewMergedSettings(profile *resellerdomain.Profile, product productdomain.Product, settings []resellerdomain.ProductSetting) []ProductSettingPreviewItem {
	products, skus := indexProductSettings(settings)
	productSetting := products[product.ID]
	listed := productSetting == nil || productSetting.IsListed
	productItem := ProductSettingPreviewItem{SKUID: 0, IsListed: listed, Valid: true, BasePrice: product.PriceAmount.Decimal.Round(2)}
	items := []ProductSettingPreviewItem{productItem}
	for i := range product.SKUs {
		sku := &product.SKUs[i]
		if !sku.IsActive {
			continue
		}
		setting := skus[productSettingKey{productID: product.ID, skuID: sku.ID}]
		price, _, base, floor, err := ResolveSKUPrice(profile, sku, productSetting, setting)
		skuListed := listed && (setting == nil || setting.IsListed)
		item := ProductSettingPreviewItem{SKUID: sku.ID, IsListed: skuListed, BasePrice: base, MinPrice: floor, EffectivePrice: price, Valid: err == nil || !skuListed, ErrorCode: previewErrorCode(err)}
		items = append(items, item)
		if skuListed && !item.Valid {
			items[0].Valid = false
			items[0].ErrorCode = item.ErrorCode
		}
		if len(items) == 2 || (skuListed && price.LessThan(items[0].EffectivePrice)) {
			items[0].EffectivePrice = price
			items[0].BasePrice = base
			items[0].MinPrice = floor
		}
	}
	if len(product.SKUs) == 0 {
		price, _, err := ResolveUnitAmount(profile, productSetting, nil, productItem.BasePrice)
		if err == nil {
			err = ValidateUnitAmount(profile, nil, productItem.BasePrice, price)
		}
		items[0].EffectivePrice = price
		items[0].Valid = err == nil || !listed
		items[0].ErrorCode = previewErrorCode(err)
	}
	return items
}

func buildPreviewSetting(input ProductSettingInput) (resellerdomain.ProductSetting, error) {
	mode := strings.TrimSpace(input.PricingMode)
	if mode == "" {
		mode = resellerdomain.PricingModeInherit
	}
	switch mode {
	case resellerdomain.PricingModeInherit, resellerdomain.PricingModeMarkupPercent, resellerdomain.PricingModeFixedMarkup, resellerdomain.PricingModeFixedPrice:
	default:
		return resellerdomain.ProductSetting{}, resellercontract.ErrPricingModeInvalid
	}
	return resellerdomain.ProductSetting{
		SKUID:             input.SKUID,
		IsListed:          input.IsListed,
		PricingMode:       mode,
		MarkupPercent:     money.FromDecimal(input.MarkupPercent.Round(2)),
		FixedMarkupAmount: money.FromDecimal(input.FixedMarkupAmount.Round(2)),
		FixedPriceAmount:  money.FromDecimal(input.FixedPriceAmount.Round(2)),
		SortOrder:         input.SortOrder,
	}, nil
}

func previewValidateProductRuleAcrossSKUs(profile *resellerdomain.Profile, product productdomain.Product, productSetting *resellerdomain.ProductSetting) (bool, string) {
	for i := range product.SKUs {
		sku := &product.SKUs[i]
		if !sku.IsActive {
			continue
		}
		base := sku.PriceAmount.Decimal.Round(2)
		price, _, err := ResolveUnitAmount(profile, productSetting, nil, base)
		if err == nil {
			err = ValidateUnitAmount(profile, sku, base, price)
		}
		if err != nil {
			return false, previewErrorCode(err)
		}
	}
	return true, ""
}

func previewErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, resellercontract.ErrMarkupExceeded):
		return "markup_exceeded"
	default:
		return "price_invalid"
	}
}

func computeProductEffectivePrices(profile *resellerdomain.Profile, product productdomain.Product, settings []resellerdomain.ProductSetting) (map[uint]decimal.Decimal, map[uint]string, error) {
	effective := map[uint]decimal.Decimal{}
	rules := map[uint]string{}
	byProduct, bySKU := indexProductSettings(settings)
	productSetting := byProduct[product.ID]
	if productSetting != nil && productSetting.IsListed {
		price, rule, err := ResolveUnitAmount(profile, productSetting, nil, product.PriceAmount.Decimal.Round(2))
		if err != nil {
			return effective, rules, err
		}
		effective[0] = price.Round(2)
		rules[0] = rule.Source
	}
	for i := range product.SKUs {
		sku := &product.SKUs[i]
		if !sku.IsActive {
			continue
		}
		skuSetting := bySKU[productSettingKey{productID: product.ID, skuID: sku.ID}]
		if productSetting != nil && !productSetting.IsListed {
			continue
		}
		if skuSetting != nil && !skuSetting.IsListed {
			continue
		}
		price, rule, _, _, _ := ResolveSKUPrice(profile, sku, productSetting, skuSetting)
		effective[sku.ID] = price.Round(2)
		rules[sku.ID] = rule.Source
	}
	return effective, rules, nil
}

type productSettingKey struct {
	productID uint
	skuID     uint
}

func indexProductSettings(settings []resellerdomain.ProductSetting) (map[uint]*resellerdomain.ProductSetting, map[productSettingKey]*resellerdomain.ProductSetting) {
	byProduct := make(map[uint]*resellerdomain.ProductSetting)
	bySKU := make(map[productSettingKey]*resellerdomain.ProductSetting)
	for i := range settings {
		setting := settings[i]
		row := setting
		if setting.SKUID == 0 {
			byProduct[setting.ProductID] = &row
			continue
		}
		bySKU[productSettingKey{productID: setting.ProductID, skuID: setting.SKUID}] = &row
	}
	return byProduct, bySKU
}

func findProductSKU(items []productdomain.ProductSKU, skuID uint) *productdomain.ProductSKU {
	for i := range items {
		if items[i].ID == skuID {
			return &items[i]
		}
	}
	return nil
}
