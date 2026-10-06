// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	procurementcontract "github.com/dujiao-next/internal/modules/procurement/contract"
	procurementdomain "github.com/dujiao-next/internal/modules/procurement/domain"
)

// SubmitToUpstream Worker 调用：向上游站点提交采购单
func (s *Service) SubmitToUpstream(procurementOrderID uint) error {
	procOrder, err := s.procRepo.GetByID(procurementOrderID)
	if err != nil {
		return fmt.Errorf("load procurement order: %w", err)
	}
	if procOrder == nil {
		return procurementcontract.ErrNotFound
	}

	// 校验状态
	if procOrder.Status != "pending" && procOrder.Status != "failed" {
		return nil // duplicate workers must not resubmit an in-flight or final order
	}
	guard, ok := s.procRepo.(procurementcontract.SubmissionRepository)
	if !ok {
		return fmt.Errorf("procurement repository lacks atomic submission guards")
	}

	// 获取连接和适配器
	connection, err := s.connections.Open(procOrder.ConnectionID)
	if err != nil {
		s.markProcurementError(procOrder, fmt.Sprintf("load connection failed: %v", err))
		return fmt.Errorf("load connection: %w", err)
	}
	if connection == nil {
		s.rejectProcurement(procOrder, fmt.Sprintf("connection %d not found", procOrder.ConnectionID))
		return nil // 永久性错误，不重试
	}

	// 加载本地订单获取 SKU 信息
	localOrder, err := s.orderRepo.GetByID(procOrder.LocalOrderID)
	if err != nil {
		s.markProcurementError(procOrder, fmt.Sprintf("load local order failed: %v", err))
		return fmt.Errorf("load local order: %w", err)
	}
	if localOrder == nil {
		s.rejectProcurement(procOrder, fmt.Sprintf("local order %d not found", procOrder.LocalOrderID))
		return nil // 永久性错误，不重试
	}
	if len(localOrder.Items) == 0 {
		s.rejectProcurement(procOrder, fmt.Sprintf("local order %d has no items", localOrder.ID))
		return nil // 永久性错误，不重试
	}
	if (localOrder.Status != constants.OrderStatusPaid && localOrder.Status != constants.OrderStatusFulfilling) || !localOrder.RefundedAmount.Decimal.IsZero() {
		return procurementcontract.ErrStatusInvalid
	}
	item := localOrder.Items[0]

	// 查找 SKU 映射
	upstreamSKUID, found, err := s.skuMapRepo.FindUpstreamSKUID(item.SKUID)
	if err != nil {
		s.markProcurementError(procOrder, fmt.Sprintf("lookup sku mapping failed: %v", err))
		return fmt.Errorf("lookup sku mapping: %w", err)
	}
	if !found {
		s.rejectProcurement(procOrder, fmt.Sprintf("no sku mapping for local sku %d", item.SKUID))
		return nil // 永久性错误，不重试
	}

	// 构建上游请求
	req := procurementcontract.CreateOrderRequest{
		SKUID:             upstreamSKUID,
		Quantity:          item.Quantity,
		DownstreamOrderNo: localOrder.OrderNo,
		TraceID:           procOrder.TraceID,
		CallbackURL:       connection.CallbackURL(),
	}

	// 传递人工表单数据（如有）
	if len(item.ManualFormSubmissionJSON) > 0 {
		req.ManualFormData = item.ManualFormSubmissionJSON
	}
	// Persist the claim before making the chargeable request. If the worker
	// crashes afterwards this remains submitting, never a fresh purchase job.
	claimed, err := guard.ClaimSubmission(procOrder.ID, time.Now())
	if err != nil {
		return fmt.Errorf("claim procurement submission: %w", err)
	}
	if !claimed {
		return nil
	}
	procOrder.Status = procurementdomain.StatusSubmitting

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := connection.CreateOrder(ctx, req)
	if err != nil {
		return s.markSubmissionUnknown(procOrder, fmt.Sprintf("upstream request outcome unknown: %v", err))
	}
	if resp == nil {
		return s.markSubmissionUnknown(procOrder, "upstream returned an empty purchase response")
	}

	if !resp.OK {
		retryable := isRetryableErrorCode(resp.ErrorCode)
		errMsg := resp.ErrorMessage
		if errMsg == "" {
			errMsg = resp.ErrorCode
		}
		if resp.OrderID > 0 || strings.TrimSpace(resp.OrderNo) != "" {
			return s.markSubmissionUnknown(procOrder, "upstream returned an order identity with a failed purchase response: "+errMsg, resp)
		}
		if !retryable && !isDefiniteSubmitRejection(resp.ErrorCode) {
			return s.markSubmissionUnknown(procOrder, "upstream purchase outcome requires reconciliation: "+errMsg)
		}
		return s.handleSubmitFailure(procOrder, connection, errMsg, retryable)
	}
	if resp.OrderID == 0 || !isValidSubmitSuccessStatus(resp.Status) {
		return s.markSubmissionUnknown(procOrder, "upstream success response has no valid paid order identity/status", resp)
	}

	// Persist identity and acceptance together; never overwrite a faster callback.
	now := time.Now()
	accepted, err := guard.RecordSubmissionAccepted(procOrder.ID, resp, now)
	if err != nil {
		return s.markSubmissionUnknown(procOrder, fmt.Sprintf("purchase response could not be recorded; reconcile upstream order %d: %v", resp.OrderID, err))
	}
	if !accepted {
		return nil
	}

	logger.Infow("procurement_order_accepted",
		"procurement_order_id", procOrder.ID,
		"upstream_order_id", resp.OrderID,
		"upstream_order_no", resp.OrderNo,
	)

	// 入队轮询任务（30s 延迟，作为回调的 fallback）
	if s.queue != nil {
		_ = s.queue.EnqueuePoll(procOrder.ID, 30*time.Second)
	}

	return nil
}

