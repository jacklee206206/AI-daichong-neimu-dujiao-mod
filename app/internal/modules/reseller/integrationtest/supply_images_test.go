// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package integrationtest

import (
	"errors"
	"github.com/dujiao-next/internal/constants"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
	"testing"
)

func TestSupplyAgreementCannotBeOverriddenByResellerAndSurvivesReset(t *testing.T) {
	db := openResellerProductSettingServiceTestDB(t)
	user, profile, product, skus := seedResellerProductSettingServiceData(t, db)
	svc := newResellerProductSettingServiceForTest(db)
	supply, floor := decimal.NewFromInt(120), decimal.NewFromInt(127)
	// Public main-store price and operator's procurement cost are independent of this agreement.
	if err := db.Model(&productdomain.ProductSKU{}).Where("id = ?", skus[0].ID).Updates(map[string]interface{}{"price_amount": "150", "cost_price_amount": "80"}).Error; err != nil {
		t.Fatal(err)
	}
	_, err := svc.SaveAdminProductSettings(profile.ID, product.ID, ResellerProductSettingSaveInput{Settings: []ResellerProductSettingInput{{SKUID: skus[0].ID, IsListed: true, PricingMode: resellerdomain.PricingModeInherit, SupplyPriceAmount: &supply, MinPriceAmount: &floor}}})
	if err != nil {
		t.Fatal(err)
	}
	low := decimal.NewFromInt(1)
	_, err = svc.SaveUserProductSettings(user.ID, product.ID, ResellerProductSettingSaveInput{Settings: []ResellerProductSettingInput{{SKUID: skus[0].ID, IsListed: true, PricingMode: resellerdomain.PricingModeInherit, SupplyPriceAmount: &low}}})
	if !errors.Is(err, resellercontract.ErrSupplyPriceForbidden) {
		t.Fatalf("supply tampering accepted: %v", err)
	}
	_, err = svc.SaveUserProductSettings(user.ID, product.ID, ResellerProductSettingSaveInput{Settings: []ResellerProductSettingInput{{SKUID: skus[0].ID, IsListed: true, PricingMode: resellerdomain.PricingModeInherit, MinPriceAmount: &low}}})
	if !errors.Is(err, resellercontract.ErrSupplyPriceForbidden) {
		t.Fatalf("minimum-price tampering accepted: %v", err)
	}
	_, err = svc.SaveUserProductSettings(user.ID, product.ID, ResellerProductSettingSaveInput{Settings: []ResellerProductSettingInput{{SKUID: skus[0].ID, IsListed: true, PricingMode: resellerdomain.PricingModeFixedPrice, FixedPriceAmount: decimal.NewFromInt(126)}}})
	if !errors.Is(err, resellercontract.ErrPriceBelowBase) {
		t.Fatalf("floor bypass accepted: %v", err)
	}
	detail, err := svc.SaveUserProductSettings(user.ID, product.ID, ResellerProductSettingSaveInput{Settings: []ResellerProductSettingInput{{SKUID: skus[0].ID, IsListed: true, PricingMode: resellerdomain.PricingModeFixedPrice, FixedPriceAmount: decimal.NewFromInt(128)}}})
	if err != nil {
		t.Fatal(err)
	}
	if !detail.EffectiveBySKUID[skus[0].ID].Equal(decimal.NewFromInt(128)) {
		t.Fatal("wrong retail")
	}
	// Updating one SKU preserves the other SKU and immutable supply agreement.
	_, err = svc.SaveUserProductSettings(user.ID, product.ID, ResellerProductSettingSaveInput{Settings: []ResellerProductSettingInput{{SKUID: skus[1].ID, IsListed: true, PricingMode: resellerdomain.PricingModeFixedPrice, FixedPriceAmount: decimal.NewFromInt(230)}}})
	if err != nil {
		t.Fatal(err)
	}
	var first resellerdomain.ProductSetting
	db.Where("reseller_id = ? AND product_id = ? AND sku_id = ?", profile.ID, product.ID, skus[0].ID).First(&first)
	if !first.FixedPriceAmount.Decimal.Equal(decimal.NewFromInt(128)) || !first.SupplyPriceAmount.Decimal.Equal(supply) {
		t.Fatalf("partial save clobbered agreement: %+v", first)
	}
	if err = svc.ResetUserProductSetting(user.ID, product.ID, skus[0].ID); err != nil {
		t.Fatal(err)
	}
	db.Where("id = ?", first.ID).First(&first)
	if !first.SupplyPriceAmount.Decimal.Equal(supply) || !first.MinPriceAmount.Decimal.Equal(floor) {
		t.Fatal("user reset erased supply agreement")
	}
}

func TestResellerProductImagesAreScopedAndDoNotOverwritePrices(t *testing.T) {
	db := openResellerProductSettingServiceTestDB(t)
	user, profile, product, _ := seedResellerProductSettingServiceData(t, db)
	svc := newResellerProductSettingServiceForTest(db)
	secondUser := userdomain.User{Email: "images-second@example.test", PasswordHash: "test", Status: constants.UserStatusActive}
	if err := db.Create(&secondUser).Error; err != nil {
		t.Fatal(err)
	}
	second := resellerdomain.Profile{UserID: secondUser.ID, Status: resellerdomain.ProfileStatusActive, SettlementStatus: resellerdomain.SettlementStatusNormal}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	setting := resellerdomain.ProductSetting{ResellerID: profile.ID, ProductID: product.ID, SKUID: 0, IsListed: false, PricingMode: resellerdomain.PricingModeMarkupPercent, MarkupPercent: money.FromDecimal(decimal.NewFromInt(15)), SupplyPriceAmount: money.FromDecimal(decimal.NewFromInt(95))}
	if err := db.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}
	for _, images := range [][]string{{"javascript:alert(1)"}, make([]string, 21)} {
		if _, err := svc.SaveUserProductImages(user.ID, product.ID, images); !errors.Is(err, resellercontract.ErrProductImagesInvalid) {
			t.Fatalf("unsafe image accepted: %v", err)
		}
	}
	if err := db.Model(&setting).UpdateColumn("is_listed", false).Error; err != nil {
		t.Fatal(err)
	}
	detail, err := svc.SaveUserProductImages(user.ID, product.ID, []string{"/uploads/reseller/custom.png", "/uploads/reseller/second.png"})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Settings) != 1 || len(detail.Settings[0].ImagesJSON) != 2 || detail.Settings[0].IsListed || !detail.Settings[0].SupplyPriceAmount.Decimal.Equal(decimal.NewFromInt(95)) {
		t.Fatal("image save clobbered settings")
	}
	other, err := svc.GetUserProductSetting(secondUser.ID, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Settings) != 0 {
		t.Fatal("image leaked to another reseller")
	}
	detail, err = svc.SaveUserProductImages(user.ID, product.ID, []string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Settings[0].ImagesJSON) != 0 || !detail.Settings[0].SupplyPriceAmount.Decimal.Equal(decimal.NewFromInt(95)) {
		t.Fatal("image reset clobbered agreement")
	}
	var main productdomain.Product
	db.First(&main, product.ID)
	if len(main.Images) != 0 {
		t.Fatal("main-store images changed")
	}
}
