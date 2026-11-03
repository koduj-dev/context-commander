import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { useSearchParams } from "react-router-dom";
import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import {
  api,
  type Board,
  type Counts,
  type Effort,
  type ItemKind,
  type ItemStatus,
  type Person,
  type PhaseStatus,
  type PlanHit,
  type PlanStatus,
  type Presence,
} from "../lib/api";
import { useLive, type Live } from "../lib/live";
import { usePolled, useResource } from "../lib/resource";
import {
  Badge,
  Btn,
  Cell,
  DataTable,
  Def,
  Defs,
  EmptyState,
  ErrorState,
  Loading,
  Meter,
  Mono,
  Note,
  PageHead,
  Panel,
  Row,
  Seg,
  SelectField,
  Stack,
  TextArea,
  TextField,
} from "../ui";
import { useProject } from "./ProjectLayout";

/** How often the board refreshes while it is on screen and no stream is open. */
export const BOARD_REFRESH_MS = 10_000;
/**
 * How often it refreshes while a stream is open. Not never: presence turns
 * idle by the clock, with no write anyone could be told about.
 */
export const LIVE_REFRESH_MS = 60_000;

/** The board's stream, shared so an open plan re-reads on the same pulse. */
const BoardLive = createContext<Live>({ live: false, pulse: 0 });

/**
 * Where the project's work stands (docs/adr/0022).
 *
 * Read top-down it answers, in order: who is working right now and on what,
 * how far each phase is, and what each plan consists of. The board re-reads
 * itself when the server says it changed, and every ten seconds while
 * visible when the server cannot say (lib/live.ts). That is what makes it
 * usable to watch an agent from another machine; nothing here needs a reload.
 *
 * Time is "tracked" and tokens "reported", never "spent": time is the span
 * covered by heartbeats and tokens are what a client chose to send, so both
 * are lower bounds, and the words say so.
 */
export function ProjectPlans() {
  const t = useT();
  const { namespace, slug } = useProject();
  const live = useLive(api.planBoardEvents(namespace, slug));
  const board = usePolled(
    () => api.planBoard(namespace, slug),
    [namespace, slug],
    live.live ? LIVE_REFRESH_MS : BOARD_REFRESH_MS,
    live.pulse,
  );
  // `?plan=…&item=…` is how a search result elsewhere (the quick finder)
  // names what it found: that plan opens and that step is marked.
  const [params] = useSearchParams();
  const linkedPlan = params.get("plan") ?? undefined;
  const linkedItem = params.get("item") ?? undefined;

  if (board.loading) return <Loading />;
  if (board.error || !board.data) return <ErrorState error={board.error} onRetry={board.reload} />;
  const b = board.data;
  // The board leaves abandoned plans out, and a search finds them anyway;
  // such a plan is opened on its own, loaded by id.
  const linkedOffBoard = linkedPlan !== undefined && !b.plans.some((p) => p.id === linkedPlan);

  return (
    <BoardLive.Provider value={live}>
      <PageHead title={t("plans.title")} lead={t("plans.lead")} />
      <Stack>
        {board.stale && <Note tone="warn">{t("plans.stale")}</Note>}
        <Panel
          title={t("plans.workingNow")}
          sub={live.live ? t("plans.live") : t("plans.refreshes")}
          tight
        >
          <PresenceTable items={b.presence} people={b.people} />
        </Panel>

        {b.blocked.length > 0 && (
          <Panel title={t("plans.blockedTitle", { count: b.blocked.length })} tight>
            <DataTable columns={[{ key: "what" }, { key: "who", align: "r" }]}>
              {b.blocked.map((a) => (
                <tr key={a.item.id}>
                  <td>
                    <Cell
                      primary={a.item.text}
                      secondary={
                        <span className="dim">
                          {a.plan_title}
                          {a.item.note ? ` · ${a.item.note}` : ""}
                        </span>
                      }
                    />
                  </td>
                  <td className="r">{personName(b.people, a.item.assignee, t("plans.nobody"))}</td>
                </tr>
              ))}
            </DataTable>
          </Panel>
        )}

        <Phases board={b} onChanged={board.reload} />
        {linkedOffBoard && linkedPlan && (
          <Panel title={t("plans.linkedPlan")}>
            <PlanDetail
              planID={linkedPlan}
              highlight={linkedItem}
              board={b}
              onChanged={board.reload}
            />
          </Panel>
        )}
        <Plans
          board={b}
          onChanged={board.reload}
          linked={linkedOffBoard ? undefined : linkedPlan}
          highlight={linkedItem}
        />
        <Search />
      </Stack>
    </BoardLive.Provider>
  );
}

