package bridge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iAghaTraker/InfraPilot/internal/identity"
)

func TestIdentityEndpointReportsExistingDevice(t *testing.T) {
	id, _, err := identity.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	bridgeHandler(id).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/identity", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), id.DeviceID) {
		t.Fatalf("identity response status=%d body=%s", w.Code, w.Body.String())
	}
}
