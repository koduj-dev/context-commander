package mcp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	ccmcp "github.com/koduj-dev/context-commander/adapters/mcp"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/embeddings"
	"github.com/koduj-dev/context-commander/internal/embeddings/embeddingstest"
)

type semanticSearchResult struct {
	Items []struct {
		ID      string  `json:"id"`
		Match   string  `json:"match"`
		Score   float64 `json:"score"`
		Body    string  `json:"body"`
		Snippet string  `json:"snippet"`
	} `json:"items"`
	Mode     string `json:"mode"`
	Degraded bool   `json:"degraded"`
}

// An agent searches in Czech for something a colleague wrote down in
// English, finds it by meaning, and asks for what is related to it.
func TestMemorySearchModesAndRelatedOverMCP(t *testing.T) {
	var ix *embeddings.Index
	f := newFixtureWith(t, func(d *ccmcp.Deps) {
		root, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
		if err != nil {
			t.Fatal(err)
		}
		ix = embeddings.NewIndex(&embeddingstest.Fake{}, embeddings.NewEncryptedStore(crypto.NewProvider(root), 0),
			d.Pool, d.Memories, embeddings.Options{MinSimilarity: 0.5, SweepInterval: -1})
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go ix.Run(ctx)
		d.Memories.EnableSemantic(ix)
	})
	ctx := context.Background()
	s, err := f.connect(ctx, f.newUserWithToken("sem-mcp@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	const project = "sem-mcp/work"
	callTool[projectResult](t, ctx, s, "project.create", map[string]any{"project": project, "name": "Work"})
	webhook := callTool[memoryToolResult](t, ctx, s, "memory.create", map[string]any{
		"project": project, "kind": "gotcha", "title": "Webhook callbacks are retried with backoff", "body": "Make handlers idempotent.",
	})
	near := callTool[memoryToolResult](t, ctx, s, "memory.create", map[string]any{
		"project": project, "title": "Webhook retries arrive out of order",
	})
	callTool[memoryToolResult](t, ctx, s, "memory.create", map[string]any{"project": project, "title": "Release notes template"})
	for deadline := time.Now().Add(10 * time.Second); ix.Status(ctx).Pending != 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("memories were not embedded within 10s")
		}
	}

	const czech = "zpětné volání opakování"
	text := callTool[semanticSearchResult](t, ctx, s, "memory.search", map[string]any{"project": project, "query": czech, "mode": "text"})
	if len(text.Items) != 0 || text.Mode != "text" {
		t.Fatalf("text search: %+v", text)
	}
	sem := callTool[semanticSearchResult](t, ctx, s, "memory.search", map[string]any{"project": project, "query": czech, "mode": "semantic"})
	if len(sem.Items) != 2 || sem.Items[0].Match != "semantic" || sem.Items[0].Body != "" || sem.Mode != "semantic" {
		t.Fatalf("semantic search: %+v", sem)
	}
	hybrid := callTool[semanticSearchResult](t, ctx, s, "memory.search", map[string]any{"query": "idempotent"})
	if hybrid.Mode != "hybrid" || len(hybrid.Items) != 1 || hybrid.Items[0].ID != webhook.ID || hybrid.Items[0].Match != "body" {
		t.Fatalf("default search across projects: %+v", hybrid)
	}

	rel := callTool[semanticSearchResult](t, ctx, s, "memory.related", map[string]any{"project": project, "memory_id": webhook.ID, "limit": 3})
	if len(rel.Items) != 1 || rel.Items[0].ID != near.ID {
		t.Fatalf("related: %+v", rel)
	}

	res, err := s.CallTool(ctx, &sdkmcp.CallToolParams{Name: "memory.search", Arguments: map[string]any{"project": project, "query": "x", "mode": "fuzzy"}})
	if err != nil || !res.IsError {
		t.Fatalf("an unknown mode was not a tool error: %+v %v", res, err)
	}
}

// Without a provider, memory.related and semantic mode are tool errors that
// say semantic search is not configured; text search works as before.
func TestSemanticToolsWithoutAProvider(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s, err := f.connect(ctx, f.newUserWithToken("nosem-mcp@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	const project = "nosem-mcp/work"
	callTool[projectResult](t, ctx, s, "project.create", map[string]any{"project": project, "name": "Work"})
	m := callTool[memoryToolResult](t, ctx, s, "memory.create", map[string]any{"project": project, "title": "Slow database"})
	text := callTool[semanticSearchResult](t, ctx, s, "memory.search", map[string]any{"project": project, "query": "database"})
	if text.Mode != "text" || len(text.Items) != 1 {
		t.Fatalf("default search without a provider: %+v", text)
	}
	for name, args := range map[string]map[string]any{
		"memory.search":  {"project": project, "query": "database", "mode": "semantic"},
		"memory.related": {"project": project, "memory_id": m.ID},
	} {
		res, err := s.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || !res.IsError {
			t.Fatalf("%s without a provider: %+v %v", name, res, err)
		}
		msg := ""
		for _, c := range res.Content {
			if tc, ok := c.(*sdkmcp.TextContent); ok {
				msg += tc.Text
			}
		}
		if !strings.Contains(msg, "semantic search is not configured") {
			t.Fatalf("%s error does not say why: %q", name, msg)
		}
	}
}
