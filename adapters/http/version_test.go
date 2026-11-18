package http_test

import (
	"context"
	"net/http"
	"testing"

	cchttp "github.com/koduj-dev/context-commander/adapters/http"
	"github.com/koduj-dev/context-commander/internal/ratelimit"
	"github.com/koduj-dev/context-commander/internal/updatecheck"
)

type latestRelease string

func (l latestRelease) Latest(context.Context, bool) (string, string, error) {
	return string(l), "https://example.test/releases/" + string(l), nil
}

// The version endpoint says what runs and, with the check on, what is newer.
func TestInstallationVersion(t *testing.T) {
	updates := updatecheck.New("v0.1.0-beta.4", latestRelease("v0.1.0-beta.5"))
	updates.Check(context.Background())
	f := newFixtureWithDeps(t, nil, ratelimit.Limits{}, func(d *cchttp.Deps) { d.Updates = updates })
	token := f.newUserWithToken("version@example.test")

	var v struct {
		Current         string `json:"current"`
		UpdateCheck     bool   `json:"update_check"`
		Latest          string `json:"latest"`
		UpdateAvailable bool   `json:"update_available"`
		URL             string `json:"url"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/installation/version", token, nil, http.StatusOK, "version"), &v)
	if v.Current != "v0.1.0-beta.4" || !v.UpdateCheck || v.Latest != "v0.1.0-beta.5" || !v.UpdateAvailable || v.URL == "" {
		t.Errorf("version %+v", v)
	}
}
