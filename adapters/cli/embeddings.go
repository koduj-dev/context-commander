package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/embeddings"
	"github.com/koduj-dev/context-commander/internal/memories"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// Semantic search configuration (docs/adr/0028, docs/semantic-search.md).
// Every flag has an environment variable, so a .env file next to the
// compose file configures it like everything else.
const (
	embeddingsProviderEnv      = "CC_EMBEDDINGS_PROVIDER"
	embeddingsURLEnv           = "CC_EMBEDDINGS_URL"
	embeddingsModelEnv         = "CC_EMBEDDINGS_MODEL"
	embeddingsAPIKeyFileEnv    = "CC_EMBEDDINGS_API_KEY_FILE" //nolint:gosec // G101: an env var name
	embeddingsDimensionsEnv    = "CC_EMBEDDINGS_DIMENSIONS"
	embeddingsTimeoutEnv       = "CC_EMBEDDINGS_TIMEOUT"
	embeddingsStoreEnv         = "CC_EMBEDDINGS_STORE"
	embeddingsCacheMBEnv       = "CC_EMBEDDINGS_CACHE_MB"
	embeddingsMinSimilarityEnv = "CC_EMBEDDINGS_MIN_SIMILARITY"
)

func embeddingsFlags(cmd *cobra.Command) {
	cmd.Flags().String("embeddings-provider", "",
		fmt.Sprintf(`turn on semantic search of memories with this embedding provider: "ollama" (local, recommended) or "openai" (any OpenAI-compatible API — memory text leaves this server); empty keeps text search only (defaults to $%s)`, embeddingsProviderEnv))
	cmd.Flags().String("embeddings-url", "",
		fmt.Sprintf("provider base URL (defaults to $%s, else %s for ollama, %s for openai)", embeddingsURLEnv, embeddings.DefaultOllamaURL, embeddings.DefaultOpenAIURL))
	cmd.Flags().String("embeddings-model", "",
		fmt.Sprintf("embedding model; changing it re-embeds every memory (defaults to $%s, else %s for ollama, %s for openai)", embeddingsModelEnv, embeddings.DefaultOllamaModel, embeddings.DefaultOpenAIModel))
	// No flag for the key itself, for the reason there is no
	// --ldap-bind-password: an argument is visible in `ps`.
	cmd.Flags().String("embeddings-api-key-file", "",
		fmt.Sprintf("file holding the API key the openai provider sends as a bearer token (defaults to $%s)", embeddingsAPIKeyFileEnv))
	cmd.Flags().Int("embeddings-dimensions", 0,
		fmt.Sprintf("vector length to request (openai text-embedding-3 models can shorten theirs) and to enforce; 0 accepts what the model returns (defaults to $%s)", embeddingsDimensionsEnv))
	cmd.Flags().Duration("embeddings-timeout", 0,
		fmt.Sprintf("bound on one request to the provider (defaults to $%s, else %s)", embeddingsTimeoutEnv, embeddings.DefaultTimeout))
	cmd.Flags().String("embeddings-store", "",
		fmt.Sprintf(`where vectors live: "encrypted" (default — sealed with the project key, compared in memory) or "pgvector" (plaintext in the database, needs the pgvector extension and `+"`ccd migrate --pgvector`"+`; see docs/adr/0028 for what it reveals) (defaults to $%s)`, embeddingsStoreEnv))
	cmd.Flags().Int("embeddings-cache-mb", 0,
		fmt.Sprintf("memory for decrypted vectors with the encrypted store, in MiB (defaults to $%s, else %d)", embeddingsCacheMBEnv, embeddings.DefaultCacheBytes>>20))
	cmd.Flags().Float64("embeddings-min-similarity", 0,
		fmt.Sprintf("cosine similarity below which a memory is not a semantic hit; depends on the model (defaults to $%s, else %.2f)", embeddingsMinSimilarityEnv, embeddings.DefaultMinSimilarity))
}

func flagOrEnv(cmd *cobra.Command, name, env string) string {
	if v, _ := cmd.Flags().GetString(name); v != "" {
		return v
	}
	return os.Getenv(env)
}

