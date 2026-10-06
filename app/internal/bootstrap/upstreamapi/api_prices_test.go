// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package upstreamwiring

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dujiao-next/internal/app/container"
	"github.com/dujiao-next/internal/app/httpserver/middleware"
	orderwiring "github.com/dujiao-next/internal/bootstrap/order"
	"github.com/dujiao-next/internal/constants"
	apidomain "github.com/dujiao-next/internal/modules/apicredential/domain"
	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"
	productapp "github.com/dujiao-next/internal/modules/catalog/product/application"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	productstore "github.com/dujiao-next/internal/modules/catalog/product/store/gormstore"
	downstreamdomain "github.com/dujiao-next/internal/modules/downstreamcallback/domain"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	userstore "github.com/dujiao-next/internal/modules/identity/user/infrastructure/gormstore"
	memberdomain "github.com/dujiao-next/internal/modules/memberlevel/domain"
	orderapp "github.com/dujiao-next/internal/modules/order/application"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	orderstore "github.com/dujiao-next/internal/modules/order/infrastructure/gormstore"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	promotiondomain "github.com/dujiao-next/internal/modules/promotion/domain"
	promotionstore "github.com/dujiao-next/internal/modules/promotion/infrastructure/gormstore"
	upstreamhttp "github.com/dujiao-next/internal/modules/upstreamapi/transport/http"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/dujiao-next/internal/upstream"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type apiPriceQueue struct{}

func (apiPriceQueue) Enabled() bool                                  { return true }
func (apiPriceQueue) EnqueueTimeoutCancel(uint, time.Duration) error { return nil }
func (apiPriceQueue) EnqueueStatusEmail(uint, string) error          { return nil }

type apiPriceMember struct{}

func (apiPriceMember) ResolveMemberPrice(_, _, _ uint, base decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	price := base.Mul(decimal.RequireFromString("0.8")).Round(2)
	return price, base.Sub(price)
}
func (apiPriceMember) OnOrderPaid(uint, decimal.Decimal) error         { return nil }
func (apiPriceMember) GetByID(uint) (*memberdomain.MemberLevel, error) { return nil, nil }

type apiPriceSettings struct{}

func (apiPriceSettings) GetByKey(string) (jsonmap.JSON, error)  { return nil, nil }
func (apiPriceSettings) GetSiteCurrency(string) (string, error) { return "CNY", nil }

type apiPriceCredentials struct{ credential *apidomain.ApiCredential }

func (s apiPriceCredentials) GetByApiKey(key string) (*apidomain.ApiCredential, error) {
	if key == s.credential.ApiKey {
		return s.credential, nil
	}
	return nil, nil
}
func (apiPriceCredentials) TouchLastUsedAt(uint, time.Time) error { return nil }

type apiPriceReferences struct{}

func (apiPriceReferences) Create(*downstreamdomain.OrderRef) error { return nil }
func (apiPriceReferences) GetByCredentialAndDownstreamNo(uint, string) (*downstreamdomain.OrderRef, error) {
	return nil, nil
}

type apiPricePayments struct {
	db     *gorm.DB
	amount string
}

func (s *apiPricePayments) CreatePayment(input upstreamhttp.CreatePaymentInput) (*upstreamhttp.CreatePaymentResult, error) {
	var order orderdomain.Order
	if err := s.db.First(&order, input.OrderID).Error; err != nil {
		return nil, err
	}
	s.amount = order.TotalAmount.String()
	return &upstreamhttp.CreatePaymentResult{}, nil
}

