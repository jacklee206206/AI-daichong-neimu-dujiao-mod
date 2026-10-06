// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package integrationtest

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	downstreamcontract "github.com/dujiao-next/internal/modules/downstreamcallback/contract"
	"github.com/dujiao-next/internal/modules/downstreamcallback/infrastructure/callbackclient"
	procurementcontract "github.com/dujiao-next/internal/modules/procurement/contract"
	procurementdomain "github.com/dujiao-next/internal/modules/procurement/domain"
	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
	upstreamhttp "github.com/dujiao-next/internal/modules/upstreamapi/transport/http"
	"github.com/gin-gonic/gin"
)

type callbackWireProcurements struct {
	orderNo string
	result  chan *procurementcontract.Fulfillment
}

func (s *callbackWireProcurements) GetByLocalOrderNo(orderNo string) (*procurementdomain.Order, error) {
	s.orderNo = orderNo
	return &procurementdomain.Order{ID: 7, ConnectionID: 1, LocalOrderNo: orderNo, UpstreamOrderID: 999}, nil
}

func (s *callbackWireProcurements) HandleUpstreamCallback(id uint, status string, fulfillment *procurementcontract.Fulfillment) error {
	if id == 7 && status == "delivered" && s.orderNo == "LOCAL-FIXTURE-1" {
		s.result <- fulfillment
	}
	return nil
}

func TestCallbackClientMatchesReceiverWireContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	procurements := &callbackWireProcurements{result: make(chan *procurementcontract.Fulfillment, 1)}
	handler := &upstreamhttp.Handler{Dependencies: upstreamhttp.Dependencies{
		Connections:       callbackWireConnections{conn: &siteconnectiondomain.Connection{ID: 1, ApiKey: "fixture-key", ApiSecret: "fixture-secret", Status: "active"}},
		ConnectionSecrets: callbackWireSecrets{}, Procurements: procurements,
	}}
	router := gin.New()
	router.POST("/api/v1/upstream/callback", handler.HandleCallback)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	err := callbackclient.NewWithHTTPClient(server.Client()).Send(context.Background(), downstreamcontract.DeliveryRequest{
		URL: server.URL + "/api/v1/upstream/callback", APIKey: "fixture-key", APISecret: "fixture-secret",
		Payload: downstreamcontract.CallbackPayload{
			Event: "order.fulfilled", OrderID: 999, OrderNo: "UP-FIXTURE-999", DownstreamOrderNo: "LOCAL-FIXTURE-1", Status: "completed", Timestamp: time.Now().Unix(),
			Fulfillment: &downstreamcontract.Fulfillment{Type: "auto", Status: "delivered", Payload: "FIXTURE-NOT-A-REAL-CARD"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case fulfillment := <-procurements.result:
		if fulfillment == nil || fulfillment.Payload != "FIXTURE-NOT-A-REAL-CARD" || fulfillment.Type != "auto" {
			t.Fatal("callback did not preserve card fulfillment")
		}
	default:
		t.Fatal("signed callback did not reach matching local procurement")
	}
}

// These stubs represent the connection-owned credential boundary only.
type callbackWireConnections struct {
	conn *siteconnectiondomain.Connection
}

func (s callbackWireConnections) GetByApiKey(string) (*siteconnectiondomain.Connection, error) {
	return s.conn, nil
}

type callbackWireSecrets struct{}

func (callbackWireSecrets) DecryptSecret(value string) (string, error) { return value, nil }
