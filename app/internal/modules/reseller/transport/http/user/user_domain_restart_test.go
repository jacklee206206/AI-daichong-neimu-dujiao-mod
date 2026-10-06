// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package userhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	app "github.com/dujiao-next/internal/modules/reseller/application"
	"github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/platform/http/response"
)

type restartStub struct {
	managementStub
	calls int
}

func (s *restartStub) InspectDomainConnect(context.Context, uint, string) (*app.DomainConnectInspection, error) {
	return nil, nil
}
func (s *restartStub) ConnectUserDomain(context.Context, uint, string, string, contract.DNSCredentials) (*app.DomainConnectResult, error) {
	return nil, nil
}
func (s *restartStub) UserDomainConnectStatus(uint, string) (*app.DomainConnectResult, error) {
	return nil, nil
}
func (s *restartStub) RestartUserDomainConnect(context.Context, uint, string) (*app.DomainConnectResult, error) {
	s.calls++
	return &app.DomainConnectResult{Phase: "dns_pending", Message: "queued"}, nil
}
func TestRestartRequiresExplicitConfirmation(t *testing.T) {
	for _, body := range []string{`{"domain":"example.com"}`, `{"domain":"example.com","confirm_restart":false}`, `{"domain":"example.com","confirm_restart":true}`} {
		stub := &restartStub{}
		h := NewUserHandler(stub, siteConfigStub{}, uploadStub{})
		c, w := newUserHandlerTestContext(http.MethodPost, "/domains/connect/restart", []byte(body), 2)
		h.RestartDomainConnect(c)
		var result struct {
			Code int `json:"status_code"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if body == `{"domain":"example.com","confirm_restart":true}` {
			if stub.calls != 1 {
				t.Fatal("confirmed restart not called")
			}
		} else if stub.calls != 0 || result.Code != response.CodeBadRequest {
			t.Fatalf("unconfirmed restart ran %s", w.Body.String())
		}
	}
}
