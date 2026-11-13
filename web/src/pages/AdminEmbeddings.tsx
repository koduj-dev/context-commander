import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import { api, type EmbeddingsStatus } from "../lib/api";
import { useResource } from "../lib/resource";
import { Badge, Def, Defs, ErrorState, Loading, Meter, Mono, Note, Panel, Stack } from "../ui";

/**
 * Semantic search, as an administrator needs to see it (docs/adr/0028):
 * where memory text is sent to be embedded, whether that endpoint answers,
 * and whether the queue keeps up. No memory text and no vector is shown —
 * the endpoint never returns either.
 *
 * "Where does our text go" comes first, because it is the question an
 * administrator is asked; the health below it is what they check when
 * someone reports that search by meaning stopped finding things.
 */
export function EmbeddingsCard() {
  const t = useT();
  const status = useResource(() => api.embeddingsStatus(), []);

  return (
    <Panel title={t("embeddings.title")}>
      {status.loading ? (
        <Loading />
      ) : status.error || !status.data ? (
        <ErrorState error={status.error} onRetry={status.reload} />
      ) : (
        <EmbeddingsBody s={status.data} />
      )}
    </Panel>
  );
}

export function EmbeddingsBody({ s }: { s: EmbeddingsStatus }) {
  const t = useT();
  const f = useFormat();
  if (!s.configured) {
    return <Note tone="info">{t("embeddings.off")}</Note>;
  }
  const reach =
    s.reachable === undefined ? (
      <Badge>{t("embeddings.unchecked")}</Badge>
    ) : s.reachable ? (
      <Badge tone="ok">{t("embeddings.reachable")}</Badge>
    ) : (
      <Badge tone="danger">{t("embeddings.unreachable")}</Badge>
    );
  const cache =
    s.cache_limit_bytes && s.cache_limit_bytes > 0
      ? Math.round(((s.cache_bytes ?? 0) / s.cache_limit_bytes) * 100)
      : undefined;

  return (
    <Stack gap="sm">
      <Defs>
        <Def term={t("embeddings.provider")}>
          <Mono>{s.provider}</Mono>
        </Def>
        <Def term={t("embeddings.model")}>
          <Mono>{s.model}</Mono>
          {s.dimensions ? (
            <span className="dim"> {t("embeddings.dimensions", { count: s.dimensions })}</span>
          ) : null}
        </Def>
        {s.endpoint && (
          <Def term={t("embeddings.endpoint")}>
            <Mono truncate>{s.endpoint}</Mono>
          </Def>
        )}
        <Def term={t("embeddings.store")}>
          {s.store === "pgvector"
            ? t("embeddings.store.pgvector")
            : t("embeddings.store.encrypted")}
        </Def>
        <Def term={t("embeddings.health")}>{reach}</Def>
        {s.last_success && (
          <Def term={t("embeddings.lastSuccess")}>{f.relative(s.last_success)}</Def>
        )}
        <Def term={t("embeddings.pending")}>
          {s.pending < 0 ? (
            <span className="dim">{t("embeddings.unknown")}</span>
          ) : (
            <span className="num">{f.number(s.pending)}</span>
          )}
        </Def>
        <Def term={t("embeddings.queue")}>
          <span className="num">
            {s.queue_capacity
              ? t("embeddings.queueOf", {
                  queued: f.number(s.queue),
                  capacity: f.number(s.queue_capacity),
                })
              : f.number(s.queue)}
          </span>
        </Def>
        {cache !== undefined && (
          <Def term={t("embeddings.cache")}>
            <Meter percent={cache} tone={cache >= 90 ? "warn" : undefined} />
            <span className="dim">
              {t("embeddings.cacheOf", {
                used: f.bytes(s.cache_bytes ?? 0),
                limit: f.bytes(s.cache_limit_bytes ?? 0),
              })}
            </span>
          </Def>
        )}
      </Defs>
      {s.last_error && (
        <Note tone="warn">
          {s.last_error_at
            ? t("embeddings.lastErrorAt", {
                when: f.relative(s.last_error_at),
                error: s.last_error,
              })
            : t("embeddings.lastError", { error: s.last_error })}
        </Note>
      )}
      {s.dropped > 0 && <Note tone="warn">{t("embeddings.dropped", { count: s.dropped })}</Note>}
    </Stack>
  );
}