function personName(people: Record<string, Person>, id: string | undefined, fallback: string) {
  if (!id) return fallback;
  return people[id]?.name || fallback;
}

/**
 * Who is where. Used here for one project and on the dashboard for all of
 * them, which is the only difference `showProject` makes.
 */
export function PresenceTable({
  items,
  people,
  showProject,
}: {
  items: Presence[];
  people: Record<string, Person>;
  showProject?: boolean;
}) {
  const t = useT();
  const f = useFormat();
  if (items.length === 0) {
    return <EmptyState title={t("plans.nobodyPresent")} body={t("plans.nobodyPresentBody")} />;
  }
  return (
    <DataTable
      columns={[
        { key: "who", label: t("plans.column.who") },
        { key: "where", label: t("plans.column.where") },
        { key: "seen", label: t("plans.column.seen"), align: "r" },
      ]}
    >
      {items.map((p) => {
        const person = people[p.principal_id];
        const where = p.item_text
          ? t("plans.onItem", { plan: p.plan_title ?? "", item: p.item_text })
          : p.plan_title || t("plans.inProject");
        return (
          <tr key={p.client_id}>
            <td>
              <Cell
                primary={
                  <>
                    {person?.name || t("plans.nobody")}
                    {p.you && (
                      <>
                        {" "}
                        <Badge tone="active">{t("plans.you")}</Badge>
                      </>
                    )}
                  </>
                }
                secondary={
                  <>
                    <Mono dim>{p.client_type}</Mono>{" "}
                    {person?.kind === "service" && <Badge>{t("plans.agent")}</Badge>}
                  </>
                }
              />
            </td>
            <td>
              <Cell
                primary={
                  showProject ? (
                    <>
                      <Mono>{p.project}</Mono> · {where}
                    </>
                  ) : (
                    where
                  )
                }
                secondary={p.activity ? <span className="dim">{p.activity}</span> : undefined}
              />
            </td>
            <td className="r nowrap">
              {p.idle ? (
                <Badge tone="warn">{t("plans.idleSince", { when: f.relative(p.last_seen) })}</Badge>
              ) : (
                <Badge tone="ok">
                  {t("plans.activeFor", { duration: f.duration(secondsSince(p.since)) })}
                </Badge>
              )}
            </td>
          </tr>
        );
      })}
    </DataTable>
  );
}

function secondsSince(iso: string): number {
  return Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
}

/** Done of everything that counts: skipped steps are neither done nor owed. */
export function progress(c: Counts): { done: number; total: number; percent: number } {
  const total = c.open + c.in_progress + c.done + c.blocked;
  return { done: c.done, total, percent: total ? Math.round((c.done / total) * 100) : 0 };
}

function EffortCell({ effort }: { effort: Effort }) {
  const t = useT();
  const f = useFormat();
  const tokens =
    effort.tokens_in + effort.tokens_out + effort.cache_read_tokens + effort.cache_write_tokens;
  if (effort.seconds === 0 && tokens === 0) return <span className="dim">—</span>;
  return (
    <Cell
      primary={
        effort.seconds > 0 ? t("plans.tracked", { duration: f.duration(effort.seconds) }) : "—"
      }
      secondary={
        tokens > 0 ? (
          <>
            {/* The split is what a cost comparison needs: the four kinds are
                priced differently, and for an agent nearly all input is cache. */}
            <span
              className="dim"
              title={t("plans.tokensBreakdown", {
                in: f.number(effort.tokens_in),
                out: f.number(effort.tokens_out),
                cacheRead: f.number(effort.cache_read_tokens),
                cacheWrite: f.number(effort.cache_write_tokens),
              })}
            >
              {t("plans.tokensReported", { count: tokens })}
            </span>
            <CostEstimateNote effort={effort} />
          </>
        ) : undefined
      }
    />
  );
}

