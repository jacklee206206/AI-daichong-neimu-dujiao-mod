// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
)

const domainProbePath = "/.well-known/opengpt-reseller-verification"

type DomainDNSRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}
type DomainConnectionSetup struct {
	Domain       *resellerdomain.Domain `json:"domain"`
	Records      []DomainDNSRecord      `json:"records"`
	Instructions []string               `json:"instructions"`
}

type domainDNSResolver interface {
	LookupTXT(context.Context, string) ([]string, error)
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

// DomainConnectionSetupForUser never reveals another reseller's domain or challenge.
func (s *ManagementService) DomainConnectionSetupForUser(userID, domainID uint) (*DomainConnectionSetup, error) {
	profile, err := s.store.GetProfileByUserID(userID)
	if err != nil {
		return nil, err
	}
	if profile == nil || profile.Status != resellerdomain.ProfileStatusActive {
		return nil, resellercontract.ErrProfileInactive
	}
	row, err := s.store.GetDomainByID(domainID)
	if err != nil {
		return nil, err
	}
	if row == nil || row.ResellerID != profile.ID || row.Type != resellerdomain.DomainTypeCustom {
		return nil, productcontract.ErrNotFound
	}
	row.Profile = nil
	records := []DomainDNSRecord{}
	if row.VerificationToken != "" {
		records = append(records, DomainDNSRecord{"TXT", "_opengpt-verification." + row.Domain, row.VerificationToken})
	}
	if ip := net.ParseIP(strings.TrimSpace(s.cfg.DomainEntryIP)); ip != nil && ip.To4() != nil {
		records = append(records, DomainDNSRecord{"A", row.Domain, ip.String()})
	}
	if row.ConnectMode == resellercontract.DomainConnectCloudflareSaaS {
		records = []DomainDNSRecord{{"CNAME", row.Domain, s.cfg.DomainEntryHost}, {"TXT", "_opengpt-verification." + row.Domain, row.VerificationToken}}
		records = append(records, decodeSaaSRecords(row)...)
		return &DomainConnectionSetup{Domain: row, Records: records, Instructions: []string{
			"添加指向统一入口的 CNAME 和该域名专属 TXT；平台会持续检测，关闭页面也会继续处理。",
			"首次接入使用仅 DNS（灰色云朵）。Cloudflare 同账号域名需保留仅 DNS；不同账号域名接入成功后可选代理并使用完全（严格）。",
			"若下面显示额外的 Cloudflare TXT 验证记录，请一并添加。根域名与 www 使用各自的 TXT 值，记录需持续保留。",
			"域名归属、边缘证书、源站证书和公网 HTTPS 全部通过后才会启用店铺。",
		}}, nil
	}
	return &DomainConnectionSetup{Domain: row, Records: records, Instructions: []string{
		"首次接入请使用仅 DNS（灰色云朵），并移除指向其他服务器的 A、AAAA 记录；支持最终解析到入口 IP 的 CNAME。",
		"添加 TXT 所有权证明和网站解析后点击检测；通过后由平台签发证书，并实际验证 HTTPS 后启用。",
		"根域名与 www 是两个独立域名，请分别提交并使用各自的 TXT 验证值。",
		"接入完成后可开启 Cloudflare 代理，SSL/TLS 使用完全（严格）。请保留 TXT 记录。",
	}}, nil
}

func verifyDomainDNS(ctx context.Context, row *resellerdomain.Domain, entryIP string, resolver domainDNSResolver) error {
	ip := net.ParseIP(strings.TrimSpace(entryIP))
	if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return errors.New("平台尚未配置有效公网入口 IP，请联系管理员。")
	}
	if err := verifyDomainOwnership(ctx, row, resolver); err != nil {
		return err
	}
	addresses, err := resolver.LookupIPAddr(ctx, row.Domain)
	if err != nil || len(addresses) == 0 {
		return errors.New("未查到网站解析，请添加 A 记录或有效的 CNAME 记录。")
	}
	for _, address := range addresses {
		if !address.IP.Equal(ip) {
			return errors.New("网站解析尚未全部指向平台入口。首次接入请关闭 Cloudflare 代理，并移除其他 A、AAAA 记录。")
		}
	}
	return nil
}

