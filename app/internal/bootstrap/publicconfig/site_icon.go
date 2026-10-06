// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package publicconfigwiring

import (
	"context"
	"io/fs"
	"net/http"
	"strings"

	"github.com/dujiao-next/internal/app/container"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/gin-gonic/gin"
)

type iconTenantResolver interface {
	ResolveRequest(context.Context, *http.Request) (resellercontract.TenantContext, error)
}

type iconSettings interface {
	GetConfig(map[string]interface{}) (map[string]interface{}, error)
}

type iconOverlay interface {
	ApplyPublicConfigOverlay(context.Context, resellercontract.TenantContext, map[string]interface{}) (map[string]interface{}, error)
}

// NewSiteIconResolver shares the public configuration's tenant and brand rules.
// It reads on each request so replacing an icon in settings takes effect without a restart.
func NewSiteIconResolver(c *container.Container) func(*gin.Context) (string, error) {
	var resolver iconTenantResolver
	if c.ResellerDomainResolver != nil {
		resolver = c.ResellerDomainResolver
	}
	var overlay iconOverlay
	if c.ResellerSiteConfigService != nil {
		overlay = c.ResellerSiteConfigService
	}
	return newSiteIconResolver(resolver, c.SettingService, overlay)
}

func newSiteIconResolver(resolver iconTenantResolver, settings iconSettings, overlay iconOverlay) func(*gin.Context) (string, error) {
	return func(c *gin.Context) (string, error) {
		ctx := c.Request.Context()
		tenant := resellercontract.MainTenantContext(c.Request.Host)
		if resolver != nil {
			var err error
			tenant, err = resolver.ResolveRequest(ctx, c.Request)
			if err != nil {
				return "", err
			}
		}
		if tenant.Unavailable {
			return "", fs.ErrNotExist
		}
		base, err := settings.GetConfig(nil)
		if err != nil {
			return "", err
		}
		if overlay != nil {
			base, err = overlay.ApplyPublicConfigOverlay(ctx, tenant, base)
			if err != nil {
				return "", err
			}
		}
		var brand map[string]interface{}
		switch v := base["brand"].(type) {
		case map[string]interface{}:
			brand = v
		case jsonmap.JSON:
			brand = map[string]interface{}(v)
		}
		icon, _ := brand["site_icon"].(string)
		return strings.TrimSpace(icon), nil
	}
}
