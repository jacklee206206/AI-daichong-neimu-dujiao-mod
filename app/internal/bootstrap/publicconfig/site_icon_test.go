// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package publicconfigwiring

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/gin-gonic/gin"
)

type iconTestSettings struct{ icon string }

func (s *iconTestSettings) GetConfig(map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"brand": jsonmap.JSON{"site_icon": s.icon}}, nil
}

type iconTestResolver struct{}

func (iconTestResolver) ResolveRequest(_ context.Context, r *http.Request) (resellercontract.TenantContext, error) {
	switch r.Host {
	case "main.test":
		return resellercontract.MainTenantContext(r.Host), nil
	case "shop.test":
		return resellercontract.ResellerTenantContext(r.Host, 9, 10, r.Host), nil
	default:
		return resellercontract.UnavailableTenantContext(r.Host, "unknown"), nil
	}
}

type iconTestOverlay struct{}

func (iconTestOverlay) ApplyPublicConfigOverlay(_ context.Context, tenant resellercontract.TenantContext, base map[string]interface{}) (map[string]interface{}, error) {
	if tenant.IsReseller() {
		return map[string]interface{}{"brand": map[string]interface{}{"site_icon": "/uploads/shop.png"}}, nil
	}
	return base, nil
}

func TestSiteIconResolverUsesCurrentTenantBrand(t *testing.T) {
	settings := &iconTestSettings{icon: " /uploads/main.jpg "}
	resolve := newSiteIconResolver(iconTestResolver{}, settings, iconTestOverlay{})
	for _, tt := range []struct{ host, want string }{
		{"main.test", "/uploads/main.jpg"}, {"shop.test", "/uploads/shop.png"},
		{"main.test", "/uploads/main.jpg"},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "https://"+tt.host+"/favicon.ico", nil)
		got, err := resolve(c)
		if err != nil || got != tt.want {
			t.Fatalf("%s: got %q, %v; want %q", tt.host, got, err, tt.want)
		}
	}
	settings.icon = "/uploads/replaced.png"
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "https://main.test/favicon.ico", nil)
	if got, err := resolve(c); err != nil || got != settings.icon {
		t.Fatalf("settings update not reflected: %q %v", got, err)
	}
	c.Request = httptest.NewRequest("GET", "https://unknown.test/favicon.ico", nil)
	if got, err := resolve(c); got != "" || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unknown host received a brand: %q %v", got, err)
	}
}