// embeddingsConfig is semantic search's configuration, read from flags and
// the environment and checked before anything starts.
type embeddingsConfig struct {
	provider      embeddings.Provider
	store         string
	cacheBytes    int64
	minSimilarity float64
}

// embeddingsFromFlags reads the configuration. A nil provider means semantic
// search is off, which is the default. Every mistake is a refusal to start:
// an operator who asked for semantic search and got text search would not
// notice until somebody wondered why the results were worse.
func embeddingsFromFlags(cmd *cobra.Command) (embeddingsConfig, error) {
	var cfg embeddingsConfig
	kind := strings.ToLower(strings.TrimSpace(flagOrEnv(cmd, "embeddings-provider", embeddingsProviderEnv)))
	cfg.store = strings.ToLower(strings.TrimSpace(flagOrEnv(cmd, "embeddings-store", embeddingsStoreEnv)))
	if cfg.store == "" {
		cfg.store = embeddings.StoreEncrypted
	}
	if cfg.store != embeddings.StoreEncrypted && cfg.store != embeddings.StorePgvector {
		return cfg, fmt.Errorf("--embeddings-store must be %q or %q, not %q", embeddings.StoreEncrypted, embeddings.StorePgvector, cfg.store)
	}
	if kind == "" {
		return cfg, nil
	}

	c := embeddings.Config{
		URL:   flagOrEnv(cmd, "embeddings-url", embeddingsURLEnv),
		Model: flagOrEnv(cmd, "embeddings-model", embeddingsModelEnv),
	}
	var err error
	if c.Dimensions, err = intFlagOrEnv(cmd, "embeddings-dimensions", embeddingsDimensionsEnv); err != nil {
		return cfg, err
	}
	c.Timeout, _ = cmd.Flags().GetDuration("embeddings-timeout")
	if raw := os.Getenv(embeddingsTimeoutEnv); c.Timeout == 0 && raw != "" {
		if c.Timeout, err = time.ParseDuration(raw); err != nil {
			return cfg, fmt.Errorf("$%s: %w", embeddingsTimeoutEnv, err)
		}
	}
	mb, err := intFlagOrEnv(cmd, "embeddings-cache-mb", embeddingsCacheMBEnv)
	if err != nil {
		return cfg, err
	}
	if mb < 0 {
		return cfg, fmt.Errorf("--embeddings-cache-mb must not be negative")
	}
	cfg.cacheBytes = int64(mb) << 20
	cfg.minSimilarity, _ = cmd.Flags().GetFloat64("embeddings-min-similarity")
	if raw := os.Getenv(embeddingsMinSimilarityEnv); cfg.minSimilarity == 0 && raw != "" {
		if cfg.minSimilarity, err = strconv.ParseFloat(raw, 64); err != nil {
			return cfg, fmt.Errorf("$%s: %w", embeddingsMinSimilarityEnv, err)
		}
	}
	if cfg.minSimilarity < -1 || cfg.minSimilarity > 1 {
		return cfg, fmt.Errorf("--embeddings-min-similarity must be between -1 and 1")
	}

	switch kind {
	case "ollama":
		cfg.provider, err = embeddings.NewOllama(c)
	case "openai":
		path := flagOrEnv(cmd, "embeddings-api-key-file", embeddingsAPIKeyFileEnv)
		if path != "" {
			raw, rerr := os.ReadFile(path) //nolint:gosec // operator-supplied path, same trust level as --tls-key
			if rerr != nil {
				return cfg, fmt.Errorf("read --embeddings-api-key-file: %w", rerr)
			}
			c.APIKey = strings.TrimSpace(string(raw))
		}
		cfg.provider, err = embeddings.NewOpenAI(c)
	default:
		return cfg, fmt.Errorf(`--embeddings-provider must be "ollama" or "openai", not %q`, kind)
	}
	return cfg, err
}

func intFlagOrEnv(cmd *cobra.Command, name, env string) (int, error) {
	v, _ := cmd.Flags().GetInt(name)
	if v != 0 {
		return v, nil
	}
	raw := os.Getenv(env)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("$%s: %w", env, err)
	}
	return n, nil
}