// markProcurementError 记录错误信息但不改变状态（用于瞬态错误，asynq 可重试）
func (s *Service) markProcurementError(procOrder *procurementdomain.Order, errMsg string) {
	now := time.Now()
	guard, ok := s.procRepo.(procurementcontract.SubmissionRepository)
	if !ok {
		return
	}
	_, _ = guard.CompareAndSwapStatus(procOrder.ID, []string{procOrder.Status}, procOrder.Status, map[string]interface{}{
		"error_message": errMsg,
		"updated_at":    now,
	})
	logger.Warnw("procurement_prepare_error",
		"procurement_order_id", procOrder.ID,
		"error", errMsg,
	)
}

// rejectProcurement 将采购单标记为 rejected（用于永久性配置错误，不值得重试）
// 同时回退本地订单状态并通知管理员
func (s *Service) rejectProcurement(procOrder *procurementdomain.Order, errMsg string) {
	now := time.Now()
	guard, ok := s.procRepo.(procurementcontract.SubmissionRepository)
	if !ok {
		return
	}
	changed, err := guard.CompareAndSwapStatus(procOrder.ID, []string{procOrder.Status}, "rejected", map[string]interface{}{
		"error_message": errMsg,
		"updated_at":    now,
	})
	if err != nil || !changed {
		return
	}
	logger.Warnw("procurement_rejected_config_error",
		"procurement_order_id", procOrder.ID,
		"error", errMsg,
	)
	s.rollbackLocalOrderOnProcurementFailure(procOrder, errMsg)
}

// rollbackLocalOrderOnProcurementFailure 采购单终态失败时回退本地订单状态并通知管理员
func (s *Service) rollbackLocalOrderOnProcurementFailure(procOrder *procurementdomain.Order, errMsg string) {
	localOrder, err := s.orderRepo.GetByID(procOrder.LocalOrderID)
	if err != nil || localOrder == nil {
		return
	}
	if localOrder.Status == constants.OrderStatusFulfilling {
		now := time.Now()
		guard, ok := s.procRepo.(procurementcontract.SubmissionRepository)
		if !ok {
			return
		}
		changed, err := guard.UpdateLocalStatusIf(localOrder.ID, constants.OrderStatusFulfilling, constants.OrderStatusPaid, now)
		if err != nil || !changed {
			return
		}
		// 如果是子订单，同步父订单状态
		if localOrder.ParentID != nil && s.orderLifecycle != nil {
			_, _ = s.orderLifecycle.SyncParentStatus(*localOrder.ParentID, now)
		}
		logger.Infow("procurement_failure_order_rolled_back",
			"procurement_order_id", procOrder.ID,
			"local_order_id", localOrder.ID,
			"from_status", constants.OrderStatusFulfilling,
			"to_status", constants.OrderStatusPaid,
		)
	}
	s.notifyProcurementFailure(procOrder, errMsg)
}

// notifyProcurementFailure 发送采购失败异常告警
func (s *Service) notifyProcurementFailure(procOrder *procurementdomain.Order, errMsg string) {
	if s.notifications == nil {
		return
	}
	_ = s.notifications.NotifyFailure(procOrder, errMsg)
}

