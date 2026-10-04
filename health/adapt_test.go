package health_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudresty/rig"
	"github.com/cloudresty/rig/health"
)

func TestAdaptMountsOnRigRouter(t *testing.T) {
	reg := health.New()
	reg.MarkWired()
	r := rig.New()
	r.GET("/health/live", health.Adapt(reg.LiveHandler()))
	r.GET("/health/ready", health.Adapt(reg.ReadyHandler()))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if rec.Code != 200 {
		t.Fatalf("live: %d %s", rec.Code, rec.Body)
	}
	// No checks registered and wired: ready is trivially OK.
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if rec.Code != 200 {
		t.Fatalf("ready: %d %s", rec.Code, rec.Body)
	}
}