func (s *ManagementService) VerifyUserDomain(ctx context.Context, userID, domainID uint) (*DomainConnectionSetup, error) {
	return s.verifyUserDomain(ctx, userID, domainID, net.DefaultResolver)
}
func (s *ManagementService) verifyUserDomain(ctx context.Context, userID, domainID uint, resolver domainDNSResolver) (*DomainConnectionSetup, error) {
	setup, err := s.DomainConnectionSetupForUser(userID, domainID)
	if err != nil {
		return nil, err
	}
	row := setup.Domain
	if row.Status == resellerdomain.DomainStatusDisabled || row.VerificationToken == "" {
		return nil, resellercontract.ErrDomainStatusInvalid
	}
	now := time.Now().UTC()
	if row.LastDNSCheckAt != nil && now.Sub(*row.LastDNSCheckAt) < 10*time.Second {
		return setup, nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	checkErr := verifyDomainDNS(checkCtx, row, s.cfg.DomainEntryIP, resolver)
	if row.ConnectMode == resellercontract.DomainConnectCloudflareSaaS {
		checkErr = verifySaaSDNS(checkCtx, row, resolver)
	}
	err = s.store.WithinManagementTransaction(func(tx resellercontract.ManagementStore) error {
		current, err := tx.GetDomainByIDForUpdate(domainID)
		if err != nil {
			return err
		}
		if current == nil || current.ResellerID != row.ResellerID || current.VerificationToken != row.VerificationToken || current.Status == resellerdomain.DomainStatusDisabled {
			return resellercontract.ErrDomainStatusInvalid
		}
		current.LastDNSCheckAt = &now
		if checkErr == nil {
			current.VerificationStatus = resellerdomain.DomainVerificationVerified
			current.VerifiedAt = &now
			current.LastDNSError = ""
		} else {
			current.LastDNSError = checkErr.Error()
			// Transient DNS failures must not disable an already live shop.
			if current.Status != resellerdomain.DomainStatusActive {
				current.VerificationStatus = resellerdomain.DomainVerificationPending
				current.VerifiedAt = nil
				if current.AutoConnectRequestedAt == nil {
					current.TLSStatus = "pending"
				}
				current.TLSReadyAt = nil
			}
		}
		current.UpdatedAt = now
		return tx.UpdateDomain(current)
	})
	if err != nil {
		return nil, err
	}
	return s.DomainConnectionSetupForUser(userID, domainID)
}

// verifyDomainConnectionForApproval never trusts a submitted TLS status. The
// destination is the operator-configured IP, not user-provided DNS or a URL.
func (s *ManagementService) verifyDomainConnectionForApproval(ctx context.Context, domainID uint) error {
	row, err := s.store.GetDomainByID(domainID)
	if err != nil {
		return err
	}
	if row == nil {
		return productcontract.ErrNotFound
	}
	if row.Type != resellerdomain.DomainTypeCustom {
		return nil
	}
	if row.ConnectMode == resellercontract.DomainConnectCloudflareSaaS {
		return resellercontract.ErrDomainStatusInvalid
	}
	if row.Profile == nil || row.Profile.Status != resellerdomain.ProfileStatusActive {
		return resellercontract.ErrProfileInactive
	}
	if row.Status != resellerdomain.DomainStatusPendingReview && row.Status != resellerdomain.DomainStatusDisabled {
		return resellercontract.ErrDomainStatusInvalid
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	checkErr := verifyDomainDNS(checkCtx, row, s.cfg.DomainEntryIP, net.DefaultResolver)
	tlsReady := false
	if checkErr == nil {
		checkErr = probeDomainHTTPS(checkCtx, row.Domain, s.cfg.DomainEntryIP, row.VerificationToken)
		tlsReady = checkErr == nil
	}
	now := time.Now().UTC()
	err = s.store.WithinManagementTransaction(func(tx resellercontract.ManagementStore) error {
		current, err := tx.GetDomainByIDForUpdate(domainID)
		if err != nil {
			return err
		}
		if current == nil || current.VerificationToken != row.VerificationToken || current.Status != row.Status {
			return resellercontract.ErrDomainStatusInvalid
		}
		current.LastDNSCheckAt = &now
		if tlsReady {
			current.VerificationStatus = resellerdomain.DomainVerificationVerified
			current.VerifiedAt = &now
			current.TLSStatus = "ready"
			current.TLSReadyAt = &now
			current.LastDNSError = ""
		} else {
			current.TLSStatus = "pending"
			current.TLSReadyAt = nil
			current.LastDNSError = checkErr.Error()
		}
		return tx.UpdateDomain(current)
	})
	if err != nil {
		return err
	}
	if checkErr != nil {
		return fmt.Errorf("%w: %s", resellercontract.ErrDomainStatusInvalid, checkErr)
	}
	return nil
}

func probeDomainHTTPS(ctx context.Context, host, entryIP, token string) error {
	ip := net.ParseIP(strings.TrimSpace(entryIP))
	if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return errors.New("平台入口 IP 无效。")
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), "443"))
		},
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect is not allowed") }}
	return probeDomainHTTPSWithClient(ctx, client, host, token)
}
func probeDomainHTTPSWithClient(ctx context.Context, client *http.Client, host, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+domainProbePath, nil)
	if err != nil {
		return errors.New("域名 HTTPS 检查地址无效。")
	}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("HTTPS 证书或连接检查未通过，请完成平台证书签发后重试。")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 256))
	if err != nil || res.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != token {
		return errors.New("HTTPS 接入探针不匹配，请核对该域名的平台 Nginx 配置。")
	}
	return nil
}

// Keep hostnames safe for DNS, TLS SNI and the managed Nginx template. IDNs must
// be supplied in their ASCII (punycode) form.
func validCustomDomainHost(host string) bool {
	if len(host) > 253 || !strings.Contains(host, ".") || net.ParseIP(host) != nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func verifyDomainOwnership(ctx context.Context, row *resellerdomain.Domain, resolver domainDNSResolver) error {
	if row.VerificationToken == "" {
		return errors.New("域名缺少所有权验证记录，请重新提交。")
	}
	records, err := resolver.LookupTXT(ctx, "_opengpt-verification."+row.Domain)
	if err != nil {
		return errors.New("尚未查到 TXT 记录，请核对记录并等待 DNS 生效后重试。")
	}
	matched := false
	for _, record := range records {
		if strings.TrimSpace(record) == row.VerificationToken {
			matched = true
			break
		}
	}
	if !matched {
		return errors.New("TXT 验证值不匹配，请使用该域名页面显示的专属验证值。")
	}
	return nil
}
