// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"hash/fnv"
	"net"
	"strings"
	"time"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/config"
	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	contract "github.com/dujiao-next/internal/modules/reseller/contract"
	domain "github.com/dujiao-next/internal/modules/reseller/domain"
	"golang.org/x/net/publicsuffix"
)

var (
	ErrDomainAutoConnectUnavailable = errors.New("平台暂未启用自动接入，请使用手动解析")
	ErrDomainRootRequired           = errors.New("请填写根域名，例如 example.com；不要填写 www、其他子域名、网址或路径。中文域名请使用 Punycode 格式")
	ErrDomainConnectBusy            = errors.New("正在处理域名接入，请稍后重试")
	ErrDomainLimit                  = errors.New("每个店铺最多可绑定 10 个域名，请联系管理员调整")
)

type DomainConnectInspection struct {
	RootDomain          string   `json:"root_domain"`
	ConnectMode         string   `json:"connect_mode"`
	EntryHost           string   `json:"entry_host"`
	PlatformReady       bool     `json:"platform_ready"`
	PlatformMessage     string   `json:"platform_message"`
	CanRestart          bool     `json:"can_restart"`
	RestartMessage      string   `json:"restart_message"`
	Provider            string   `json:"provider"`
	ProviderLabel       string   `json:"provider_label"`
	Supported           bool     `json:"supported"`
	Domains             []string `json:"domains"`
	EntryIP             string   `json:"entry_ip"`
	RequiredPermissions []string `json:"required_permissions"`
	Conflicts           []string `json:"conflicts"`
	Instructions        []string `json:"instructions"`
}
type DomainConnectResult struct {
	Domains []domain.Domain `json:"domains"`
	Phase   string          `json:"phase"`
	Message string          `json:"message"`
}

// SetDNSProvider is called only during container construction. Credentials are
// request-scoped and never become service or database fields.
func (s *ManagementService) SetDNSProvider(provider contract.DNSProvider) { s.dnsProvider = provider }
func (s *ManagementService) SetSaaSProvider(provider contract.SaaSProvider) {
	s.saasProvider = provider
}
func (s *ManagementService) saasMode() bool {
	return s.cfg.DomainConnectMode == contract.DomainConnectCloudflareSaaS
}
func (s *ManagementService) saasReady() bool {
	return validCustomDomainHost(s.cfg.DomainEntryHost) && s.saasProvider != nil && s.saasProvider.Ready()
}

func validateConnectRoot(raw string, cfg config.ResellerConfig) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.ContainsAny(raw, ":/\\?#@ \t\r\n") {
		return "", ErrDomainRootRequired
	}
	root, err := normalizeAndValidateCustomDomain(raw, cfg)
	if err != nil {
		return "", err
	}
	effective, err := publicsuffix.EffectiveTLDPlusOne(root)
	_, icann := publicsuffix.PublicSuffix(root)
	if err != nil || !icann || effective != root {
		return "", ErrDomainRootRequired
	}
	if _, err := normalizeAndValidateCustomDomain("www."+root, cfg); err != nil {
		return "", err
	}
	return root, nil
}

func (s *ManagementService) connectProfile(userID uint) (*domain.Profile, error) {
	if s == nil || s.store == nil || userID == 0 {
		return nil, productcontract.ErrNotFound
	}
	if s.cfg.DomainConnectMode != "" && s.cfg.DomainConnectMode != "legacy" && s.cfg.DomainConnectMode != contract.DomainConnectCloudflareSaaS {
		return nil, ErrDomainAutoConnectUnavailable
	}
	if !s.cfg.Enabled || !s.cfg.DomainAutoConnectEnabled || s.dnsProvider == nil {
		return nil, ErrDomainAutoConnectUnavailable
	}
	ip := net.ParseIP(s.cfg.DomainEntryIP)
	if ip == nil || ip.To4() == nil || ip.IsPrivate() || !ip.IsGlobalUnicast() {
		return nil, ErrDomainAutoConnectUnavailable
	}
	profile, err := s.store.GetProfileByUserID(userID)
	if err != nil {
		return nil, err
	}
	if profile == nil || profile.Status != domain.ProfileStatusActive {
		return nil, contract.ErrProfileInactive
	}
	return profile, nil
}