/**
 * What the tokens would have cost at the operator's prices — shown only when
 * the server has a price table, and always worded as an estimate: the tokens
 * are what clients reported, the prices what the operator configured.
 */
function CostEstimateNote({ effort }: { effort: Effort }) {
  const t = useT();
  const f = useFormat();
  const est = effort.cost_estimate;
  if (!est) return null;
  const priced = (effort.models ?? []).some((m) => m.estimated_cost !== null);
  const amount = f.money(est.amount, est.currency);
  const label = !priced
    ? t("plans.costNoPrice")
    : est.unpriced_models.length > 0
      ? t("plans.costEstimatePartial", { amount })
      : t("plans.costEstimate", { amount });
  const lines = (effort.models ?? []).map((m) => {
    const tokens = t("plans.modelTokens", {
      model: m.model || t("plans.modelNotStated"),
      in: f.number(m.tokens_in),
      out: f.number(m.tokens_out),
      cacheRead: f.number(m.cache_read_tokens),
      cacheWrite: f.number(m.cache_write_tokens),
    });
    const cost =
      m.estimated_cost === null
        ? t("plans.modelNoPrice")
        : t("plans.costEstimate", { amount: f.money(m.estimated_cost, est.currency) });
    return `${tokens} — ${cost}`;
  });
  if (est.prices_as_of) lines.push(t("plans.pricesAsOf", { date: est.prices_as_of }));
  return (
    <>
      <span className="dim"> · </span>
      <span className="dim" title={lines.join("\n")}>
        {label}
      </span>
    </>
  );
}

/**
 * Move up and move down, for a row in an ordered list. Buttons rather than
 * dragging: they work from the keyboard and with a screen reader, and need
 * no library. `onMove` gets the 1-based place to move to, which is what the
 * server takes (docs/adr/0022, "Reordering").
 */
function MoveButtons({
  name,
  index,
  count,
  disabled,
  onMove,
}: {
  name: string;
  index: number;
  count: number;
  disabled: boolean;
  onMove: (position: number) => void;
}) {
  const t = useT();
  return (
    <>
      <Btn
        small
        label={t("plans.moveUp", { name })}
        title={t("plans.moveUp", { name })}
        disabled={disabled || index === 0}
        onClick={() => onMove(index)}
      >
        ↑
      </Btn>{" "}
      <Btn
        small
        label={t("plans.moveDown", { name })}
        title={t("plans.moveDown", { name })}
        disabled={disabled || index === count - 1}
        onClick={() => onMove(index + 2)}
      >
        ↓
      </Btn>
    </>
  );
}

// ---------------------------------------------------------------- phases

