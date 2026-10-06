// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package presenter

import (
	"time"

	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/shopspring/decimal"
)

type ResellerProfileSummaryResp struct {
	ID               uint      `json:"id"`
	Status           string    `json:"status"`
	SettlementStatus string    `json:"settlement_status"`
	CreatedAt        time.Time `json:"created_at"`
}

type ResellerBalanceResp struct {
	ID              uint      `json:"id"`
	Currency        string    `json:"currency"`
	Status          string    `json:"status"`
	AvailableAmount string    `json:"available_amount"`
	LockedAmount    string    `json:"locked_amount"`
	NegativeAmount  string    `json:"negative_amount"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ResellerLedgerResp struct {
	ReviewState         string     `json:"review_state"`
	CanConfirm          bool       `json:"can_confirm"`
	ConfirmationMode    string     `json:"confirmation_mode"`
	DeliveryCompletedAt *time.Time `json:"delivery_completed_at,omitempty"`
	ConfirmedAt         *time.Time `json:"confirmed_at,omitempty"`
	ID                  uint       `json:"id"`
	OrderID             *uint      `json:"order_id,omitempty"`
	Type                string     `json:"type"`
	Amount              string     `json:"amount"`
	Currency            string     `json:"currency"`
	Status              string     `json:"status"`
	AvailableAt         *time.Time `json:"available_at,omitempty"`
	WithdrawRequestID   *uint      `json:"withdraw_request_id,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

type ResellerWithdrawResp struct {
	ID           uint       `json:"id"`
	Amount       string     `json:"amount"`
	Currency     string     `json:"currency"`
	Channel      string     `json:"channel"`
	Account      string     `json:"account"`
	Status       string     `json:"status"`
	RejectReason string     `json:"reject_reason,omitempty"`
	ProcessedAt  *time.Time `json:"processed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type ResellerDashboardResp struct {
	Opened                 bool                        `json:"opened"`
	Profile                *ResellerProfileSummaryResp `json:"profile,omitempty"`
	Balances               []ResellerBalanceResp       `json:"balances,omitempty"`
	WithdrawEnabled        bool                        `json:"withdraw_enabled"`
	WithdrawDisabledReason string                      `json:"withdraw_disabled_reason,omitempty"`
	WithdrawMinBalanceCNY  string                      `json:"withdraw_min_balance_cny"`
}

type ResellerManagementProfileResp struct {
	ID                   uint       `json:"id"`
	Status               string     `json:"status"`
	ApplyReason          string     `json:"apply_reason,omitempty"`
	RejectReason         string     `json:"reject_reason,omitempty"`
	DefaultMarkupPercent string     `json:"default_markup_percent"`
	MaxMarkupPercent     string     `json:"max_markup_percent"`
	SettlementStatus     string     `json:"settlement_status"`
	ReviewedAt           *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type ResellerDomainResp struct {
	ID                       uint       `json:"id"`
	Domain                   string     `json:"domain"`
	Type                     string     `json:"type"`
	VerificationToken        string     `json:"verification_token,omitempty"`
	VerificationStatus       string     `json:"verification_status"`
	LastDNSCheckAt           *time.Time `json:"last_dns_check_at,omitempty"`
	LastDNSError             string     `json:"last_dns_error,omitempty"`
	TLSStatus                string     `json:"tls_status"`
	TLSReadyAt               *time.Time `json:"tls_ready_at,omitempty"`
	DNSProvider              string     `json:"dns_provider,omitempty"`
	ConnectMode              string     `json:"connect_mode"`
	CloudflareHostnameID     string     `json:"cloudflare_hostname_id,omitempty"`
	CloudflareHostnameStatus string     `json:"cloudflare_hostname_status,omitempty"`
	CloudflareSSLStatus      string     `json:"cloudflare_ssl_status,omitempty"`
	CloudflareValidationJSON string     `json:"cloudflare_validation_json,omitempty"`
	LastTLSError             string     `json:"last_tls_error,omitempty"`
	AutoConnectRequestedAt   *time.Time `json:"auto_connect_requested_at,omitempty"`
	AutoConnectAttempts      int        `json:"auto_connect_attempts"`
	Status                   string     `json:"status"`
	IsPrimary                bool       `json:"is_primary"`
	VerifiedAt               *time.Time `json:"verified_at,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

type ResellerManagementSnapshotResp struct {
	Opened   bool                           `json:"opened"`
	CanApply bool                           `json:"can_apply"`
	Profile  *ResellerManagementProfileResp `json:"profile,omitempty"`
	Domains  []ResellerDomainResp           `json:"domains"`
}

type ResellerSiteConfigResp struct {
	Banner       jsonmap.JSON  `json:"banner"`
	ID           uint          `json:"id"`
	SiteName     string        `json:"site_name"`
	Logo         string        `json:"logo"`
	Favicon      string        `json:"favicon"`
	Announcement jsonmap.JSON  `json:"announcement"`
	Support      jsonmap.JSON  `json:"support"`
	SEO          jsonmap.JSON  `json:"seo"`
	FooterLinks  []interface{} `json:"footer_links"`
	NavConfig    jsonmap.JSON  `json:"nav_config"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type ResellerSiteConfigSnapshotResp struct {
	Opened  bool                    `json:"opened"`
	CanEdit bool                    `json:"can_edit"`
	Config  *ResellerSiteConfigResp `json:"config,omitempty"`
}

type ResellerSiteConfigOwnerUserResp struct {
	ID          uint   `json:"id"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

type ResellerSiteConfigProfileRefResp struct {
	ID               uint                             `json:"id"`
	UserID           uint                             `json:"user_id"`
	Status           string                           `json:"status,omitempty"`
	SettlementStatus string                           `json:"settlement_status,omitempty"`
	User             *ResellerSiteConfigOwnerUserResp `json:"user,omitempty"`
}

type AdminResellerSiteConfigResp struct {
	Banner       jsonmap.JSON                      `json:"banner"`
	ID           uint                              `json:"id"`
	ResellerID   uint                              `json:"reseller_id"`
	SiteName     string                            `json:"site_name"`
	Logo         string                            `json:"logo"`
	Favicon      string                            `json:"favicon"`
	Announcement jsonmap.JSON                      `json:"announcement"`
	Support      jsonmap.JSON                      `json:"support"`
	SEO          jsonmap.JSON                      `json:"seo"`
	FooterLinks  []interface{}                     `json:"footer_links"`
	NavConfig    jsonmap.JSON                      `json:"nav_config"`
	Profile      *ResellerSiteConfigProfileRefResp `json:"profile,omitempty"`
	CreatedAt    time.Time                         `json:"created_at"`
	UpdatedAt    time.Time                         `json:"updated_at"`
}

type ResellerProductSettingResp struct {
	SupplyPriceOverride  string     `json:"supply_price_override"`
	MinPriceOverride     string     `json:"min_price_override"`
	ID                   uint       `json:"id"`
	ProductID            uint       `json:"product_id"`
	SKUID                uint       `json:"sku_id"`
	IsListed             bool       `json:"is_listed"`
	PricingMode          string     `json:"pricing_mode"`
	MarkupPercent        string     `json:"markup_percent"`
	FixedMarkupAmount    string     `json:"fixed_markup_amount"`
	FixedPriceAmount     string     `json:"fixed_price_amount"`
	EffectivePriceAmount string     `json:"effective_price_amount,omitempty"`
	RuleSource           string     `json:"rule_source,omitempty"`
	SortOrder            int        `json:"sort_order"`
	UpdatedAt            *time.Time `json:"updated_at,omitempty"`
}

type ResellerProductSettingProductResp struct {
	Images           []string     `json:"images"`
	PlatformImages   []string     `json:"platform_images"`
	ImagesCustomized bool         `json:"images_customized"`
	ID               uint         `json:"id"`
	Slug             string       `json:"slug"`
	Title            jsonmap.JSON `json:"title"`
	PriceAmount      string       `json:"price_amount"`
	IsActive         bool         `json:"is_active"`
}

type ResellerProductSettingSKUResp struct {
	SupplyPriceAmount    string                      `json:"supply_price_amount"`
	MinPriceAmount       string                      `json:"min_price_amount"`
	ExpectedProfitAmount string                      `json:"expected_profit_amount"`
	ID                   uint                        `json:"id"`
	SKUCode              string                      `json:"sku_code"`
	SpecValues           jsonmap.JSON                `json:"spec_values"`
	BasePriceAmount      string                      `json:"base_price_amount"`
	IsActive             bool                        `json:"is_active"`
	Setting              *ResellerProductSettingResp `json:"setting,omitempty"`
	EffectivePrice       string                      `json:"effective_price_amount,omitempty"`
}

type ResellerProductSettingDetailResp struct {
	Product        ResellerProductSettingProductResp `json:"product"`
	ProductSetting *ResellerProductSettingResp       `json:"product_setting,omitempty"`
	SKUs           []ResellerProductSettingSKUResp   `json:"skus"`
}

type ResellerProductSettingDTOInput struct {
	Product          productdomain.Product
	Settings         []resellerdomain.ProductSetting
	EffectiveBySKUID map[uint]string
	RuleBySKUID      map[uint]string
}

type AdminResellerProductSettingUserResp struct {
	ID          uint   `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

type AdminResellerProductSettingProfileResp struct {
	ID               uint                                 `json:"id"`
	UserID           uint                                 `json:"user_id"`
	Status           string                               `json:"status"`
	SettlementStatus string                               `json:"settlement_status"`
	User             *AdminResellerProductSettingUserResp `json:"user,omitempty"`
}

type AdminResellerProductSettingProductResp struct {
	ID          uint         `json:"id"`
	Slug        string       `json:"slug"`
	Title       jsonmap.JSON `json:"title"`
	PriceAmount string       `json:"price_amount"`
	IsActive    bool         `json:"is_active"`
}

type AdminResellerProductSettingResp struct {
	SupplyPriceAmount string                                  `json:"supply_price_amount"`
	MinPriceAmount    string                                  `json:"min_price_amount"`
	ID                uint                                    `json:"id"`
	ResellerID        uint                                    `json:"reseller_id"`
	ProductID         uint                                    `json:"product_id"`
	SKUID             uint                                    `json:"sku_id"`
	IsListed          bool                                    `json:"is_listed"`
	PricingMode       string                                  `json:"pricing_mode"`
	MarkupPercent     string                                  `json:"markup_percent"`
	FixedMarkupAmount string                                  `json:"fixed_markup_amount"`
	FixedPriceAmount  string                                  `json:"fixed_price_amount"`
	SortOrder         int                                     `json:"sort_order"`
	CreatedAt         time.Time                               `json:"created_at"`
	UpdatedAt         time.Time                               `json:"updated_at"`
	Profile           *AdminResellerProductSettingProfileResp `json:"profile,omitempty"`
	Product           *AdminResellerProductSettingProductResp `json:"product,omitempty"`
}

func NewResellerProfileSummaryResp(profile *resellerdomain.Profile) *ResellerProfileSummaryResp {
	if profile == nil {
		return nil
	}
	return &ResellerProfileSummaryResp{
		ID:               profile.ID,
		Status:           profile.Status,
		SettlementStatus: profile.SettlementStatus,
		CreatedAt:        profile.CreatedAt,
	}
}

func NewResellerManagementProfileResp(profile *resellerdomain.Profile) *ResellerManagementProfileResp {
	if profile == nil {
		return nil
	}
	return &ResellerManagementProfileResp{
		ID:                   profile.ID,
		Status:               profile.Status,
		ApplyReason:          profile.ApplyReason,
		RejectReason:         profile.RejectReason,
		DefaultMarkupPercent: profile.DefaultMarkupPercent.String(),
		MaxMarkupPercent:     profile.MaxMarkupPercent.String(),
		SettlementStatus:     profile.SettlementStatus,
		ReviewedAt:           profile.ReviewedAt,
		CreatedAt:            profile.CreatedAt,
		UpdatedAt:            profile.UpdatedAt,
	}
}

func NewResellerDomainResp(row *resellerdomain.Domain) ResellerDomainResp {
	if row == nil {
		return ResellerDomainResp{}
	}
	return ResellerDomainResp{
		ID:                 row.ID,
		Domain:             row.Domain,
		Type:               row.Type,
		VerificationToken:  row.VerificationToken,
		VerificationStatus: row.VerificationStatus,
		LastDNSCheckAt:     row.LastDNSCheckAt,
		LastDNSError:       row.LastDNSError,
		TLSStatus:          row.TLSStatus,
		TLSReadyAt:         row.TLSReadyAt,
		DNSProvider:        row.DNSProvider,
		ConnectMode:        row.ConnectMode, CloudflareHostnameID: row.CloudflareHostnameID, CloudflareHostnameStatus: row.CloudflareHostnameStatus, CloudflareSSLStatus: row.CloudflareSSLStatus, CloudflareValidationJSON: row.CloudflareValidationJSON,
		LastTLSError:           row.LastTLSError,
		AutoConnectRequestedAt: row.AutoConnectRequestedAt,
		AutoConnectAttempts:    row.AutoConnectAttempts,
		Status:                 row.Status,
		IsPrimary:              row.IsPrimary,
		VerifiedAt:             row.VerifiedAt,
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
	}
}

func NewResellerDomainRespList(rows []resellerdomain.Domain) []ResellerDomainResp {
	result := make([]ResellerDomainResp, 0, len(rows))
	for i := range rows {
		result = append(result, NewResellerDomainResp(&rows[i]))
	}
	return result
}

func NewResellerManagementSnapshotResp(profile *resellerdomain.Profile, domains []resellerdomain.Domain, canApply bool) ResellerManagementSnapshotResp {
	if profile == nil {
		return ResellerManagementSnapshotResp{Opened: false, CanApply: canApply, Domains: []ResellerDomainResp{}}
	}
	return ResellerManagementSnapshotResp{
		Opened:   true,
		CanApply: canApply,
		Profile:  NewResellerManagementProfileResp(profile),
		Domains:  NewResellerDomainRespList(domains),
	}
}

func NewResellerSiteConfigResp(row *resellerdomain.SiteConfig) *ResellerSiteConfigResp {
	if row == nil {
		return nil
	}
	return &ResellerSiteConfigResp{
		ID:           row.ID,
		SiteName:     row.SiteName,
		Logo:         row.Logo,
		Favicon:      row.Favicon,
		Announcement: row.AnnouncementJSON,
		Support:      row.SupportJSON,
		SEO:          row.SEOJSON,
		FooterLinks:  resellerFooterLinksFromEnvelope(row.FooterLinksJSON),
		NavConfig:    row.NavConfigJSON,
		Banner:       resellerBannerFromTheme(row.ThemeJSON),
		UpdatedAt:    row.UpdatedAt,
	}
}

func resellerFooterLinksFromEnvelope(raw jsonmap.JSON) []interface{} {
	if raw == nil {
		return make([]interface{}, 0)
	}
	if items, ok := raw["items"].([]interface{}); ok {
		return items
	}
	if typed, ok := raw["items"].([]jsonmap.JSON); ok {
		out := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	}
	return make([]interface{}, 0)
}

func NewResellerSiteConfigSnapshotResp(profile *resellerdomain.Profile, row *resellerdomain.SiteConfig, canEdit bool) ResellerSiteConfigSnapshotResp {
	return ResellerSiteConfigSnapshotResp{
		Opened:  profile != nil,
		CanEdit: canEdit,
		Config:  NewResellerSiteConfigResp(row),
	}
}

func NewAdminResellerSiteConfigResp(row *resellerdomain.SiteConfig) AdminResellerSiteConfigResp {
	if row == nil {
		return AdminResellerSiteConfigResp{FooterLinks: make([]interface{}, 0)}
	}
	var profile *ResellerSiteConfigProfileRefResp
	if row.Profile != nil {
		profile = &ResellerSiteConfigProfileRefResp{
			ID:               row.Profile.ID,
			UserID:           row.Profile.UserID,
			Status:           row.Profile.Status,
			SettlementStatus: row.Profile.SettlementStatus,
		}
		if row.Profile.User != nil {
			profile.User = &ResellerSiteConfigOwnerUserResp{
				ID:          row.Profile.User.ID,
				Email:       row.Profile.User.Email,
				DisplayName: row.Profile.User.DisplayName,
			}
		}
	}
	return AdminResellerSiteConfigResp{
		ID:           row.ID,
		ResellerID:   row.ResellerID,
		SiteName:     row.SiteName,
		Logo:         row.Logo,
		Favicon:      row.Favicon,
		Announcement: row.AnnouncementJSON,
		Support:      row.SupportJSON,
		SEO:          row.SEOJSON,
		FooterLinks:  resellerFooterLinksFromEnvelope(row.FooterLinksJSON),
		NavConfig:    row.NavConfigJSON,
		Banner:       resellerBannerFromTheme(row.ThemeJSON),
		Profile:      profile,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func NewAdminResellerSiteConfigRespList(rows []resellerdomain.SiteConfig) []AdminResellerSiteConfigResp {
	result := make([]AdminResellerSiteConfigResp, 0, len(rows))
	for i := range rows {
		result = append(result, NewAdminResellerSiteConfigResp(&rows[i]))
	}
	return result
}

func NewResellerProductSettingDetailResp(input ResellerProductSettingDTOInput) ResellerProductSettingDetailResp {
	productSetting := findResellerProductSetting(input.Settings, 0)
	resp := ResellerProductSettingDetailResp{
		Product: ResellerProductSettingProductResp{
			Images:         append([]string{}, input.Product.Images...),
			PlatformImages: append([]string{}, input.Product.Images...),
			ID:             input.Product.ID,
			Slug:           input.Product.Slug,
			Title:          input.Product.TitleJSON,
			PriceAmount:    input.Product.PriceAmount.String(),
			IsActive:       input.Product.IsActive,
		},
		SKUs: make([]ResellerProductSettingSKUResp, 0, len(input.Product.SKUs)),
	}
	if productSetting != nil {
		resp.ProductSetting = newResellerProductSettingResp(*productSetting, input.EffectiveBySKUID[0], input.RuleBySKUID[0])
		if len(productSetting.ImagesJSON) > 0 {
			resp.Product.Images = append([]string{}, productSetting.ImagesJSON...)
			resp.Product.ImagesCustomized = true
		}
	}
	for i := range input.Product.SKUs {
		sku := input.Product.SKUs[i]
		setting := findResellerProductSetting(input.Settings, sku.ID)
		var settingResp *ResellerProductSettingResp
		if setting != nil {
			settingResp = newResellerProductSettingResp(*setting, input.EffectiveBySKUID[sku.ID], input.RuleBySKUID[sku.ID])
		}
		supply, floor := resellerdomain.ResolveSupplyTerms(&sku, productSetting, setting)
		profit := decimal.Zero
		if effective, err := decimal.NewFromString(input.EffectiveBySKUID[sku.ID]); err == nil {
			profit = effective.Sub(supply).Round(2)
		}
		resp.SKUs = append(resp.SKUs, ResellerProductSettingSKUResp{
			SupplyPriceAmount:    supply.StringFixed(2),
			MinPriceAmount:       floor.StringFixed(2),
			ExpectedProfitAmount: profit.StringFixed(2),
			ID:                   sku.ID,
			SKUCode:              sku.SKUCode,
			SpecValues:           sku.SpecValuesJSON,
			BasePriceAmount:      supply.StringFixed(2),
			IsActive:             sku.IsActive,
			Setting:              settingResp,
			EffectivePrice:       input.EffectiveBySKUID[sku.ID],
		})
	}
	return resp
}

func NewResellerProductSettingListResp(rows []ResellerProductSettingDTOInput) []ResellerProductSettingDetailResp {
	out := make([]ResellerProductSettingDetailResp, 0, len(rows))
	for i := range rows {
		out = append(out, NewResellerProductSettingDetailResp(rows[i]))
	}
	return out
}

// ResellerProductSettingPreviewItemResp 单个商品级（sku_id=0）或 SKU 级规则的预览结果。
type ResellerProductSettingPreviewItemResp struct {
	SupplyPriceAmount    string `json:"supply_price_amount"`
	MinPriceAmount       string `json:"min_price_amount"`
	ExpectedProfitAmount string `json:"expected_profit_amount"`
	SKUID                uint   `json:"sku_id"`
	IsListed             bool   `json:"is_listed"`
	BasePriceAmount      string `json:"base_price_amount"`
	EffectivePriceAmount string `json:"effective_price_amount"`
	Valid                bool   `json:"valid"`
	ErrorCode            string `json:"error_code,omitempty"`
}

type ResellerProductSettingPreviewResp struct {
	Items []ResellerProductSettingPreviewItemResp `json:"items"`
}

// ResellerProductSettingPreviewInput 由 handler 从 service 结果映射而来（金额已格式化为两位小数字符串）。
type ResellerProductSettingPreviewInput struct {
	MinPrice       string
	SKUID          uint
	IsListed       bool
	BasePrice      string
	EffectivePrice string
	Valid          bool
	ErrorCode      string
}

func NewResellerProductSettingPreviewResp(items []ResellerProductSettingPreviewInput) ResellerProductSettingPreviewResp {
	out := ResellerProductSettingPreviewResp{Items: make([]ResellerProductSettingPreviewItemResp, 0, len(items))}
	for _, item := range items {
		profit := decimal.Zero
		if price, err := decimal.NewFromString(item.EffectivePrice); err == nil {
			if supply, err := decimal.NewFromString(item.BasePrice); err == nil {
				profit = price.Sub(supply).Round(2)
			}
		}
		out.Items = append(out.Items, ResellerProductSettingPreviewItemResp{
			SupplyPriceAmount:    item.BasePrice,
			MinPriceAmount:       item.MinPrice,
			ExpectedProfitAmount: profit.StringFixed(2),
			SKUID:                item.SKUID,
			IsListed:             item.IsListed,
			BasePriceAmount:      item.BasePrice,
			EffectivePriceAmount: item.EffectivePrice,
			Valid:                item.Valid,
			ErrorCode:            item.ErrorCode,
		})
	}
	return out
}

func newResellerProductSettingResp(setting resellerdomain.ProductSetting, effectivePrice string, ruleSource string) *ResellerProductSettingResp {
	updatedAt := setting.UpdatedAt
	return &ResellerProductSettingResp{
		SupplyPriceOverride:  setting.SupplyPriceAmount.String(),
		MinPriceOverride:     setting.MinPriceAmount.String(),
		ID:                   setting.ID,
		ProductID:            setting.ProductID,
		SKUID:                setting.SKUID,
		IsListed:             setting.IsListed,
		PricingMode:          setting.PricingMode,
		MarkupPercent:        setting.MarkupPercent.String(),
		FixedMarkupAmount:    setting.FixedMarkupAmount.String(),
		FixedPriceAmount:     setting.FixedPriceAmount.String(),
		EffectivePriceAmount: effectivePrice,
		RuleSource:           ruleSource,
		SortOrder:            setting.SortOrder,
		UpdatedAt:            &updatedAt,
	}
}

func findResellerProductSetting(settings []resellerdomain.ProductSetting, skuID uint) *resellerdomain.ProductSetting {
	for i := range settings {
		if settings[i].SKUID == skuID {
			return &settings[i]
		}
	}
	return nil
}

func NewAdminResellerProductSettingResp(row resellerdomain.ProductSetting) AdminResellerProductSettingResp {
	resp := AdminResellerProductSettingResp{
		SupplyPriceAmount: row.SupplyPriceAmount.String(),
		MinPriceAmount:    row.MinPriceAmount.String(),
		ID:                row.ID,
		ResellerID:        row.ResellerID,
		ProductID:         row.ProductID,
		SKUID:             row.SKUID,
		IsListed:          row.IsListed,
		PricingMode:       row.PricingMode,
		MarkupPercent:     row.MarkupPercent.String(),
		FixedMarkupAmount: row.FixedMarkupAmount.String(),
		FixedPriceAmount:  row.FixedPriceAmount.String(),
		SortOrder:         row.SortOrder,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
	if row.Profile != nil {
		profile := &AdminResellerProductSettingProfileResp{
			ID:               row.Profile.ID,
			UserID:           row.Profile.UserID,
			Status:           row.Profile.Status,
			SettlementStatus: row.Profile.SettlementStatus,
		}
		if row.Profile.User != nil {
			profile.User = &AdminResellerProductSettingUserResp{
				ID:          row.Profile.User.ID,
				Email:       row.Profile.User.Email,
				DisplayName: row.Profile.User.DisplayName,
			}
		}
		resp.Profile = profile
	}
	if row.Product != nil {
		resp.Product = &AdminResellerProductSettingProductResp{
			ID:          row.Product.ID,
			Slug:        row.Product.Slug,
			Title:       row.Product.TitleJSON,
			PriceAmount: row.Product.PriceAmount.String(),
			IsActive:    row.Product.IsActive,
		}
	}
	return resp
}

func NewAdminResellerProductSettingRespList(rows []resellerdomain.ProductSetting) []AdminResellerProductSettingResp {
	out := make([]AdminResellerProductSettingResp, 0, len(rows))
	for i := range rows {
		out = append(out, NewAdminResellerProductSettingResp(rows[i]))
	}
	return out
}

func NewResellerBalanceResp(row *resellerdomain.BalanceAccount) ResellerBalanceResp {
	if row == nil {
		return ResellerBalanceResp{}
	}
	return ResellerBalanceResp{
		ID:              row.ID,
		Currency:        row.Currency,
		Status:          row.Status,
		AvailableAmount: row.AvailableAmountCache.String(),
		LockedAmount:    row.LockedAmountCache.String(),
		NegativeAmount:  row.NegativeAmountCache.String(),
		UpdatedAt:       row.UpdatedAt,
	}
}

func NewResellerBalanceRespList(rows []resellerdomain.BalanceAccount) []ResellerBalanceResp {
	result := make([]ResellerBalanceResp, 0, len(rows))
	for i := range rows {
		result = append(result, NewResellerBalanceResp(&rows[i]))
	}
	return result
}

func NewResellerLedgerResp(row *resellerdomain.LedgerEntry) ResellerLedgerResp {
	if row == nil {
		return ResellerLedgerResp{}
	}
	return ResellerLedgerResp{
		ReviewState:         row.ReviewState,
		CanConfirm:          row.CanConfirm,
		ID:                  row.ID,
		OrderID:             row.OrderID,
		Type:                row.Type,
		Amount:              row.Amount.String(),
		Currency:            row.Currency,
		Status:              row.Status,
		AvailableAt:         row.AvailableAt,
		ConfirmationMode:    row.ConfirmationMode,
		DeliveryCompletedAt: row.DeliveryCompletedAt,
		ConfirmedAt:         row.ConfirmedAt,
		WithdrawRequestID:   row.WithdrawRequestID,
		CreatedAt:           row.CreatedAt,
	}
}

func NewResellerLedgerRespList(rows []resellerdomain.LedgerEntry) []ResellerLedgerResp {
	result := make([]ResellerLedgerResp, 0, len(rows))
	for i := range rows {
		result = append(result, NewResellerLedgerResp(&rows[i]))
	}
	return result
}

func NewResellerWithdrawResp(row *resellerdomain.WithdrawRequest) ResellerWithdrawResp {
	if row == nil {
		return ResellerWithdrawResp{}
	}
	return ResellerWithdrawResp{
		ID:           row.ID,
		Amount:       row.Amount.String(),
		Currency:     row.Currency,
		Channel:      row.Channel,
		Account:      row.Account,
		Status:       row.Status,
		RejectReason: row.RejectReason,
		ProcessedAt:  row.ProcessedAt,
		CreatedAt:    row.CreatedAt,
	}
}

func NewResellerWithdrawRespList(rows []resellerdomain.WithdrawRequest) []ResellerWithdrawResp {
	result := make([]ResellerWithdrawResp, 0, len(rows))
	for i := range rows {
		result = append(result, NewResellerWithdrawResp(&rows[i]))
	}
	return result
}

func NewResellerDashboardResp(opened bool, profile *resellerdomain.Profile, balances []resellerdomain.BalanceAccount, withdrawEnabled bool, withdrawDisabledReason string) ResellerDashboardResp {
	if !opened {
		return ResellerDashboardResp{Opened: false}
	}
	return ResellerDashboardResp{
		Opened:                 true,
		Profile:                NewResellerProfileSummaryResp(profile),
		Balances:               NewResellerBalanceRespList(balances),
		WithdrawEnabled:        withdrawEnabled,
		WithdrawDisabledReason: withdrawDisabledReason,
		WithdrawMinBalanceCNY:  decimal.NewFromInt(resellercontract.MinimumWithdrawBalanceCNY).StringFixed(2),
	}
}

func resellerBannerFromTheme(theme jsonmap.JSON) jsonmap.JSON {
	switch value := theme["banner"].(type) {
	case jsonmap.JSON:
		return value
	case map[string]interface{}:
		return jsonmap.JSON(value)
	default:
		return jsonmap.JSON{"mode": "inherit"}
	}
}
