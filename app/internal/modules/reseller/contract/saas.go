// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package contract

import (
	"context"
	"errors"
)

const DomainConnectCloudflareSaaS = "cloudflare_saas"

type SaaSHostname struct {
	ID        string
	Hostname  string
	Status    string
	SSLStatus string
	// Contains only allowlisted public DNS verification records, never raw API responses.
	ValidationRecords []DNSRecord
}

type SaaSProvider interface {
	Ready() bool
	CheckPlatform(context.Context) error
	EnsureHostname(context.Context, string, string) (*SaaSHostname, error)
}

var (
	ErrSaaSUnavailable = errors.New("平台统一域名入口尚未配置完成，请联系管理员；不会提交域名或修改 DNS")
	ErrSaaSRequest     = errors.New("Cloudflare 域名接入暂未完成，请检查平台授权、回退源站和服务状态后重试")
	ErrSaaSConflict    = errors.New("Cloudflare 已有域名配置与当前接入不一致，请管理员核对；未覆盖现有配置")
)
