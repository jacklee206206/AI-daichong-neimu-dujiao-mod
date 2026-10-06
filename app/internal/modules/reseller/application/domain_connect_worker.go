// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"context"
	"net"
	"time"

	"github.com/dujiao-next/internal/cache"
	contract "github.com/dujiao-next/internal/modules/reseller/contract"
	domain "github.com/dujiao-next/internal/modules/reseller/domain"
)

type autoConnectStore interface {
	ListAutoConnectDomains(int) ([]domain.Domain, error)
}

// ProcessDomainConnections runs even after the browser closes. The unprivileged
// application verifies DNS; a separate root timer provisions Nginx/certificates.
// A provisioner's database flag alone never grants an active tenant binding.
func (s *ManagementService) ProcessDomainConnections(ctx context.Context) error {
	if s == nil || !s.cfg.Enabled || !s.cfg.DomainAutoConnectEnabled {
		return nil
	}
	repo, ok := s.store.(autoConnectStore)
	if !ok {
		return ErrDomainAutoConnectUnavailable
	}
	rows, err := repo.ListAutoConnectDomains(20)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	for _, row := range rows {
		if bounded.Err() != nil {
			break
		}
		if row.LastDNSCheckAt != nil && time.Since(*row.LastDNSCheckAt) < 30*time.Second {
			continue
		}
		if err = s.progressDomainConnection(bounded, &row, net.DefaultResolver, probeDomainHTTPS); err != nil {
			return err
		}
	}
	return nil
}

func (s *ManagementService) progressDomainConnection(ctx context.Context, row *domain.Domain, resolver domainDNSResolver, probe func(context.Context, string, string, string) error) error {
	if row == nil || row.AutoConnectRequestedAt == nil || row.Status != domain.DomainStatusPendingReview || row.Type != domain.DomainTypeCustom {
		return nil
	}
	if row.ConnectMode == contract.DomainConnectCloudflareSaaS {
		return s.progressSaaSConnection(ctx, row, resolver, probe, probeSaaSEdgeHTTPS)
	}
	checkCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	dnsErr := verifyDomainDNS(checkCtx, row, s.cfg.DomainEntryIP, resolver)
	var tlsErr error
	tlsChecked := dnsErr == nil && row.TLSStatus == "provisioned"
	if tlsChecked {
		tlsErr = probe(checkCtx, row.Domain, s.cfg.DomainEntryIP, row.VerificationToken)
	}
	now := time.Now().UTC()
	activated := false
	err := s.store.WithinManagementTransaction(func(tx contract.ManagementStore) error {
		current, e := tx.GetDomainByIDForUpdate(row.ID)
		if e != nil {
			return e
		}
		if current == nil || current.Status != domain.DomainStatusPendingReview || current.ResellerID != row.ResellerID || current.VerificationToken != row.VerificationToken || current.AutoConnectRequestedAt == nil {
			return nil
		}
		profile, e := tx.GetProfileByID(current.ResellerID)
		if e != nil {
			return e
		}
		if profile == nil || profile.Status != domain.ProfileStatusActive {
			return nil
		}
		current.LastDNSCheckAt = &now
		if dnsErr != nil {
			current.LastDNSError = dnsErr.Error()
			current.VerificationStatus = domain.DomainVerificationPending
			current.VerifiedAt = nil
			return tx.UpdateDomain(current)
		}
		current.VerificationStatus = domain.DomainVerificationVerified
		current.VerifiedAt = &now
		current.LastDNSError = ""
		// Check both the original snapshot and current state. A concurrent disable,
		// new certificate attempt or ownership rotation cancels this activation.
		if tlsChecked && current.TLSStatus == "provisioned" {
			if tlsErr != nil {
				current.LastTLSError = tlsErr.Error()
			} else {
				current.LastTLSError = ""
				current.TLSStatus = "ready"
				current.TLSReadyAt = &now
				current.Status = domain.DomainStatusActive
				all, e := tx.ListDomainsByResellerID(current.ResellerID)
				if e != nil {
					return e
				}
				if !hasActiveVerifiedPrimary(all, current.ID) {
					current.IsPrimary = true
				}
				activated = true
			}
		}
		return tx.UpdateDomain(current)
	})
	if err == nil && activated {
		_ = cache.DelResellerDomain(ctx, row.Domain)
	}
	return err
}