// Exercises the signed upstream route, real composition adapter, order pricing,
// persisted order and payment handoff against the same prices returned by catalog.
func TestAPISupplyQuotesMatchSignedOrdersAndPublicRequestsCannotOptIn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:api_prices_%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&categorydomain.Category{}, &productdomain.Product{}, &productdomain.ProductSKU{}, &userdomain.User{}, &orderdomain.Order{}, &orderdomain.OrderItem{}, &fulfillmentdomain.Fulfillment{}, &promotiondomain.Promotion{}, &paymentdomain.Payment{}); err != nil {
		t.Fatal(err)
	}
	amount := func(value int64) money.Amount { return money.FromDecimal(decimal.NewFromInt(value)) }
	category := categorydomain.Category{Slug: "api-category", NameJSON: jsonmap.JSON{"en-US": "API"}, IsActive: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	product := productdomain.Product{CategoryID: category.ID, Slug: "api-prices", TitleJSON: jsonmap.JSON{"en-US": "API prices"}, PriceAmount: amount(124), IsActive: true, PurchaseType: constants.ProductPurchaseMember, FulfillmentType: constants.FulfillmentTypeManual, WholesalePrices: productdomain.WholesalePriceTiers{{MinQuantity: 2, UnitPrice: amount(70)}}}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	skus := []productdomain.ProductSKU{
		{ProductID: product.ID, SKUCode: "Plus", PriceAmount: amount(124), APISupplyPriceAmount: amount(115), ResellerSupplyPriceAmount: amount(118), IsActive: true, ManualStockTotal: -1},
		{ProductID: product.ID, SKUCode: "Pro5x", PriceAmount: amount(730), APISupplyPriceAmount: amount(650), ResellerSupplyPriceAmount: amount(660), IsActive: true, ManualStockTotal: -1},
		{ProductID: product.ID, SKUCode: "Pro10x", PriceAmount: amount(1160), APISupplyPriceAmount: amount(1070), ResellerSupplyPriceAmount: amount(1080), IsActive: true, ManualStockTotal: -1},
	}
	if err := db.Create(&skus).Error; err != nil {
		t.Fatal(err)
	}
	promotion := promotiondomain.Promotion{Name: "retail-discount", ScopeType: constants.ScopeTypeProduct, ScopeRefID: product.ID, Type: constants.PromotionTypeFixed, Value: amount(40), IsActive: true}
	if err := db.Create(&promotion).Error; err != nil {
		t.Fatal(err)
	}
	user := userdomain.User{Email: "api-prices@example.test", PasswordHash: "hash", Status: constants.UserStatusActive, MemberLevelID: 1}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	products, skuStore, users := productstore.NewProductStore(db), productstore.NewSKUStore(db), userstore.New(db)
	svc := orderapp.NewOrderService(orderapp.OrderServiceOptions{OrderStore: orderstore.New(db, "test-credential-secret-32-bytes-long"), UserStore: users, ProductStore: products, ProductSKUStore: skuStore, PromotionRepo: promotionstore.New(db), MemberLevelService: apiPriceMember{}, Queue: apiPriceQueue{}, ExpireMinutes: 15})
	payments := &apiPricePayments{db: db}
	handler := &upstreamhttp.Handler{Dependencies: upstreamhttp.Dependencies{Products: productServiceAdapter{products: productapp.NewService(productapp.Options{Products: products})}, ProductRepository: products, SKUs: skuStore, Users: users, MemberLevels: apiPriceMember{}, Settings: apiPriceSettings{}, Orders: orderServiceAdapter{orders: svc}, Payments: payments, DownstreamRefs: apiPriceReferences{}}}
	now := time.Now()
	credentials := apiPriceCredentials{credential: &apidomain.ApiCredential{ID: 1, UserID: user.ID, User: &user, ApiKey: "fixture-key", ApiSecret: "fixture-secret", Status: constants.ApiCredentialStatusApproved, IsActive: true, LastUsedAt: &now}}
	router := gin.New()
	signed := router.Group("/api/v1/upstream", middleware.UpstreamAPIAuthMiddleware(credentials))
	signed.GET("/products", handler.ListProducts)
	signed.GET("/products/:id", handler.GetProduct)
	signed.POST("/orders", handler.CreateOrder)
	publicHandler := orderwiring.New(&container.Container{OrderService: svc}).Create
	router.POST("/api/v1/orders", func(c *gin.Context) { c.Set("user_id", user.ID); c.Next() }, publicHandler.CreateOrder)
	call := func(method, path, body string, sign bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if sign {
			stamp := time.Now().Unix()
			request.Header.Set(upstream.HeaderApiKey, credentials.credential.ApiKey)
			request.Header.Set(upstream.HeaderTimestamp, fmt.Sprint(stamp))
			request.Header.Set(upstream.HeaderSignature, upstream.Sign(credentials.credential.ApiSecret, method, path, stamp, []byte(body)))
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	for _, path := range []string{"/api/v1/upstream/products", fmt.Sprintf("/api/v1/upstream/products/%d", product.ID)} {
		response := call(http.MethodGet, path, "", true)
		if response.Code != 200 {
			t.Fatalf("catalog: %s", response.Body.String())
		}
		var payload struct {
			Items []struct {
				Price     string          `json:"price_amount"`
				Wholesale json.RawMessage `json:"wholesale_prices"`
				SKUs      []struct {
					ID     uint   `json:"id"`
					Price  string `json:"price_amount"`
					Member string `json:"member_price"`
				} `json:"skus"`
			} `json:"items"`
			Product json.RawMessage `json:"product"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Product) > 0 {
			var one = payload.Items
			if err := json.Unmarshal([]byte("["+string(payload.Product)+"]"), &one); err != nil {
				t.Fatal(err)
			}
			payload.Items = one
		}
		if len(payload.Items) != 1 || payload.Items[0].Price != "115.00" || len(payload.Items[0].SKUs) != 3 || len(payload.Items[0].Wholesale) != 0 {
			t.Fatalf("catalog headline or tiers: %s", response.Body.String())
		}
		for index, sku := range payload.Items[0].SKUs {
			if sku.Price != skus[index].APISupplyPriceAmount.String() || sku.Member != "" {
				t.Fatalf("catalog SKU price: %s", response.Body.String())
			}
		}
	}
	unsigned := call(http.MethodPost, "/api/v1/upstream/orders", fmt.Sprintf(`{"sku_id":%d,"quantity":2,"use_api_supply_price":true}`, skus[0].ID), false)
	if unsigned.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned request accepted: %s", unsigned.Body.String())
	}
	var count int64
	db.Model(&orderdomain.Order{}).Count(&count)
	if count != 0 {
		t.Fatal("unsigned request created order")
	}
	for _, sku := range skus {
		response := call(http.MethodPost, "/api/v1/upstream/orders", fmt.Sprintf(`{"sku_id":%d,"quantity":2,"use_api_supply_price":false}`, sku.ID), true)
		var payload struct {
			OK     bool   `json:"ok"`
			ID     uint   `json:"order_id"`
			Amount string `json:"amount"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		expected := sku.APISupplyPriceAmount.Mul(decimal.NewFromInt(2)).StringFixed(2)
		if response.Code != 200 || !payload.OK || payload.Amount != expected || payments.amount != expected {
			t.Fatalf("quote/order/payment handoff mismatch: %s; handoff=%s", response.Body.String(), payments.amount)
		}
		var persisted orderdomain.Order
		if err := db.First(&persisted, payload.ID).Error; err != nil {
			t.Fatal(err)
		}
		if persisted.TotalAmount.String() != expected || !persisted.MemberDiscountAmount.IsZero() || !persisted.PromotionDiscountAmount.IsZero() || !persisted.WholesaleDiscountAmount.IsZero() {
			t.Fatalf("fixed API price discounted: %+v", persisted)
		}
	}
	public := call(http.MethodPost, "/api/v1/orders", fmt.Sprintf(`{"items":[{"product_id":%d,"sku_id":%d,"quantity":2}],"use_api_supply_price":true,"UseAPISupplyPrice":true,"api_supply_price_amount":"1.00"}`, product.ID, skus[0].ID), false)
	var normal struct {
		Status int `json:"status_code"`
		Data   struct {
			Total string `json:"total_amount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(public.Body.Bytes(), &normal); err != nil {
		t.Fatal(err)
	}
	// Legacy retail pricing: wholesale 70 beats promotion 84, then member 80% = 56 each.
	if normal.Status != 0 || normal.Data.Total != "112.00" {
		t.Fatalf("public request changed pricing context: %s", public.Body.String())
	}
	if err := skuStore.UpdateAPIPrice(product.ID, skus[0].ID, decimal.Zero); err != nil {
		t.Fatal(err)
	}
	legacy := call(http.MethodPost, "/api/v1/upstream/orders", fmt.Sprintf(`{"sku_id":%d,"quantity":2}`, skus[0].ID), true)
	if !strings.Contains(legacy.Body.String(), `"amount":"112.00"`) {
		t.Fatalf("zero API override must preserve legacy calculation: %s", legacy.Body.String())
	}
	for _, initial := range skus {
		got, err := skuStore.GetByID(initial.ID)
		if err != nil || got.PriceAmount.String() != initial.PriceAmount.String() || got.ResellerSupplyPriceAmount.String() != initial.ResellerSupplyPriceAmount.String() {
			t.Fatalf("retail/reseller terms changed: %+v %v", got, err)
		}
	}
	var forged orderapp.CreateOrderInput
	if err := json.Unmarshal([]byte(`{"use_api_supply_price":true,"UseAPISupplyPrice":true}`), &forged); err != nil {
		t.Fatal(err)
	}
	if forged.UseAPISupplyPrice {
		t.Fatal("internal pricing context must not be JSON-bindable")
	}
}
