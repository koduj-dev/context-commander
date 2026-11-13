package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	cchttp "github.com/koduj-dev/context-commander/adapters/http"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/embeddings"
	"github.com/koduj-dev/context-commander/internal/embeddings/embeddingstest"
	"github.com/koduj-dev/context-commander/internal/ratelimit"
)

type semanticHits struct {
	Items []struct {
		ID      string  `json:"id"`
		Match   string  `json:"match"`
		Snippet string  `json:"snippet"`
		Score   float64 `json:"score"`
		Project string  `json:"project"`
	} `json:"items"`
	Mode     string `json:"mode"`
	Degraded bool   `json:"degraded"`
}

// newSemanticFixture is the HTTP fixture with semantic search on, through a
// fake provider and the encrypted store, and the index's worker running.
func newSemanticFixture(t *testing.T) (*fixture, *embeddings.Index) {
	t.Helper()
	var ix *embeddings.Index
	f := newFixtureWithDeps(t, nil, ratelimit.Limits{}, func(d *cchttp.Deps) {
		root, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
		if err != nil {
			t.Fatal(err)
		}
		store := embeddings.NewEncryptedStore(crypto.NewProvider(root), 0)
		ix = embeddings.NewIndex(&embeddingstest.Fake{}, store, d.Pool, d.Memories, embeddings.Options{MinSimilarity: 0.5, SweepInterval: -1})
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go ix.Run(ctx)
		d.Memories.EnableSemantic(ix)
		d.Embeddings = ix
	})
	return f, ix
}

func waitForIndex(t *testing.T, ix *embeddings.Index) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ix.Status(context.Background()).Pending == 0 {
			return
		}
	}
	t.Fatal("memories were not embedded within 10s")
}

