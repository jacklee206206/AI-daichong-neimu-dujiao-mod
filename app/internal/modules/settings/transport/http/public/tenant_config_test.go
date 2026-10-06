// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package publicconfighttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	reseller "github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/gin-gonic/gin"
)

type tenantConfigCache struct {
	rows          map[string][]byte
	reads, writes int
}

func (s *tenantConfigCache) CacheKey(id *uint) string {
	if id == nil {
		return "main"
	}
	return fmt.Sprintf("reseller:%d", *id)
}
func (s *tenantConfigCache) GetJSON(_ context.Context, key string, out *map[string]interface{}) (bool, error) {
	s.reads++
	raw, ok := s.rows[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}
func (s *tenantConfigCache) SetJSON(_ context.Context, key string, value interface{}, _ time.Duration) error {
	s.writes++
	raw, err := json.Marshal(value)
	if err == nil {
		s.rows[key] = raw
	}
	return err
}

type tenantConfigSettings struct{ reads int }

func (s *tenantConfigSettings) GetConfig(_ map[string]interface{}) (map[string]interface{}, error) {
	s.reads++
	return map[string]interface{}{"brand": map[string]interface{}{"site_name": "Main Store", "site_url": "https://main.example.test"}}, nil
}
func (*tenantConfigSettings) GetWalletRechargeChannelIDs() []uint { return nil }
func (*tenantConfigSettings) GetWalletOnlyPayment() bool          { return false }
func (*tenantConfigSettings) GetAffiliateSettingMap() (map[string]interface{}, error) {
	return nil, nil
}
func (*tenantConfigSettings) GetSMTPEnabled() bool                             { return false }
func (*tenantConfigSettings) GetRegistrationEnabled(v bool) (bool, error)      { return v, nil }
func (*tenantConfigSettings) GetEmailVerificationEnabled(v bool) (bool, error) { return v, nil }
func (*tenantConfigSettings) GetRegistrationEmailDomainPolicy() (bool, []string, error) {
	return false, nil, nil
}
func (*tenantConfigSettings) GetByKey(string) (interface{}, error)            { return nil, nil }
func (*tenantConfigSettings) GetActiveHomeAnnouncement() (jsonmap.JSON, bool) { return nil, false }

type tenantConfigPayments struct{}

func (tenantConfigPayments) GetOrderPaymentChannels() ([]map[string]interface{}, error) {
	return nil, nil
}

type tenantConfigOverlay struct{}

func (tenantConfigOverlay) ApplyPublicConfigOverlay(_ context.Context, tenant reseller.TenantContext, data map[string]interface{}) (map[string]interface{}, error) {
	if tenant.IsReseller() {
		data["brand"] = map[string]interface{}{"site_name": fmt.Sprintf("Store %d", *tenant.ResellerID), "site_url": "https://" + tenant.Host}
		data["tenant"] = map[string]interface{}{"host": tenant.Host, "primary_domain": tenant.PrimaryDomain, "mode": "reseller"}
	}
	return data, nil
}

func configRequest(t *testing.T, h *Handler, tenant reseller.TenantContext) (int, map[string]interface{}) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "https://"+tenant.Host+"/api/v1/public/config", nil)
	c.Request = c.Request.WithContext(reseller.WithTenantContext(c.Request.Context(), tenant))
	h.GetConfig(c)
	if w.Code != http.StatusOK {
		return w.Code, nil
	}
	var body struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return w.Code, body.Data
}

func TestPublicConfigCacheKeepsRequestHostAndRealCanonicalForBothAliasOrders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, hosts := range [][]string{{"shop.example.test", "www.shop.example.test", "shop.example.test"}, {"www.shop.example.test", "shop.example.test", "www.shop.example.test"}} {
		t.Run(hosts[0], func(t *testing.T) {
			cache := &tenantConfigCache{rows: map[string][]byte{}}
			settings := &tenantConfigSettings{}
			h := NewHandler(cache, settings, tenantConfigPayments{}, nil, nil, TelegramAuthFallback{}, nil, GoogleAuthFallback{}, tenantConfigOverlay{})
			for _, host := range hosts {
				status, data := configRequest(t, h, reseller.ResellerTenantContext(host, 7, 88, "shop.example.test"))
				if status != http.StatusOK {
					t.Fatalf("status=%d", status)
				}
				meta := data["tenant"].(map[string]interface{})
				brand := data["brand"].(map[string]interface{})
				if meta["host"] != host || meta["primary_domain"] != "shop.example.test" || brand["site_url"] != "https://shop.example.test" || brand["site_name"] != "Store 7" {
					t.Fatalf("host=%s meta=%+v brand=%+v", host, meta, brand)
				}
			}
			if settings.reads != 1 || cache.writes != 1 {
				t.Fatalf("tenant config should stay cached: reads=%d writes=%d", settings.reads, cache.writes)
			}
			// A new primary takes effect even while the decoration cache remains warm.
			_, data := configRequest(t, h, reseller.ResellerTenantContext("shop.example.test", 7, 88, "www.shop.example.test"))
			if data["brand"].(map[string]interface{})["site_url"] != "https://www.shop.example.test" || data["tenant"].(map[string]interface{})["primary_domain"] != "www.shop.example.test" {
				t.Fatalf("stale canonical: %+v", data)
			}
			// A different reseller never receives the first reseller's decoration.
			_, data = configRequest(t, h, reseller.ResellerTenantContext("other.example.test", 9, 99, "other.example.test"))
			if data["brand"].(map[string]interface{})["site_name"] != "Store 9" {
				t.Fatalf("cross tenant cache: %+v", data)
			}
			for _, host := range []string{"main.example.test", "www.main.example.test"} {
				_, data = configRequest(t, h, reseller.MainTenantContext(host))
				if data["tenant"].(map[string]interface{})["host"] != host || data["brand"].(map[string]interface{})["site_url"] != "https://main.example.test" {
					t.Fatalf("main alias cache: %+v", data)
				}
			}
			reads := cache.reads
			status, _ := configRequest(t, h, reseller.UnavailableTenantContext("disabled.example.test", "not_found"))
			if status != http.StatusNotFound || cache.reads != reads {
				t.Fatalf("unavailable host consulted config cache: status=%d reads=%d", status, cache.reads)
			}
		})
	}
}

func TestPublicConfigRefreshesPreFixCachedHostAndCanonical(t *testing.T) {
	cache := &tenantConfigCache{rows: map[string][]byte{"reseller:7": []byte(`{"tenant":{"mode":"reseller","host":"www.shop.example.test","primary_domain":"www.shop.example.test"},"brand":{"site_name":"Saved Store","site_url":"https://www.shop.example.test"}}`)}}
	settings := &tenantConfigSettings{}
	h := NewHandler(cache, settings, tenantConfigPayments{}, nil, nil, TelegramAuthFallback{}, nil, GoogleAuthFallback{}, nil)
	_, data := configRequest(t, h, reseller.ResellerTenantContext("shop.example.test", 7, 88, "shop.example.test"))
	meta, brand := data["tenant"].(map[string]interface{}), data["brand"].(map[string]interface{})
	if meta["host"] != "shop.example.test" || meta["primary_domain"] != "shop.example.test" || brand["site_url"] != "https://shop.example.test" || brand["site_name"] != "Saved Store" {
		t.Fatalf("legacy cache not corrected: %+v", data)
	}
	if settings.reads != 0 || cache.writes != 0 {
		t.Fatal("metadata repair should not reload or rewrite decoration")
	}
}