function Phases({ board, onChanged }: { board: Board; onChanged: () => void }) {
  const t = useT();
  const { namespace, slug } = useProject();
  const [title, setTitle] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  const statuses: { value: PhaseStatus; label: string }[] = [
    { value: "planned", label: t("plans.phase.planned") },
    { value: "active", label: t("plans.phase.active") },
    { value: "done", label: t("plans.phase.done") },
  ];

  async function run(action: () => Promise<unknown>) {
    setBusy(true);
    setFailure(undefined);
    try {
      await action();
      onChanged();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel title={t("plans.phases")} tight>
      {board.phases.length === 0 ? (
        <EmptyState title={t("plans.noPhases")} body={t("plans.noPhasesBody")} />
      ) : (
        <DataTable
          columns={[
            { key: "order", label: t("plans.column.order") },
            { key: "phase", label: t("plans.column.phase") },
            { key: "progress", label: t("plans.column.progress") },
            { key: "effort", label: t("plans.column.effort") },
            { key: "status", label: t("plans.column.status"), align: "r" },
          ]}
        >
          {board.phases.map((ph, i) => {
            const p = progress(ph.counts);
            return (
              <tr key={ph.id}>
                <td className="nowrap">
                  <MoveButtons
                    name={ph.title}
                    index={i}
                    count={board.phases.length}
                    disabled={busy}
                    onMove={(position) =>
                      void run(() => api.updatePhase(namespace, slug, ph.id, { position }))
                    }
                  />
                </td>
                <td>
                  <Cell
                    primary={ph.title}
                    secondary={ph.goal ? <span className="dim">{ph.goal}</span> : undefined}
                  />
                </td>
                <td>
                  <span className="dim">{t("plans.doneOf", { done: p.done, total: p.total })}</span>
                  <div className="mt-sm">
                    <Meter percent={p.percent} />
                  </div>
                </td>
                <td>
                  <EffortCell effort={ph.effort} />
                </td>
                <td className="r nowrap">
                  <SelectField
                    label={t("plans.phaseStatusFor", { name: ph.title })}
                    value={ph.status}
                    options={statuses}
                    onChange={(status) =>
                      void run(() => api.updatePhase(namespace, slug, ph.id, { status }))
                    }
                  />
                </td>
              </tr>
            );
          })}
        </DataTable>
      )}
      <div className="body">
        <Row gap="sm">
          <TextField
            label={t("plans.newPhase")}
            placeholder={t("plans.newPhasePlaceholder")}
            value={title}
            onChange={setTitle}
            grow
          />
          <Btn
            disabled={busy || !title.trim()}
            onClick={() =>
              void run(async () => {
                await api.createPhase(namespace, slug, title.trim());
                setTitle("");
              })
            }
          >
            {t("plans.addPhase")}
          </Btn>
        </Row>
        {failure !== undefined && <ErrorState error={failure} />}
      </div>
    </Panel>
  );
}

// ---------------------------------------------------------------- plans

export function planStatusLabel(t: ReturnType<typeof useT>, s: PlanStatus) {
  return {
    draft: t("plans.planStatus.draft"),
    active: t("plans.planStatus.active"),
    done: t("plans.planStatus.done"),
    abandoned: t("plans.planStatus.abandoned"),
  }[s];
}

function Plans({
  board,
  onChanged,
  linked,
  highlight,
}: {
  board: Board;
  onChanged: () => void;
  linked?: string;
  highlight?: string;
}) {
  const t = useT();
  const [open, setOpen] = useState<string | undefined>(linked);
  // A second search result picked while this screen is showing changes the
  // query string, not the component; the plan it names opens all the same.
  useEffect(() => {
    if (linked) setOpen(linked);
  }, [linked]);
  const phaseTitle = (id?: string) => board.phases.find((p) => p.id === id)?.title;

  return (
    <Panel title={t("plans.plans")} tight>
      {board.plans.length === 0 ? (
        <EmptyState title={t("plans.noPlans")} body={t("plans.noPlansBody")} />
      ) : (
        <DataTable
          columns={[
            { key: "plan", label: t("plans.column.plan") },
            { key: "progress", label: t("plans.column.progress") },
            { key: "effort", label: t("plans.column.effort") },
            { key: "actions", align: "r" },
          ]}
        >
          {board.plans.map((p) => {
            const pr = progress(p.counts);
            const phase = phaseTitle(p.phase_id);
            return (
              <PlanRows
                key={p.id}
                planID={p.id}
                open={open === p.id}
                highlight={p.id === linked ? highlight : undefined}
                onToggle={() => setOpen(open === p.id ? undefined : p.id)}
                board={board}
                onChanged={onChanged}
                summary={
                  <>
                    <td>
                      <Cell
                        primary={p.title}
                        secondary={
                          <>
                            <Badge
                              tone={
                                p.status === "active"
                                  ? "active"
                                  : p.status === "done"
                                    ? "ok"
                                    : "neutral"
                              }
                            >
                              {planStatusLabel(t, p.status)}
                            </Badge>{" "}
                            {phase && <span className="dim">{phase}</span>}
                            {p.counts.blocked > 0 && (
                              <>
                                {" "}
                                <Badge tone="danger">
                                  {t("plans.blockedCount", { count: p.counts.blocked })}
                                </Badge>
                              </>
                            )}
                          </>
                        }
                      />
                    </td>
                    <td>
                      <span className="dim">
                        {t("plans.doneOf", { done: pr.done, total: pr.total })}
                      </span>
                      <div className="mt-sm">
                        <Meter percent={pr.percent} />
                      </div>
                    </td>
                    <td>
                      <EffortCell effort={p.effort} />
                    </td>
                  </>
                }
              />
            );
          })}
        </DataTable>
      )}
      <div className="body">
        <NewPlan board={board} onCreated={onChanged} />
      </div>
    </Panel>
  );
}

function PlanRows({
  planID,
  open,
  highlight,
  onToggle,
  summary,
  board,
  onChanged,
}: {
  planID: string;
  open: boolean;
  highlight?: string;
  onToggle: () => void;
  summary: ReactNode;
  board: Board;
  onChanged: () => void;
}) {
  const t = useT();
  return (
    <>
      <tr>
        {summary}
        <td className="r">
          <Btn small onClick={onToggle}>
            {open ? t("common.close") : t("plans.openPlan")}
          </Btn>
        </td>
      </tr>
      {open && (
        <tr>
          <td colSpan={4}>
            <PlanDetail planID={planID} highlight={highlight} board={board} onChanged={onChanged} />
          </td>
        </tr>
      )}
    </>
  );
}

export function itemStatusOptions(
  t: ReturnType<typeof useT>,
): { value: ItemStatus; label: string }[] {
  return [
    { value: "open", label: t("plans.item.open") },
    { value: "in_progress", label: t("plans.item.in_progress") },
    { value: "done", label: t("plans.item.done") },
    { value: "blocked", label: t("plans.item.blocked") },
    { value: "skipped", label: t("plans.item.skipped") },
  ];
}

/**
 * One plan, item by item. Loaded when opened rather than with the board: the
 * board carries counts, and twenty plans' worth of items on every ten-second
 * refresh would be most of the payload for rows nobody expanded. It refreshes
 * along with the board, though, so an agent ticking a step shows up here too.
 */
function PlanDetail({
  planID,
  highlight,
  board,
  onChanged,
}: {
  planID: string;
  /** The step a search result pointed at: marked, and scrolled to once. */
  highlight?: string;
  board: Board;
  onChanged: () => void;
}) {
  const t = useT();
  const { namespace, slug } = useProject();
  const live = useContext(BoardLive);
  const plan = usePolled(
    () => api.plan(namespace, slug, planID),
    [namespace, slug, planID],
    live.live ? LIVE_REFRESH_MS : BOARD_REFRESH_MS,
    live.pulse,
  );
  const [failure, setFailure] = useState<unknown>();
  const [adding, setAdding] = useState("");
  const [addKind, setAddKind] = useState<ItemKind>("task");
  const [addOpen, setAddOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const highlightRow = useRef<HTMLTableRowElement>(null);
  const loaded = plan.data !== undefined;
  // Once per step, not on every ten-second refresh: scrolling someone back
  // to a row they have moved away from would make the screen unusable.
  useEffect(() => {
    if (loaded && highlight) highlightRow.current?.scrollIntoView?.({ block: "center" });
  }, [loaded, highlight]);

  async function run(action: () => Promise<unknown>) {
    setBusy(true);
    setFailure(undefined);
    try {
      await action();
      plan.reload();
      onChanged();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  if (plan.loading) return <Loading />;
  if (plan.error || !plan.data) return <ErrorState error={plan.error} onRetry={plan.reload} />;
  const p = plan.data;
  const options = itemStatusOptions(t);
  const lines = adding
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean);

  const planStatuses: { value: PlanStatus; label: string }[] = (
    ["draft", "active", "done", "abandoned"] as PlanStatus[]
  ).map((value) => ({ value, label: planStatusLabel(t, value) }));
  const phaseOptions = [
    { value: "", label: t("plans.noPhase") },
    ...board.phases.map((ph) => ({ value: ph.id, label: ph.title })),
  ];

  return (
    <div className="stack">
      {p.body && <p className="dim">{p.body}</p>}
      <Defs>
        <Def term={t("plans.column.status")}>
          <SelectField
            label={t("plans.planStatusFor", { name: p.title })}
            value={p.status}
            options={planStatuses}
            onChange={(status) => void run(() => api.updatePlan(namespace, slug, p.id, { status }))}
          />
        </Def>
        <Def term={t("plans.column.phase")}>
          <SelectField
            label={t("plans.phaseFor", { name: p.title })}
            value={p.phase_id ?? ""}
            options={phaseOptions}
            onChange={(phase) =>
              void run(() => api.updatePlan(namespace, slug, p.id, { phase_id: phase || null }))
            }
          />
        </Def>
      </Defs>

      {p.items.length === 0 ? (
        <EmptyState title={t("plans.noItems")} />
      ) : (
        <DataTable
          columns={[
            { key: "order", label: t("plans.column.order") },
            { key: "step", label: t("plans.column.step") },
            { key: "who", label: t("plans.column.who") },
            { key: "effort", label: t("plans.column.effort") },
            { key: "status", label: t("plans.column.status"), align: "r" },
          ]}
        >
          {p.items.map((it, i) => (
            <tr
              key={it.id}
              className={it.id === highlight ? "active" : undefined}
              aria-current={it.id === highlight ? "true" : undefined}
              ref={it.id === highlight ? highlightRow : undefined}
            >
              <td className="nowrap">
                <MoveButtons
                  name={it.text}
                  index={i}
                  count={p.items.length}
                  disabled={busy}
                  onMove={(position) =>
                    void run(() => api.updatePlanItem(namespace, slug, it.id, { position }))
                  }
                />
              </td>
              <td>
                <Cell
                  primary={
                    <>
                      {it.kind === "check" && (
                        <>
                          <Badge tone="info">{t("plans.check")}</Badge>{" "}
                        </>
                      )}
                      {it.text}
                    </>
                  }
                  secondary={it.note ? <span className="dim">{it.note}</span> : undefined}
                />
              </td>
              <td>
                {personName(board.people, it.assignee, t("plans.nobody"))}
                {it.status !== "done" && it.status !== "skipped" && (
                  <div className="mt-sm">
                    <Btn
                      small
                      disabled={busy}
                      title={t("plans.workOnThisHelp")}
                      onClick={() =>
                        void run(async () => {
                          await api.updatePlanItem(namespace, slug, it.id, {
                            status: "in_progress",
                          });
                          await api.reportPresence(namespace, slug, it.id);
                        })
                      }
                    >
                      {t("plans.workOnThis")}
                    </Btn>
                  </div>
                )}
              </td>
              <td>
                <EffortCell effort={it.effort} />
              </td>
              <td className="r">
                <SelectField
                  label={t("plans.statusFor", { item: it.text })}
                  value={it.status}
                  options={options}
                  onChange={(status) =>
                    void run(() => api.updatePlanItem(namespace, slug, it.id, { status }))
                  }
                />
              </td>
            </tr>
          ))}
        </DataTable>
      )}

      {addOpen ? (
        <>
          <TextArea
            label={t("plans.addItems")}
            value={adding}
            onChange={setAdding}
            rows={3}
            labelled
          />
          <Row gap="sm">
            <Seg
              label={t("plans.kind")}
              value={addKind}
              onChange={setAddKind}
              options={[
                { value: "task", label: t("plans.task") },
                { value: "check", label: t("plans.check") },
              ]}
            />
            <Btn
              small
              kind="primary"
              disabled={busy || lines.length === 0}
              onClick={() =>
                void run(async () => {
                  await api.addPlanItems(
                    namespace,
                    slug,
                    p.id,
                    lines.map((text) => ({ text, kind: addKind })),
                  );
                  setAdding("");
                  setAddOpen(false);
                })
              }
            >
              {t("plans.addItemsButton", { count: lines.length })}
            </Btn>
            <Btn small onClick={() => setAddOpen(false)}>
              {t("common.cancel")}
            </Btn>
          </Row>
        </>
      ) : (
        <Row gap="sm">
          <Btn small onClick={() => setAddOpen(true)}>
            {t("plans.addSteps")}
          </Btn>
          <Btn
            small
            kind="danger"
            disabled={busy}
            onClick={() => {
              if (window.confirm(t("plans.confirmDelete", { name: p.title }))) {
                void run(() => api.deletePlan(namespace, slug, p.id));
              }
            }}
          >
            {t("plans.deletePlan")}
          </Btn>
        </Row>
      )}
      {failure !== undefined && <ErrorState error={failure} />}
    </div>
  );
}

/**
 * Writing a plan by hand. Folded away until asked for: most plans arrive from
 * agents through plan.create, and an open form would be the largest thing on
 * a screen whose job is to show what is happening.
 */
function NewPlan({ board, onCreated }: { board: Board; onCreated: () => void }) {
  const t = useT();
  const { namespace, slug } = useProject();
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [items, setItems] = useState("");
  const [phase, setPhase] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  const lines = items
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean);

  async function create() {
    if (!title.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      await api.createPlan(namespace, slug, {
        title: title.trim(),
        phase_id: phase || undefined,
        items: lines.map((text) => ({ text })),
      });
      setTitle("");
      setItems("");
      setOpen(false);
      onCreated();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  if (!open) {
    return (
      <Btn small onClick={() => setOpen(true)}>
        {t("plans.newPlan")}
      </Btn>
    );
  }

  return (
    <div className="stack">
      <TextField label={t("plans.newPlanTitle")} value={title} onChange={setTitle} labelled />
      <SelectField
        label={t("plans.column.phase")}
        value={phase}
        onChange={setPhase}
        options={[
          { value: "", label: t("plans.noPhase") },
          ...board.phases.map((ph) => ({ value: ph.id, label: ph.title })),
        ]}
      />
      <TextArea
        label={t("plans.newPlanItems")}
        value={items}
        onChange={setItems}
        rows={4}
        labelled
      />
      <Row gap="sm">
        <Btn kind="primary" small disabled={busy || !title.trim()} onClick={() => void create()}>
          {busy ? t("common.loading") : t("plans.createPlan")}
        </Btn>
        <Btn small onClick={() => setOpen(false)}>
          {t("common.cancel")}
        </Btn>
      </Row>
      {failure !== undefined && <ErrorState error={failure} />}
    </div>
  );
}

// ---------------------------------------------------------------- search

function Search() {
  const t = useT();
  const { namespace, slug } = useProject();
  const [query, setQuery] = useState("");
  const [submitted, setSubmitted] = useState("");
  const hits = useResource<PlanHit[] | undefined>(
    () => (submitted ? api.searchPlans(namespace, slug, submitted) : Promise.resolve(undefined)),
    [namespace, slug, submitted],
  );
  const options = itemStatusOptions(t);
  const statusLabel = (s: string) => options.find((o) => o.value === s)?.label;

  return (
    <Panel title={t("plans.search")} tight>
      <div className="body">
        <Row gap="sm">
          <TextField
            label={t("plans.search")}
            type="search"
            placeholder={t("plans.searchPlaceholder")}
            value={query}
            onChange={setQuery}
            grow
          />
          <Btn disabled={!query.trim()} onClick={() => setSubmitted(query.trim())}>
            {t("plans.searchButton")}
          </Btn>
        </Row>
      </div>
      {submitted &&
        (hits.loading ? (
          <Loading />
        ) : hits.error ? (
          <ErrorState error={hits.error} onRetry={hits.reload} />
        ) : !hits.data || hits.data.length === 0 ? (
          <EmptyState title={t("plans.noHits", { query: submitted })} />
        ) : (
          <DataTable columns={[{ key: "what" }, { key: "status", align: "r" }]}>
            {hits.data.map((h) => (
              <tr key={`${h.plan_id}:${h.item_id ?? ""}`}>
                <td>
                  <Cell
                    primary={h.text}
                    secondary={h.item_id ? <span className="dim">{h.plan_title}</span> : undefined}
                  />
                </td>
                <td className="r">
                  <Badge>
                    {h.item_id ? statusLabel(h.status) : planStatusLabel(t, h.plan_status)}
                  </Badge>
                </td>
              </tr>
            ))}
          </DataTable>
        ))}
    </Panel>
  );
}