func (s *ManagementService) InspectDomainConnect(ctx context.Context, userID uint, raw string) (*DomainConnectInspection, error) {
	profile, err := s.connectProfile(userID)
	if err != nil {
		return nil, err
	}
	root, err := validateConnectRoot(raw, s.cfg)
	if err != nil {
		return nil, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	inspection, err := s.dnsProvider.Detect(checkCtx, root)
	if err != nil {
		return nil, err
	}
	result := &DomainConnectInspection{RootDomain: root, Provider: inspection.Provider, ProviderLabel: inspection.ProviderLabel, Supported: inspection.Supported,
		Domains: []string{root, "www." + root}, EntryIP: s.cfg.DomainEntryIP, RequiredPermissions: inspection.RequiredPermissions, Conflicts: []string{}, Instructions: []string{
			"支持 .com、.xyz、.cn、.shop、.net、.vip、.cc、.top 等正常注册的根域名；域名在哪里买不影响接入，以当前权威 DNS 为准。",
			"检测后使用该域名的专用 API 授权；授权信息仅用于本次请求，不保存。根域名与 www 会一起接入。",
			"授权后会先检查全部现有解析；冲突时停止，只补充缺失记录。首次接入使用仅 DNS（灰色云朵），证书验证后才启用。",
		}}
	result.ConnectMode = "legacy"
	result.PlatformReady = true
	if s.saasMode() {
		result.ConnectMode = contract.DomainConnectCloudflareSaaS
		result.EntryHost = s.cfg.DomainEntryHost
		result.PlatformReady = s.saasReady()
		if result.PlatformReady {
			if e := s.saasProvider.CheckPlatform(checkCtx); e != nil {
				result.PlatformReady = false
				result.PlatformMessage = e.Error()
			}
		}
		if !result.PlatformReady && result.PlatformMessage == "" {
			result.PlatformMessage = contract.ErrSaaSUnavailable.Error()
		}
		result.Instructions = []string{
			"支持 .com、.xyz、.net、.org 等标准公网根域名，域名在哪里买都可以。",
			"提交后为根域名和 www 分别添加页面显示的 CNAME 与专属 TXT 记录；平台会持续验证并自动签发证书。",
			"也可选择 DNS 授权代写记录，仅补充缺失记录，遇到冲突停止；授权信息不保存。",
			"首次接入建议仅 DNS（灰色云朵）。Cloudflare 同账号域名必须使用仅 DNS；不同账号接入完成后可选代理并使用完全（严格）。",
		}
	}
	for _, host := range result.Domains {
		row, e := s.store.FindDomainByHost(host)
		if e != nil {
			return nil, e
		}
		if row != nil && (row.ResellerID != profile.ID || row.Type != domain.DomainTypeCustom || row.Status == domain.DomainStatusDisabled) {
			result.Conflicts = append(result.Conflicts, host+" 已被绑定或被停用，请联系管理员处理")
		} else if s.saasMode() && row != nil && row.ConnectMode != contract.DomainConnectCloudflareSaaS {
			result.CanRestart = true
			result.RestartMessage = "检测到本人之前的域名绑定。切换到统一入口会暂停旧绑定，保留店铺与专属 TXT；请将旧 A 解析改为页面指定的 CNAME，全部 HTTPS 检测通过后恢复。系统不会自动覆盖任何现有解析。"
		} else if isReadyManualDomain(row) {
			result.Conflicts = append(result.Conflicts, host+" 已手动启用，无需重复自动接入；如需改接，请联系管理员")
		}
	}
	if len(result.Conflicts) > 0 {
		result.CanRestart = false
	}
	return result, nil
}

func (s *ManagementService) ConnectUserDomain(ctx context.Context, userID uint, raw, provider string, credentials contract.DNSCredentials) (*DomainConnectResult, error) {
	defer clear(credentials)
	profile, err := s.connectProfile(userID)
	if err != nil {
		return nil, err
	}
	if s.saasMode() && !s.saasReady() {
		return nil, contract.ErrSaaSUnavailable
	}
	manual := s.saasMode() && provider == "manual"
	root, err := validateConnectRoot(raw, s.cfg)
	if err != nil {
		return nil, err
	}
	// Avoid racing simultaneous API requests for the same reserved pair. The
	// database also enforces unique live domain names across application instances.
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(root))
	lock := &s.connectMutex[hasher.Sum32()%uint32(len(s.connectMutex))]
	if !lock.TryLock() {
		return nil, ErrDomainConnectBusy
	}
	defer lock.Unlock()
	checkCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if s.saasMode() {
		if err = s.saasProvider.CheckPlatform(checkCtx); err != nil {
			return nil, err
		}
	}
	if !manual {
		if err = s.dnsProvider.Authorize(checkCtx, root, provider, credentials); err != nil {
			return nil, err
		}
	}
	rows, err := s.reserveConnectPair(profile.ID, root)
	if err != nil {
		return nil, err
	}
	if rows[0].Status == domain.DomainStatusActive && rows[1].Status == domain.DomainStatusActive &&
		domainHasTLSProof(&rows[0]) && domainHasTLSProof(&rows[1]) {
		return domainConnectState(rows), nil
	}
	records := make([]contract.DNSRecord, 0, 4)
	for _, row := range rows {
		target := contract.DNSRecord{Type: "A", Name: row.Domain, Value: s.cfg.DomainEntryIP}
		if s.saasMode() {
			target.Type = "CNAME"
			target.Value = s.cfg.DomainEntryHost
		}
		records = append(records, target, contract.DNSRecord{Type: "TXT", Name: "_opengpt-verification." + row.Domain, Value: row.VerificationToken})
	}
	if !manual {
		if err = s.dnsProvider.EnsureRecords(checkCtx, root, provider, credentials, records); err != nil {
			return nil, err
		}
	}
	// Manual submission queues verification only; ownership proof must pass before
	// any Cloudflare mutation. Authorized mode queues after all DNS writes succeed.
	// Any partial provider failure leaves records and tokens available for retry.
	now := time.Now().UTC()
	recoveredHosts := make([]string, 0, len(rows))
	err = s.store.WithinManagementTransaction(func(tx contract.ManagementStore) error {
		for _, row := range rows {
			current, e := tx.GetDomainByIDForUpdate(row.ID)
			if e != nil {
				return e
			}
			p, e := tx.GetProfileByID(profile.ID)
			if e != nil {
				return e
			}
			if p == nil || p.Status != domain.ProfileStatusActive {
				return contract.ErrProfileInactive
			}
			if current == nil || current.ResellerID != profile.ID || current.VerificationToken != row.VerificationToken || current.Status == domain.DomainStatusDisabled {
				return contract.ErrDomainStatusInvalid
			}
			// A manual approval alone is not a provisioned shop. Only after the
			// authorized DNS operation succeeds may it enter the normal queue.
			// Do not take over a manual site that became ready during that request.
			if isReadyManualDomain(current) {
				return contract.ErrDomainConflict
			}
			if current.Status == domain.DomainStatusActive && current.AutoConnectRequestedAt == nil {
				current.Status = domain.DomainStatusPendingReview
				current.VerificationStatus = domain.DomainVerificationPending
				current.VerifiedAt = nil
				current.TLSStatus = "pending"
				current.TLSReadyAt = nil
				current.LastDNSCheckAt = nil
				current.LastDNSError = ""
				current.LastTLSError = ""
				current.AutoConnectAttempts = 0
				current.AutoConnectLastAttemptAt = nil
				current.IsPrimary = false
				recoveredHosts = append(recoveredHosts, current.Domain)
			}
			current.ConnectMode = "legacy"
			if s.saasMode() {
				current.ConnectMode = contract.DomainConnectCloudflareSaaS
			}
			current.DNSProvider = provider
			if current.Status != domain.DomainStatusActive && current.AutoConnectRequestedAt == nil {
				current.AutoConnectRequestedAt = &now
			}
			if e = tx.UpdateDomain(current); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, host := range recoveredHosts {
		_ = cache.DelResellerDomain(ctx, host)
	}
	return s.UserDomainConnectStatus(userID, root)
}

func domainHasTLSProof(row *domain.Domain) bool {
	return row != nil && row.TLSStatus == "ready" && row.TLSReadyAt != nil
}

func isReadyManualDomain(row *domain.Domain) bool {
	return row != nil && row.Status == domain.DomainStatusActive && row.AutoConnectRequestedAt == nil && domainHasTLSProof(row)
}

func (s *ManagementService) reserveConnectPair(resellerID uint, root string) ([]domain.Domain, error) {
	rows := []domain.Domain{}
	err := s.store.WithinManagementTransaction(func(tx contract.ManagementStore) error {
		profile, e := tx.GetProfileByID(resellerID)
		if e != nil {
			return e
		}
		if profile == nil || profile.Status != domain.ProfileStatusActive {
			return contract.ErrProfileInactive
		}
		all, e := tx.ListDomainsByResellerID(resellerID)
		if e != nil {
			return e
		}
		existing := make([]*domain.Domain, 2)
		missing := 0
		for i, host := range []string{root, "www." + root} {
			row, e := tx.FindDomainByHost(host)
			if e != nil {
				return e
			}
			if row != nil && (row.ResellerID != resellerID || row.Type != domain.DomainTypeCustom || row.Status == domain.DomainStatusDisabled) {
				return contract.ErrDomainConflict
			}
			if row != nil && (row.AutoConnectRequestedAt != nil || row.Status == domain.DomainStatusActive) && ((row.ConnectMode == contract.DomainConnectCloudflareSaaS) != s.saasMode()) {
				return contract.ErrDomainConflict
			}
			if isReadyManualDomain(row) {
				return contract.ErrDomainConflict
			}
			existing[i] = row
			if row == nil {
				missing++
			}
		}
		if len(all)+missing > 10 {
			return ErrDomainLimit
		}
		for i, host := range []string{root, "www." + root} {
			row := existing[i]
			if row == nil {
				buf := make([]byte, 24)
				if _, e = rand.Read(buf); e != nil {
					return e
				}
				row, e = tx.UpsertDomain(domain.Domain{ResellerID: resellerID, Domain: host, Type: domain.DomainTypeCustom, VerificationToken: "opengpt-" + hex.EncodeToString(buf), VerificationStatus: domain.DomainVerificationPending, TLSStatus: "pending", Status: domain.DomainStatusPendingReview})
				if e != nil {
					return contract.ErrDomainConflict
				}
			}
			// An unqueued reservation still needs to display the correct manual records
			// after a DNS provider preflight failure.
			if row.AutoConnectRequestedAt == nil && row.Status != domain.DomainStatusActive {
				mode := "legacy"
				if s.saasMode() {
					mode = contract.DomainConnectCloudflareSaaS
				}
				if row.ConnectMode != mode {
					row.ConnectMode = mode
					if e = tx.UpdateDomain(row); e != nil {
						return e
					}
				}
			}
			row.Profile = nil
			rows = append(rows, *row)
		}
		return nil
	})
	return rows, err
}

func (s *ManagementService) UserDomainConnectStatus(userID uint, raw string) (*DomainConnectResult, error) {
	profile, err := s.connectProfile(userID)
	if err != nil {
		return nil, err
	}
	root, err := validateConnectRoot(raw, s.cfg)
	if err != nil {
		return nil, err
	}
	rows := []domain.Domain{}
	for _, host := range []string{root, "www." + root} {
		row, e := s.store.FindDomainByHost(host)
		if e != nil {
			return nil, e
		}
		if row == nil || row.ResellerID != profile.ID || row.Type != domain.DomainTypeCustom {
			return nil, productcontract.ErrNotFound
		}
		row.Profile = nil
		rows = append(rows, *row)
	}
	return domainConnectState(rows), nil
}

func domainConnectState(rows []domain.Domain) *DomainConnectResult {
	result := &DomainConnectResult{Domains: rows, Phase: "active", Message: "根域名与 www 已完成解析和 HTTPS 验证，可以访问店铺。"}
	for _, row := range rows {
		if row.Status == domain.DomainStatusDisabled {
			result.Phase = "failed"
			result.Message = "部分域名已被停用，请联系管理员处理。"
			return result
		}
	}
	for _, row := range rows {
		if row.Status == domain.DomainStatusActive && row.AutoConnectRequestedAt == nil && !domainHasTLSProof(&row) {
			result.Phase = "waiting_authorization"
			result.Message = "域名已通过人工审核，但尚未完成 HTTPS 接入；请授权 DNS 后继续接入。"
			return result
		}
	}
	for _, row := range rows {
		if row.TLSStatus == "failed" {
			result.Phase = "failed"
			result.Message = "部分域名接入失败，请查看对应 DNS 或证书错误后重试；连续证书失败需管理员处理。"
			return result
		}
	}
	for _, row := range rows {
		if row.AutoConnectRequestedAt == nil && row.Status != domain.DomainStatusActive {
			result.Phase = "waiting_authorization"
			result.Message = "请完成 DNS 授权，系统会检查现有解析并补充缺失记录。"
			return result
		}
	}
	for _, row := range rows {
		if row.VerificationStatus != domain.DomainVerificationVerified {
			result.Phase = "dns_pending"
			result.Message = "解析记录已提交，正在等待 DNS 生效。后台会继续检测，请稍后刷新。"
			if row.DNSProvider == "manual" {
				result.Message = "域名已提交，请按页面添加 CNAME 与 TXT 记录。后台会持续检测 DNS，无需保持页面打开。"
			}
			return result
		}
	}
	for _, row := range rows {
		if row.ConnectMode == contract.DomainConnectCloudflareSaaS && (row.CloudflareHostnameStatus != "active" || row.CloudflareSSLStatus != "active") {
			result.Phase = "edge_pending"
			result.Message = "归属已验证，正在验证统一入口并签发 Cloudflare 边缘证书；如有额外 TXT 记录请按提示添加。"
			return result
		}
	}
	for _, row := range rows {
		if row.Status != domain.DomainStatusActive || !domainHasTLSProof(&row) {
			result.Phase = "tls_pending"
			result.Message = "DNS 已验证，正在签发并核验 HTTPS 证书。后台会继续处理。"
			return result
		}
	}
	return result
}