// newIndex builds the index for a configuration that has a provider. With
// pgvector storage it refuses unless the optional schema is in place.
func newIndex(ctx context.Context, cfg embeddingsConfig, pool *db.Pool, cp *crypto.Provider, mem *memories.Service) (*embeddings.Index, error) {
	var store embeddings.Store
	if cfg.store == embeddings.StorePgvector {
		if err := db.CheckPgvectorSchema(ctx, pool); err != nil {
			return nil, err
		}
		store = embeddings.NewPgvectorStore()
	} else {
		store = embeddings.NewEncryptedStore(cp, cfg.cacheBytes)
	}
	return embeddings.NewIndex(cfg.provider, store, pool, mem, embeddings.Options{MinSimilarity: cfg.minSimilarity}), nil
}

// announceEmbeddings states the configuration at startup. Sending memory
// text to an API this installation does not run is a privacy decision, so
// the OpenAI-compatible provider is a warning naming where the text goes.
func announceEmbeddings(cfg embeddingsConfig) {
	if cfg.provider == nil {
		slog.Info("semantic search is off: memories are searched by text (set --embeddings-provider to turn it on)")
		return
	}
	p := cfg.provider
	if p.Name() == "openai" {
		slog.Warn("semantic search sends the title and body of every memory to an external embeddings API — "+
			"whoever runs that endpoint receives them in plaintext (docs/semantic-search.md)",
			"endpoint", p.Endpoint(), "model", p.Model())
	}
	slog.Info("semantic search enabled", "provider", p.Name(), "endpoint", p.Endpoint(), "model", p.Model(), "store", cfg.store)
	if cfg.store == embeddings.StorePgvector {
		slog.Warn("embeddings are stored in plaintext pgvector columns: anyone who can read the database can compare " +
			"memories by meaning (docs/adr/0028); --embeddings-store=encrypted keeps them sealed")
	}
}

func newEmbeddingsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "embeddings", Short: "Operate semantic search of memories (docs/semantic-search.md)"}

	reindex := &cobra.Command{
		Use:   "reindex",
		Short: "Embed every memory whose vector is missing or made by another model or from an older version",
		Long: "Backfills semantic search: run it once after turning a provider on or changing the model, with the same " +
			"--embeddings-* settings and the same $CC_ROOT_KEY as the server, connected as cc_app. A running server " +
			"catches up by itself through its sweep; this does it now and reports progress.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := embeddingsFromFlags(cmd)
			if err != nil {
				return err
			}
			if cfg.provider == nil {
				return fmt.Errorf("no embedding provider configured: pass --embeddings-provider or set $%s", embeddingsProviderEnv)
			}
			dsn, err := resolveDatabaseURL(cmd)
			if err != nil {
				return err
			}
			pool, err := db.NewPool(cmd.Context(), dsn)
			if err != nil {
				return err
			}
			defer pool.Close()
			root, err := crypto.NewEnvRootKeyProvider(rootKeyEnv)
			if err != nil {
				return err
			}
			cp := crypto.NewProvider(root)
			mem := memories.NewService(pool, cp, auth.NewAuthzService(pool, projects.NewStore()))
			ix, err := newIndex(cmd.Context(), cfg, pool, cp, mem)
			if err != nil {
				return err
			}
			all, _ := cmd.Flags().GetBool("all")
			out := cmd.OutOrStdout()
			res, err := ix.Reindex(cmd.Context(), all, func(r embeddings.ReindexResult) {
				_, _ = fmt.Fprintf(out, "embedded %d, skipped %d\n", r.Embedded, r.Skipped)
			})
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "done: embedded %d memories with %s/%s into the %s store, skipped %d\n",
				res.Embedded, cfg.provider.Name(), cfg.provider.Model(), cfg.store, res.Skipped)
			return nil
		},
	}
	reindex.Flags().Bool("all", false, "re-embed every memory, not only missing and stale ones")
	embeddingsFlags(reindex)
	databaseURLFlag(reindex)
	cmd.AddCommand(reindex)
	return cmd
}
