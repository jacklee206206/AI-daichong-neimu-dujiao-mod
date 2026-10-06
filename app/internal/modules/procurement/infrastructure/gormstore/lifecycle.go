// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package gormstore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/constants"
	procurementcontract "github.com/dujiao-next/internal/modules/procurement/contract"
	procurementdomain "github.com/dujiao-next/internal/modules/procurement/domain"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	"github.com/dujiao-next/internal/queue"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/telegramidentity"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Lifecycle owns the procurement-side persistence operations that span
// fulfillments and parent/child order status. It deliberately uses private
// records through the procurement-owned store.
type Lifecycle struct {
	db                 *gorm.DB
	queue              StatusEmailQueue
	settings           *settingsapp.Service
	defaultEmailConfig config.EmailConfig
}

var _ procurementcontract.OrderLifecycle = (*Lifecycle)(nil)
var _ procurementcontract.CallbackLifecycle = (*Lifecycle)(nil)

type StatusEmailQueue interface {
	EnqueueOrderStatusEmail(payload queue.OrderStatusEmailPayload, opts ...asynq.Option) error
}

type lifecycleOrderRecord struct {
	ID         uint                   `gorm:"primarykey"`
	ParentID   *uint                  `gorm:"index"`
	UserID     uint                   `gorm:"index;not null"`
	GuestEmail string                 `gorm:"index"`
	Status     string                 `gorm:"index;not null"`
	DeletedAt  gorm.DeletedAt         `gorm:"index"`
	Children   []lifecycleOrderRecord `gorm:"foreignKey:ParentID"`
}

func (lifecycleOrderRecord) TableName() string { return "orders" }

