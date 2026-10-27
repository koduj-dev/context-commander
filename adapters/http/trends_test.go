package http_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The trends endpoint answers in counts only, defaults to twelve weeks, and
// sees the admin's own request: authenticating is what makes a principal
// count as active, so the request asking is already in the answer.
func TestInstallationTrends(t *testing.T) {
	f := newSecurityFixture(t)
	admin := f.newInstallationAdminToken("trends-admin@example.test")

	raw := f.mustDo(http.MethodGet, "/v1/installation/trends", admin, nil, http.StatusOK, "read trends")
	var body struct {
		Weeks []struct {
			WeekStart   string `json:"week_start"`
			ActiveUsers int64  `json:"active_users"`
			// Present only when there is a claim to take a median of.
			MedianSecondsToClaim *int64 `json:"median_seconds_to_claim"`
		} `json:"weeks"`
		HandoffFlow struct {
			Handoffs             int64  `json:"handoffs"`
			MedianSecondsToClaim *int64 `json:"median_seconds_to_claim"`
		} `json:"handoff_flow"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Weeks) != 12 {
		t.Fatalf("got %d weeks, want the default of 12", len(body.Weeks))
	}
	last := body.Weeks[len(body.Weeks)-1]
	if last.ActiveUsers < 1 {
		t.Errorf("current week %s reports %d active users; the admin making this request is one",
			last.WeekStart, last.ActiveUsers)
	}
	if last.MedianSecondsToClaim != nil || body.HandoffFlow.MedianSecondsToClaim != nil {
		t.Error("a median was reported with no handoffs to take it of")
	}

	raw = f.mustDo(http.MethodGet, "/v1/installation/trends?weeks=500", admin, nil, http.StatusOK, "read clamped trends")
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Weeks) != 52 {
		t.Errorf("weeks=500 returned %d weeks, want the clamp of 52", len(body.Weeks))
	}

	resp := f.do(http.MethodGet, "/v1/installation/trends?weeks=many", admin, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("weeks=many status = %d, want 400", resp.StatusCode)
	}

	ordinary := f.newUserWithToken("trends-ordinary@example.test")
	resp = f.do(http.MethodGet, "/v1/installation/trends", ordinary, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin status = %d, want 403", resp.StatusCode)
	}
}
