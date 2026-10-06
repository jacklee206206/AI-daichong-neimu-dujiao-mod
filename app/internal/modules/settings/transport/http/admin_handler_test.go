// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package settingshttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/dujiao-next/internal/constants"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

type adminSettingsStub struct {
	updateCalls int
	result      settingsapp.UpdateResult
}

func (s *adminSettingsStub) GetByKey(string) (jsonmap.JSON, error) {
	return nil, nil
}

func (s *adminSettingsStub) UpdateWithEffects(string, map[string]interface{}) (settingsapp.UpdateResult, error) {
	s.updateCalls++
	return s.result, nil
}

type automaticAPIApprovalStub struct {
	calls int
	err   error
}

func (s *automaticAPIApprovalStub) AutoApprovePending() (int, error) { s.calls++; return 1, s.err }

func TestAPIApprovalSettingSaveProcessesPendingApplications(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, failed := range []bool{false, true} {
		stub := &adminSettingsStub{result: settingsapp.UpdateResult{Value: jsonmap.JSON{"auto_approve_applications": true}, Effects: []settingsapp.Effect{settingsapp.EffectAutoApproveApiCredentials}}}
		approvals := &automaticAPIApprovalStub{}
		if failed {
			approvals.err = errors.New("temporary database failure")
		}
		handler := NewAdminHandler(stub)
		handler.SetApiCredentialApprovals(approvals)
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", strings.NewReader(`{"key":"api_credential_config","value":{"auto_approve_applications":true}}`))
		context.Request.Header.Set("Content-Type", "application/json")
		handler.Update(context)
		if stub.updateCalls != 1 || approvals.calls != 1 {
			t.Fatal("pending applications were not processed after settings save")
		}
		if failed && !strings.Contains(recorder.Body.String(), "待审申请补处理失败") {
			t.Fatalf("partial failure was hidden: %s", recorder.Body.String())
		}
		if !failed && !strings.Contains(recorder.Body.String(), `"auto_approve_applications":true`) {
			t.Fatalf("saved setting missing: %s", recorder.Body.String())
		}
	}
}

func (s *adminSettingsStub) InvalidateCallbackRoutesCache() {}

func TestAdminHandlerRejectsGoogleAuthOnGenericUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &adminSettingsStub{}
	handler := NewAdminHandler(stub)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		strings.NewReader(`{"key":"`+constants.SettingKeyGoogleAuthConfig+`","value":{"enabled":true,"client_id":""}}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	handler.Update(context)

	if stub.updateCalls != 0 {
		t.Fatalf("generic update persisted protected Google auth config")
	}
	var body struct {
		StatusCode int    `json:"status_code"`
		Msg        string `json:"msg"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	if recorder.Code != http.StatusOK || body.StatusCode != 400 {
		t.Fatalf("http=%d status_code=%d body=%s", recorder.Code, body.StatusCode, recorder.Body.String())
	}
	if !strings.Contains(body.Msg, "/admin/settings/google-auth") {
		t.Fatalf("unexpected rejection message: %q", body.Msg)
	}
}