type lifecycleUserRecord struct {
	ID        uint `gorm:"primarykey"`
	Email     string
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (lifecycleUserRecord) TableName() string { return "users" }

type lifecycleFulfillmentRecord struct {
	ID            uint           `gorm:"primarykey"`
	OrderID       uint           `gorm:"uniqueIndex;not null"`
	Type          string         `gorm:"not null"`
	Status        string         `gorm:"not null"`
	Payload       string         `gorm:"type:text"`
	LogisticsJSON jsonmap.JSON   `gorm:"type:json"`
	DeliveredAt   *time.Time     `gorm:"index"`
	CreatedAt     time.Time      `gorm:"index"`
	UpdatedAt     time.Time      `gorm:"index"`
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

func (lifecycleFulfillmentRecord) TableName() string { return "fulfillments" }

func NewLifecycle(
	db *gorm.DB,
	queueClient StatusEmailQueue,
	settings *settingsapp.Service,
	defaultEmailConfig config.EmailConfig,
) *Lifecycle {
	return &Lifecycle{
		db: db, queue: queueClient, settings: settings, defaultEmailConfig: defaultEmailConfig,
	}
}

func (s *Store) NewLifecycle(
	queueClient StatusEmailQueue,
	settings *settingsapp.Service,
	defaultEmailConfig config.EmailConfig,
) *Lifecycle {
	return NewLifecycle(s.db, queueClient, settings, defaultEmailConfig)
}

func (l *Lifecycle) CreateUpstreamFulfillment(orderID uint, fulfillment *procurementcontract.Fulfillment, now time.Time) error {
	if fulfillment == nil || strings.TrimSpace(fulfillment.Payload) == "" {
		return fmt.Errorf("upstream delivery contains no fulfillment payload")
	}
	deliveredAt := fulfillment.DeliveredAt
	if deliveredAt == nil {
		deliveredAt = &now
	}
	return l.db.Transaction(func(tx *gorm.DB) error {
		var existing lifecycleFulfillmentRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("order_id = ?", orderID).
			First(&existing).Error
		if err == nil {
			if existing.Type != constants.FulfillmentTypeUpstream || existing.Payload != fulfillment.Payload {
				return fmt.Errorf("order already has a different fulfillment")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(&lifecycleFulfillmentRecord{
			OrderID: orderID, Type: constants.FulfillmentTypeUpstream,
			Status: constants.FulfillmentStatusDelivered, Payload: fulfillment.Payload,
			LogisticsJSON: fulfillment.DeliveryData, DeliveredAt: deliveredAt,
			CreatedAt: now, UpdatedAt: now,
		}).Error
	})
}

func (l *Lifecycle) SyncParentStatus(parentID uint, now time.Time) (string, error) {
	if parentID == 0 {
		return "", nil
	}
	var parent lifecycleOrderRecord
	if err := l.db.Preload("Children").First(&parent, parentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	if parent.ParentID != nil {
		return "", nil
	}
	if parent.Status == constants.OrderStatusCanceled || parent.Status == constants.OrderStatusRefunded || parent.Status == constants.OrderStatusCompleted {
		return parent.Status, nil
	}
	newStatus := calculateParentStatus(parent.Children, parent.Status)
	if newStatus == "" || newStatus == parent.Status {
		return parent.Status, nil
	}
	if err := l.db.Table(parent.TableName()).Where("id = ? AND deleted_at IS NULL", parent.ID).Updates(map[string]interface{}{
		"status": newStatus, "updated_at": now,
	}).Error; err != nil {
		return "", err
	}
	return newStatus, nil
}

// WithinCallbackTransaction locks the parent before the child so callbacks for
// different children cannot calculate and overwrite the parent state concurrently.
func (l *Lifecycle) WithinCallbackTransaction(procurementOrderID uint, apply func(procurementcontract.CallbackTransaction) error) error {
	return l.db.Transaction(func(tx *gorm.DB) error {
		var reference procurementdomain.Order
		if err := tx.Select("id", "local_order_id").Where("deleted_at IS NULL").First(&reference, procurementOrderID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return procurementcontract.ErrNotFound
			}
			return err
		}
		var order lifecycleOrderRecord
		if err := tx.First(&order, reference.LocalOrderID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return procurementcontract.ErrOrderNotFound
			}
			return err
		}
		parentStatus := ""
		if order.ParentID != nil {
			var parent lifecycleOrderRecord
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&parent, *order.ParentID).Error; err != nil {
				return fmt.Errorf("lock parent order: %w", err)
			}
			parentStatus = parent.Status
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, reference.LocalOrderID).Error; err != nil {
			return err
		}
		var procurement procurementdomain.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("deleted_at IS NULL").First(&procurement, procurementOrderID).Error; err != nil {
			return err
		}
		transactionLifecycle := *l
		transactionLifecycle.db = tx
		return apply(&callbackTransaction{
			lifecycle: &transactionLifecycle, procurement: &procurement, parentStatus: parentStatus,
			order: &procurementdomain.LocalOrder{
				ID: order.ID, ParentID: order.ParentID, UserID: order.UserID,
				GuestEmail: order.GuestEmail, Status: order.Status,
			},
		})
	})
}

type callbackTransaction struct {
	lifecycle    *Lifecycle
	procurement  *procurementdomain.Order
	order        *procurementdomain.LocalOrder
	parentStatus string
}

var _ procurementcontract.CallbackTransaction = (*callbackTransaction)(nil)

func (t *callbackTransaction) ProcurementOrder() *procurementdomain.Order { return t.procurement }
func (t *callbackTransaction) LocalOrder() *procurementdomain.LocalOrder  { return t.order }
func (t *callbackTransaction) ParentStatus() string                       { return t.parentStatus }

func (t *callbackTransaction) CreateUpstreamFulfillment(fulfillment *procurementcontract.Fulfillment, now time.Time) error {
	return t.lifecycle.CreateUpstreamFulfillment(t.order.ID, fulfillment, now)
}

func (t *callbackTransaction) UpdateProcurementStatus(status string, updates map[string]interface{}) error {
	if updates == nil {
		updates = map[string]interface{}{}
	}
	updates["status"] = status
	result := t.lifecycle.db.Model(&procurementdomain.Order{}).
		Where("id = ? AND deleted_at IS NULL", t.procurement.ID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return procurementcontract.ErrNotFound
	}
	t.procurement.Status = status
	return nil
}

func (t *callbackTransaction) UpdateLocalOrderStatus(status string, now time.Time) error {
	result := t.lifecycle.db.Table("orders").Where("id = ? AND deleted_at IS NULL", t.order.ID).
		Updates(map[string]interface{}{"status": status, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return procurementcontract.ErrOrderNotFound
	}
	t.order.Status = status
	return nil
}

func (t *callbackTransaction) SyncParentStatus(now time.Time) (string, error) {
	if t.order.ParentID == nil {
		return "", nil
	}
	return t.lifecycle.SyncParentStatus(*t.order.ParentID, now)
}

func (l *Lifecycle) EnqueueStatusEmail(orderID uint, status string) (bool, error) {
	if l.queue == nil || orderID == 0 {
		return true, nil
	}
	if l.settings != nil {
		smtpSetting, err := l.settings.GetSMTPSetting(l.defaultEmailConfig)
		if err != nil {
			return false, err
		}
		if !smtpSetting.Enabled || !smtpSetting.OrderNotificationEnabled {
			return true, nil
		}
	}
	receiverEmail, err := l.resolveReceiverEmail(orderID)
	if err == nil {
		receiverEmail = strings.TrimSpace(receiverEmail)
		if receiverEmail == "" || telegramidentity.IsPlaceholderEmail(receiverEmail) {
			return true, nil
		}
	}
	if err := l.queue.EnqueueOrderStatusEmail(queue.OrderStatusEmailPayload{
		OrderID: orderID, Status: strings.TrimSpace(status),
	}); err != nil {
		return false, err
	}
	return false, nil
}

func (l *Lifecycle) resolveReceiverEmail(orderID uint) (string, error) {
	var order lifecycleOrderRecord
	if err := l.db.Select("user_id", "guest_email").First(&order, orderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	if order.UserID == 0 {
		return strings.TrimSpace(order.GuestEmail), nil
	}
	var user lifecycleUserRecord
	if err := l.db.Select("email").First(&user, order.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(user.Email), nil
}

func calculateParentStatus(children []lifecycleOrderRecord, currentStatus string) string {
	if len(children) == 0 {
		return currentStatus
	}
	counts := make(map[string]int, 8)
	for _, child := range children {
		counts[strings.ToLower(strings.TrimSpace(child.Status))]++
	}
	size := len(children)
	if counts[constants.OrderStatusCanceled] == size {
		return constants.OrderStatusCanceled
	}
	if counts[constants.OrderStatusRefunded] == size {
		return constants.OrderStatusRefunded
	}
	if counts[constants.OrderStatusRefunded] > 0 || counts[constants.OrderStatusPartiallyRefunded] > 0 {
		return constants.OrderStatusPartiallyRefunded
	}
	if counts[constants.OrderStatusCompleted] == size {
		return constants.OrderStatusCompleted
	}
	delivered := counts[constants.OrderStatusDelivered] + counts[constants.OrderStatusCompleted]
	if delivered == size {
		return constants.OrderStatusDelivered
	}
	if delivered > 0 {
		return constants.OrderStatusPartiallyDelivered
	}
	if counts[constants.OrderStatusFulfilling] > 0 {
		return constants.OrderStatusFulfilling
	}
	if counts[constants.OrderStatusPaid] > 0 {
		return constants.OrderStatusPaid
	}
	if counts[constants.OrderStatusPendingPayment] > 0 {
		return constants.OrderStatusPendingPayment
	}
	return currentStatus
}