func TestSemanticSearchOverHTTP(t *testing.T) {
	f, ix := newSemanticFixture(t)
	owner := f.newUserWithToken("sem-owner@example.test")
	reader := f.newUserWithToken("sem-reader@example.test")
	f.createProject(owner, "sem-owner/work", "Work")
	memberBody, _ := json.Marshal(map[string]string{"principal_id": f.principalIDOf(reader), "role": "READER"})
	f.mustDo(http.MethodPost, "/v1/projects/sem-owner/work/members", owner, memberBody, http.StatusNoContent, "add reader")
	base := "/v1/projects/sem-owner/work/memories"

	var slow, related memoryBody
	mustJSON(t, f.mustDo(http.MethodPost, base+"/", owner, []byte(`{"title":"The database is slow","body":"Sequential scans everywhere.","branch":"perf"}`),
		http.StatusCreated, "create"), &slow)
	mustJSON(t, f.mustDo(http.MethodPost, base+"/", owner, []byte(`{"title":"Postgres query latency after deploy"}`),
		http.StatusCreated, "create related"), &related)
	f.mustDo(http.MethodPost, base+"/", owner, []byte(`{"title":"Login copy for the sign-in page"}`), http.StatusCreated, "create unrelated")
	waitForIndex(t, ix)

	var facets struct {
		SearchModes       []string `json:"search_modes"`
		DefaultSearchMode string   `json:"default_search_mode"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/facets", reader, nil, http.StatusOK, "facets"), &facets)
	if fmt.Sprint(facets.SearchModes) != "[text semantic hybrid]" || facets.DefaultSearchMode != "hybrid" {
		t.Fatalf("facets with a provider: %+v", facets)
	}

	var hits semanticHits
	mustJSON(t, f.mustDo(http.MethodGet, base+"/search?q=pomal%C3%A1+datab%C3%A1ze&mode=text", reader, nil, http.StatusOK, "text"), &hits)
	if len(hits.Items) != 0 || hits.Mode != "text" {
		t.Fatalf("text search for Czech words: %+v", hits)
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/search?q=pomal%C3%A1+datab%C3%A1ze&mode=semantic", reader, nil, http.StatusOK, "semantic"), &hits)
	if len(hits.Items) == 0 || hits.Items[0].ID != slow.ID || hits.Items[0].Match != "semantic" || hits.Items[0].Score <= 0 || hits.Mode != "semantic" {
		t.Fatalf("semantic search: %+v", hits)
	}
	// Metadata filters apply before ranking.
	mustJSON(t, f.mustDo(http.MethodGet, base+"/search?q=pomal%C3%A1+datab%C3%A1ze&mode=semantic&branch=nope", reader, nil, http.StatusOK, "filtered"), &hits)
	if len(hits.Items) != 0 {
		t.Fatalf("a branch filter was ignored: %+v", hits)
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/search?q=database", reader, nil, http.StatusOK, "default"), &hits)
	if hits.Mode != "hybrid" || len(hits.Items) == 0 || hits.Items[0].ID != slow.ID || hits.Items[0].Match != "title" {
		t.Fatalf("hybrid search: %+v", hits)
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/me/memories/search?q=datab%C3%A1ze&mode=semantic", reader, nil, http.StatusOK, "mine"), &hits)
	if len(hits.Items) == 0 || hits.Items[0].Project != "sem-owner/work" {
		t.Fatalf("semantic search across projects: %+v", hits)
	}
	f.mustDo(http.MethodGet, base+"/search?q=x&mode=fuzzy", reader, nil, http.StatusBadRequest, "unknown mode")

	mustJSON(t, f.mustDo(http.MethodGet, base+"/"+slow.ID+"/related?limit=3", reader, nil, http.StatusOK, "related"), &hits)
	if len(hits.Items) != 1 || hits.Items[0].ID != related.ID {
		t.Fatalf("related: %+v", hits)
	}
	f.mustDo(http.MethodGet, base+"/"+slow.ID+"/related?limit=0", reader, nil, http.StatusBadRequest, "bad limit")
	outsider := f.newUserWithToken("sem-outsider@example.test")
	resp := f.do(http.MethodGet, base+"/"+slow.ID+"/related", outsider, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("outsider related: %d", resp.StatusCode)
	}

	// The status is for administrators, and says nothing about content.
	f.mustDo(http.MethodGet, "/v1/installation/embeddings", reader, nil, http.StatusForbidden, "status as a member")
	admin := f.newInstallationAdminToken("sem-admin@example.test")
	var st struct {
		Configured bool   `json:"configured"`
		Provider   string `json:"provider"`
		Model      string `json:"model"`
		Store      string `json:"store"`
		Reachable  *bool  `json:"reachable"`
		Pending    int64  `json:"pending"`
		Queue      int    `json:"queue"`
	}
	raw := f.mustDo(http.MethodGet, "/v1/installation/embeddings", admin, nil, http.StatusOK, "status")
	mustJSON(t, raw, &st)
	if !st.Configured || st.Provider != "fake" || st.Model != "fake-model" || st.Store != "encrypted" || st.Reachable == nil || !*st.Reachable || st.Pending != 0 {
		t.Fatalf("status: %s", raw)
	}
}

// Without a provider nothing changes: text is the only mode, and asking for
// semantic search or related memories is a clear 400.
func TestSemanticSearchIsOffWithoutAProvider(t *testing.T) {
	f := newFixture(t)
	owner := f.newUserWithToken("nosem@example.test")
	f.createProject(owner, "nosem/work", "Work")
	base := "/v1/projects/nosem/work/memories"
	var m memoryBody
	mustJSON(t, f.mustDo(http.MethodPost, base+"/", owner, []byte(`{"title":"Slow database"}`), http.StatusCreated, "create"), &m)

	var facets struct {
		SearchModes       []string `json:"search_modes"`
		DefaultSearchMode string   `json:"default_search_mode"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, base+"/facets", owner, nil, http.StatusOK, "facets"), &facets)
	if fmt.Sprint(facets.SearchModes) != "[text]" || facets.DefaultSearchMode != "text" {
		t.Fatalf("facets without a provider: %+v", facets)
	}
	var hits semanticHits
	mustJSON(t, f.mustDo(http.MethodGet, base+"/search?q=database", owner, nil, http.StatusOK, "default"), &hits)
	if hits.Mode != "text" || len(hits.Items) != 1 {
		t.Fatalf("default search: %+v", hits)
	}
	body := f.mustDo(http.MethodGet, base+"/search?q=database&mode=semantic", owner, nil, http.StatusBadRequest, "semantic")
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	mustJSON(t, body, &e)
	if !strings.Contains(e.Error.Message, "semantic search is not configured") {
		t.Fatalf("the 400 does not say why: %s", body)
	}
	f.mustDo(http.MethodGet, base+"/search?q=database&mode=hybrid", owner, nil, http.StatusBadRequest, "hybrid")
	f.mustDo(http.MethodGet, base+"/"+m.ID+"/related", owner, nil, http.StatusBadRequest, "related")

	admin := f.newInstallationAdminToken("nosem-admin@example.test")
	var st struct {
		Configured bool `json:"configured"`
	}
	mustJSON(t, f.mustDo(http.MethodGet, "/v1/installation/embeddings", admin, nil, http.StatusOK, "status"), &st)
	if st.Configured {
		t.Fatal("status says configured without a provider")
	}
}
