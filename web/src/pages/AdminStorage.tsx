import { useState } from "react";
import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { useFormat, usePrefs } from "../app/prefs";
import { api, type NamespaceStorage, type StorageLimit } from "../lib/api";
import { useList } from "../lib/resource";
import {
  Badge,
  Btn,
  Cell,
  DataTable,
  EmptyState,
  ErrorState,
  Loading,
  Meter,
  Mono,
  PageHead,
  Panel,
  Pager,
  Row,
  Seg,
  SelectField,
  Stack,
  TextField,
} from "../ui";

/**
 * What every user and organization holds, and what each may hold
 * (docs/adr/0021).
 *
 * One row per namespace and nothing below it: a slug and numbers, never a
 * project or a path, so this screen stays on the side of the boundary the
 * Usage tab keeps (migrations/000024). An admin sets exceptions here; the
 * default itself is the server's `--storage-quota` and is shown, not edited,
 * because a value that lives in a flag would be overwritten on restart.
 */
export function AdminStorage() {
  const t = useT();
  useCrumbs("admin:storage", () => ({
    path: [t("admin.title")],
    current: t("admin.tab.storage"),
  }));
  const rows = useList((cursor) => api.installationStorage(cursor), []);

  return (
    <>
      <PageHead title={t("admin.storage.title")} lead={t("admin.storage.lead")} />
      <Stack>
        <Panel
          title={t("admin.storage.namespaces")}
          tight
          footer={
            <>
              <span className="dim">{t("admin.storage.help")}</span>
              <Pager
                shown={rows.items.length}
                hasMore={rows.hasMore}
                onLoadMore={rows.loadMore}
                busy={rows.loadingMore}
              />
            </>
          }
        >
          {rows.loading ? (
            <Loading />
          ) : rows.error ? (
            <ErrorState error={rows.error} onRetry={rows.reload} />
          ) : rows.items.length === 0 ? (
            <EmptyState title={t("admin.storage.none")} />
          ) : (
            <DataTable
              columns={[
                { key: "who", label: t("admin.storage.owner") },
                { key: "used", label: t("admin.storage.used") },
                { key: "limit", label: t("admin.storage.limit"), align: "r" },
              ]}
            >
              {rows.items.map((n) => (
                <StorageRow key={n.namespace_id} ns={n} onChanged={rows.reload} />
              ))}
            </DataTable>
          )}
        </Panel>
      </Stack>
    </>
  );
}

/** Used share of a limit, for a meter. Over 100 is possible and shown as full. */
export function usedPercent(n: NamespaceStorage): number | undefined {
  if (n.limit_bytes === undefined) return undefined;
  if (n.limit_bytes === 0) return n.used_bytes > 0 ? 100 : 0;
  return Math.min(100, Math.round((n.used_bytes / n.limit_bytes) * 100));
}

export function meterTone(percent: number): "warn" | "danger" | undefined {
  if (percent >= 100) return "danger";
  if (percent >= 80) return "warn";
  return undefined;
}

function StorageRow({ ns, onChanged }: { ns: NamespaceStorage; onChanged: () => void }) {
  const t = useT();
  const f = useFormat();
  const [open, setOpen] = useState(false);
  const percent = usedPercent(ns);

  const limit =
    ns.override === "unlimited"
      ? t("admin.storage.unlimited")
      : ns.limit_bytes === undefined
        ? t("admin.storage.noDefault")
        : ns.override === "custom"
          ? t("admin.storage.custom", { limit: f.bytes(ns.limit_bytes) })
          : t("admin.storage.default", { limit: f.bytes(ns.limit_bytes) });

  return (
    <>
      <tr>
        <td>
          <Cell
            primary={ns.display_name}
            secondary={
              <>
                <Mono>{ns.slug}</Mono>{" "}
                <Badge>
                  {ns.kind === "organization"
                    ? t("admin.storage.organization")
                    : t("admin.storage.personal")}
                </Badge>
              </>
            }
          />
        </td>
        <td>
          {f.bytes(ns.used_bytes)}
          {percent !== undefined && (
            <div className="mt-sm">
              <Meter percent={percent} tone={meterTone(percent)} />
            </div>
          )}
        </td>
        <td className="r nowrap">
          <span className="dim">{limit}</span>{" "}
          <Btn small onClick={() => setOpen((v) => !v)}>
            {t("admin.storage.change")}
          </Btn>
        </td>
      </tr>
      {open && (
        <tr>
          <td colSpan={3}>
            <LimitEditor
              ns={ns}
              onDone={() => {
                setOpen(false);
                onChanged();
              }}
              onCancel={() => setOpen(false)}
            />
          </td>
        </tr>
      )}
    </>
  );
}

const DECIMAL_UNITS = [
  { value: "MB", factor: 1e6 },
  { value: "GB", factor: 1e9 },
  { value: "TB", factor: 1e12 },
] as const;
const BINARY_UNITS = [
  { value: "MiB", factor: 2 ** 20 },
  { value: "GiB", factor: 2 ** 30 },
  { value: "TiB", factor: 2 ** 40 },
] as const;

/**
 * Reads an amount the way a person types it, in their own locale: "1,5" for
 * a Czech reader is one and a half. Returns undefined for anything that is
 * not a non-negative number.
 */
export function parseAmount(raw: string): number | undefined {
  const s = raw.trim().replace(/\s/g, "").replace(",", ".");
  if (!/^\d+(\.\d+)?$/.test(s)) return undefined;
  return Number(s);
}

function LimitEditor({
  ns,
  onDone,
  onCancel,
}: {
  ns: NamespaceStorage;
  onDone: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const { prefs } = usePrefs();
  const units = prefs.units === "binary" ? BINARY_UNITS : DECIMAL_UNITS;
  type Unit = (typeof units)[number]["value"];

  const [kind, setKind] = useState<StorageLimit["kind"]>(ns.override ?? "default");
  const [amount, setAmount] = useState("");
  const [unit, setUnit] = useState<Unit>(units[1].value);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  const parsed = parseAmount(amount);
  const factor = units.find((u) => u.value === unit)?.factor ?? 1;
  const ready = kind !== "custom" || parsed !== undefined;

  async function save() {
    if (!ready || busy) return;
    const limit: StorageLimit =
      kind === "custom" ? { kind, bytes: Math.round((parsed ?? 0) * factor) } : { kind };
    setBusy(true);
    setFailure(undefined);
    try {
      await api.setStorageLimit(ns.namespace_id, limit);
      onDone();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="stack">
      <Seg
        label={t("admin.storage.limitFor", { name: ns.display_name })}
        value={kind}
        onChange={setKind}
        options={[
          { value: "default", label: t("admin.storage.useDefault") },
          { value: "custom", label: t("admin.storage.useCustom") },
          { value: "unlimited", label: t("admin.storage.useUnlimited") },
        ]}
      />
      {kind === "custom" && (
        <Row gap="sm">
          <TextField
            label={t("admin.storage.amount")}
            value={amount}
            onChange={setAmount}
            labelled
          />
          <SelectField
            label={t("admin.storage.unit")}
            value={unit}
            onChange={setUnit}
            options={units.map((u) => ({ value: u.value as Unit, label: u.value }))}
          />
        </Row>
      )}
      <Row gap="sm">
        <Btn kind="primary" small onClick={() => void save()} disabled={busy || !ready}>
          {busy ? t("common.loading") : t("admin.storage.save")}
        </Btn>
        <Btn small onClick={onCancel}>
          {t("common.cancel")}
        </Btn>
      </Row>
      {failure !== undefined && <ErrorState error={failure} />}
    </div>
  );
}
