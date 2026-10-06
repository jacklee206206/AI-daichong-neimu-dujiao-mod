// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package contract

import (
	"context"
	"errors"
)

// DNSCredentials is request-scoped. Implementations must never persist or log it.
type DNSCredentials map[string]string

type DNSRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type DNSInspection struct {
	Provider            string   `json:"provider"`
	ProviderLabel       string   `json:"provider_label"`
	Supported           bool     `json:"supported"`
	NameServers         []string `json:"name_servers"`
	RequiredPermissions []string `json:"required_permissions"`
}

type DNSProvider interface {
	Detect(context.Context, string) (DNSInspection, error)
	Authorize(context.Context, string, string, DNSCredentials) error
	EnsureRecords(context.Context, string, string, DNSCredentials, []DNSRecord) error
}

var (
	ErrDNSProviderUnsupported = errors.New("当前权威 DNS 不支持自动接入，请使用手动解析")
	ErrDNSCredentialsInvalid  = errors.New("DNS 授权信息不完整或格式不正确")
	ErrDNSLookupFailed        = errors.New("未能检测权威 DNS，请确认域名已生效后重试")
	ErrDNSProviderMismatch    = errors.New("DNS 平台与当前权威 DNS 不一致，请重新检测")
	ErrDNSRecordConflict      = errors.New("发现现有解析冲突或停用、代理记录，已停止接入且未覆盖记录；请先在 DNS 平台核对")
	ErrDNSProviderRequest     = errors.New("DNS 平台请求未完成，请检查域名授权与权限后重试；已添加的记录会保留并在重试时复用")
	ErrDNSRecordInvalid       = errors.New("接入解析记录不合法")
)
