// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package queueadapter

import (
	"testing"
	"time"

	"github.com/dujiao-next/internal/queue"
	"github.com/hibiken/asynq"
)

type recordingClient struct{ options []asynq.Option }

func (c *recordingClient) EnqueueProcurementSubmit(_ queue.ProcurementSubmitPayload, opts ...asynq.Option) error {
	c.options = opts
	return nil
}
func (c *recordingClient) EnqueueProcurementPollStatus(_ queue.ProcurementPollStatusPayload, _ time.Duration) error {
	return nil
}

func TestSubmitSchedulesConfiguredRetryDelay(t *testing.T) {
	for _, delay := range []time.Duration{30 * time.Second, 2 * time.Minute, -time.Second} {
		client := &recordingClient{}
		if err := New(client).EnqueueSubmit(42, delay); err != nil {
			t.Fatal(err)
		}
		want := delay
		if want < 0 {
			want = 0
		}
		if len(client.options) != 1 || client.options[0].Type() != asynq.ProcessInOpt || client.options[0].Value() != want {
			t.Fatalf("retry delay %v did not reach scheduler: %v", delay, client.options)
		}
	}
}
