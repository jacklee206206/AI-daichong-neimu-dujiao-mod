// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"hash/fnv"
	"time"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/modules/reseller/domain"
)

// RestartUserDomainConnect is an explicit owner action. It never writes DNS,
// changes ownership, revives a disabled domain, or silently switches a live shop.
// HTTP callers must require confirm_restart=true before invoking this method.
func (s *ManagementService) RestartUserDomainConnect(ctx context.Context, userID uint, raw string) (*DomainConnectResult, error) {
	profile, err := s.connectProfile(userID)
	if err != nil {
		return nil, err
	}
	if !s.saasMode() || !s.saasReady() {
		return nil, contract.ErrSaaSUnavailable
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = s.saasProvider.CheckPlatform(checkCtx); err != nil {
		return nil, err
	}
	root, err := validateConnectRoot(raw, s.cfg)
	if err != nil {
		return nil, err
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(root))
	lock := &s.connectMutex[hash.Sum32()%uint32(len(s.connectMutex))]
	if !lock.TryLock() {
		return nil, ErrDomainConnectBusy
	}
	defer lock.Unlock()
	now := time.Now().UTC()
	hosts := []string{root, "www." + root}
	changed := []string{}
	err = s.store.WithinManagementTransaction(func(tx contract.ManagementStore) error {
		p, e := tx.GetProfileByID(profile.ID)
		if e != nil {
			return e
		}
		if p == nil || p.Status != domain.ProfileStatusActive {
			return contract.ErrProfileInactive
		}
		all, e := tx.ListDomainsByResellerID(profile.ID)
		if e != nil {
			return e
		}
		existing := make([]*domain.Domain, 2)
		missing := 0
		legacy := false
		for i, host := range hosts {
			row, e := tx.FindDomainByHost(host)
			if e != nil {
				return e
			}
			if row != nil && (row.ResellerID != profile.ID || row.Type != domain.DomainTypeCustom || row.Status == domain.DomainStatusDisabled) {
				return contract.ErrDomainConflict
			}
			if row == nil {
				missing++
			} else {
				locked, e := tx.GetDomainByIDForUpdate(row.ID)
				if e != nil {
					return e
				}
				if locked == nil || locked.ResellerID != profile.ID || locked.Status == domain.DomainStatusDisabled {
					return contract.ErrDomainConflict
				}
				row = locked
				if row.ConnectMode != contract.DomainConnectCloudflareSaaS {
					legacy = true
				}
			}
			existing[i] = row
		}
		if len(all)+missing > 10 {
			return ErrDomainLimit
		}
		// A repeated request must not take an already converted live storefront down.
		if !legacy {
			if missing == 0 {
				return nil
			}
			return contract.ErrDomainStatusInvalid
		}
		for i, host := range hosts {
			row := existing[i]
			if row == nil {
				token := make([]byte, 24)
				if _, e = rand.Read(token); e != nil {
					return e
				}
				row, e = tx.UpsertDomain(domain.Domain{ResellerID: profile.ID, Domain: host, Type: domain.DomainTypeCustom, VerificationToken: "opengpt-" + hex.EncodeToString(token), Status: domain.DomainStatusPendingReview})
				if e != nil {
					return contract.ErrDomainConflict
				}
			}
			if row.ConnectMode == contract.DomainConnectCloudflareSaaS {
				continue
			}
			if row.VerificationToken == "" {
				token := make([]byte, 24)
				if _, e = rand.Read(token); e != nil {
					return e
				}
				row.VerificationToken = "opengpt-" + hex.EncodeToString(token)
			}
			row.ConnectMode = contract.DomainConnectCloudflareSaaS
			row.Status = domain.DomainStatusPendingReview
			row.IsPrimary = false
			row.VerificationStatus = domain.DomainVerificationPending
			row.VerifiedAt = nil
			row.LastDNSCheckAt = nil
			row.LastDNSError = ""
			row.TLSStatus = "pending"
			row.TLSReadyAt = nil
			row.LastTLSError = ""
			row.CloudflareHostnameID = ""
			row.CloudflareHostnameStatus = ""
			row.CloudflareSSLStatus = ""
			row.CloudflareValidationJSON = ""
			row.AutoConnectRequestedAt = &now
			row.AutoConnectAttempts = 0
			row.AutoConnectLastAttemptAt = nil
			row.DNSProvider = "manual"
			if e = tx.UpdateDomain(row); e != nil {
				return e
			}
			changed = append(changed, host)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, host := range changed {
		_ = cache.DelResellerDomain(ctx, host)
	}
	return s.UserDomainConnectStatus(userID, root)
}