// handleSubmitFailure 处理提交失败
func (s *Service) handleSubmitFailure(procOrder *procurementdomain.Order, connection procurementcontract.UpstreamConnection, errMsg string, retryable bool) error {
	now := time.Now()
	guard, ok := s.procRepo.(procurementcontract.SubmissionRepository)
	if !ok {
		return fmt.Errorf("procurement repository lacks atomic submission guards")
	}

	if retryable && procOrder.RetryCount < connection.RetryMax() {
		intervals := parseRetryIntervals(connection.RetryIntervals())
		idx := procOrder.RetryCount
		if idx >= len(intervals) {
			idx = len(intervals) - 1
		}
		delay := intervals[idx]
		nextRetry := now.Add(delay)

		updates := map[string]interface{}{
			"retry_count":   procOrder.RetryCount + 1,
			"next_retry_at": &nextRetry,
			"error_message": errMsg,
			"updated_at":    now,
		}
		changed, err := guard.CompareAndSwapStatus(procOrder.ID, []string{procurementdomain.StatusSubmitting}, "failed", updates)
		if err != nil {
			return fmt.Errorf("update procurement status (failed): %w", err)
		}
		if !changed {
			return nil
		}

		logger.Warnw("procurement_submit_failed_retryable",
			"procurement_order_id", procOrder.ID,
			"retry_count", procOrder.RetryCount+1,
			"next_retry_at", nextRetry,
			"error", errMsg,
		)

		// 入队重试
		if s.queue != nil {
			return s.queue.EnqueueSubmit(procOrder.ID, delay)
		}

		return nil
	}

	// 不可重试或已达上限：拒绝
	updates := map[string]interface{}{
		"error_message": errMsg,
		"updated_at":    now,
	}
	changed, err := guard.CompareAndSwapStatus(procOrder.ID, []string{procurementdomain.StatusSubmitting}, "rejected", updates)
	if err != nil {
		return fmt.Errorf("update procurement status (rejected): %w", err)
	}
	if !changed {
		return nil
	}

	logger.Warnw("procurement_submit_rejected",
		"procurement_order_id", procOrder.ID,
		"error", errMsg,
	)

	// 回退本地订单状态并通知管理员
	s.rollbackLocalOrderOnProcurementFailure(procOrder, errMsg)

	return fmt.Errorf("procurement rejected: %s", errMsg)
}

// isRetryableErrorCode 判断上游错误码是否可重试
func isRetryableErrorCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "rate_limited", "too_many_requests":
		return true
	default:
		return false
	}
}

func isDefiniteSubmitRejection(code string) bool {
	nonRetryable := map[string]bool{
		"insufficient_balance": true,
		"payment_failed":       true,
		"product_unavailable":  true,
		"sku_unavailable":      true,
		"invalid_request":      true,
		"unauthorized":         true,
		"forbidden":            true,
		"product_out_of_stock": true,
		"bad_request":          true,
		"invalid_callback_url": true,
	}
	return nonRetryable[strings.ToLower(strings.TrimSpace(code))]
}

func isValidSubmitSuccessStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "accepted", constants.OrderStatusPaid, constants.OrderStatusFulfilling, constants.OrderStatusDelivered, constants.OrderStatusCompleted:
		return true
	default:
		return false
	}
}

func (s *Service) markSubmissionUnknown(procOrder *procurementdomain.Order, message string, responses ...*procurementcontract.CreateOrderResult) error {
	guard, ok := s.procRepo.(procurementcontract.SubmissionRepository)
	if !ok {
		return fmt.Errorf("procurement repository lacks atomic submission guards")
	}
	updates := map[string]interface{}{
		"error_message": message + "; 禁止重新采购，请先核对上游订单及余额", "next_retry_at": nil, "updated_at": time.Now(),
	}
	if len(responses) > 0 && responses[0] != nil {
		if responses[0].OrderID > 0 {
			updates["upstream_order_id"] = responses[0].OrderID
		}
		if strings.TrimSpace(responses[0].OrderNo) != "" {
			updates["upstream_order_no"] = responses[0].OrderNo
		}
	}
	changed, err := guard.CompareAndSwapStatus(procOrder.ID, []string{procurementdomain.StatusSubmitting}, procurementdomain.StatusUnknown, updates)
	if err != nil {
		return fmt.Errorf("record unknown procurement outcome: %w", err)
	}
	if changed {
		s.notifyProcurementFailure(procOrder, message)
		logger.Warnw("procurement_submit_outcome_unknown", "procurement_order_id", procOrder.ID, "error", message)
	}
	return nil
}

// parseRetryIntervals 解析重试间隔配置（JSON 数组格式如 "[30,60,300]"）
func parseRetryIntervals(raw string) []time.Duration {
	raw = strings.TrimSpace(raw)
	// 移除方括号
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")

	if raw == "" {
		return []time.Duration{30 * time.Second, 60 * time.Second, 300 * time.Second}
	}

	parts := strings.Split(raw, ",")
	intervals := make([]time.Duration, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		seconds, err := strconv.Atoi(part)
		if err != nil || seconds <= 0 {
			continue
		}
		intervals = append(intervals, time.Duration(seconds)*time.Second)
	}

	if len(intervals) == 0 {
		return []time.Duration{30 * time.Second, 60 * time.Second, 300 * time.Second}
	}
	return intervals
}
