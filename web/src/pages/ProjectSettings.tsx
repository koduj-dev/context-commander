import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useT } from "../i18n";
import { api } from "../lib/api";
import {
  Btn,
  Code,
  Copyable,
  Def,
  Defs,
  ErrorState,
  Grid,
  Mono,
  Note,
  PageHead,
  Panel,
  Stack,
  TextField,
} from "../ui";
import { useProject } from "./ProjectLayout";

/**
 * What this project is.
 *
 * Entirely read-only, and that is the API's shape rather than an omission:
 * there is no update and no delete for a project over HTTP. Renaming,
 * changing visibility and deleting are CLI operations, so this screen states
 * that instead of showing controls that would open a dialog and then fail.
 *
 * The connection block is the useful half — it is what someone actually came
 * here for: how to point an agent at this project.
 */
export function ProjectSettings() {
  const t = useT();
  const { project, namespace, slug } = useProject();

  // The server the browser is talking to is the server an agent should talk
  // to. Deriving it from the address bar beats a configured value that is
  // wrong on every deployment but the one it was written for.
  const base = window.location.origin;

  return (
    <>
      <PageHead title={t("settings.title")} lead={t("settings.lead")} />

      <Grid cols="2-1">
        <Stack>
          <Panel title={t("settings.connect")}>
            <p className="sec lead-out">{t("settings.connectLead")}</p>
            <Defs>
              <Def term={t("settings.mcpEndpoint")}>
                <Mono>{`${base}/mcp`}</Mono>
              </Def>
              <Def term={t("settings.restEndpoint")}>
                <Mono>{`${base}/v1`}</Mono>
              </Def>
            </Defs>
            <div className="mt">
              <Code>
                {`ccd --token "$CC_TOKEN" context --project ${namespace}/${slug}`}
                <br />
                {`curl -H "Authorization: Bearer $CC_TOKEN" \\`}
                <br />
                {`  ${base}/v1/projects/${namespace}/${slug}/context`}
              </Code>
            </div>
          </Panel>

          <Panel title={t("settings.encryption")}>
            <p className="sec">{t("settings.encryptionBody")}</p>
          </Panel>

          <ExportPanel />
        </Stack>

        <Stack top>
          <Panel title={t("settings.identity")}>
            <Defs>
              <Def term={t("project.path")}>
                <Mono>{`${namespace}/${slug}`}</Mono>
              </Def>
              <Def term={t("project.uuid")}>
                <Copyable value={project.id} />
              </Def>
              <Def term={t("settings.visibility")}>{project.visibility}</Def>
            </Defs>
          </Panel>

          <DeletePanel />

          <Panel title={t("settings.danger")}>
            <Note tone="info">{t("settings.noWrites")}</Note>
          </Panel>
        </Stack>
      </Grid>
    </>
  );
}

/**
 * Downloading the project as an archive (docs/adr/0035). Shown to every
 * member: the server decides who may, and a refusal says so here. Fetched
 * rather than linked, because a cookie-authenticated request needs the CSRF
 * header a plain link cannot send.
 */
function ExportPanel() {
  const t = useT();
  const { namespace, slug } = useProject();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function download() {
    setBusy(true);
    setFailure(undefined);
    try {
      const { blob, filename } = await api.exportProject(namespace, slug);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = filename;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel title={t("settings.export")}>
      <p className="sec lead-out">{t("settings.exportLead")}</p>
      <Btn onClick={() => void download()} disabled={busy}>
        {busy ? t("settings.exporting") : t("settings.exportButton")}
      </Btn>
      {failure !== undefined && (
        <div className="mt">
          <ErrorState error={failure} />
        </div>
      )}
    </Panel>
  );
}

/** Days a scheduled deletion can be cancelled (retention.DefaultGrace). */
const GRACE_DAYS = 7;

/**
 * Scheduling the project for deletion (docs/adr/0036). The path is typed
 * back as the confirmation, and the server checks it again: a button alone
 * is one stray click from hiding a team's project.
 */
function DeletePanel() {
  const t = useT();
  const navigate = useNavigate();
  const { namespace, slug } = useProject();
  const path = `${namespace}/${slug}`;
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function remove() {
    setBusy(true);
    setFailure(undefined);
    try {
      await api.deleteProject(namespace, slug, typed.trim());
      navigate("/");
    } catch (err) {
      setFailure(err);
      setBusy(false);
    }
  }

  return (
    <Panel title={t("settings.delete")}>
      <p className="sec lead-out">{t("settings.deleteLead", { days: GRACE_DAYS })}</p>
      <div className="stack-sm">
        <TextField
          label={t("settings.deleteConfirm", { path })}
          placeholder={path}
          value={typed}
          onChange={setTyped}
          labelled
        />
        <div>
          <Btn kind="danger" onClick={() => void remove()} disabled={busy || typed.trim() !== path}>
            {t("settings.deleteButton")}
          </Btn>
        </div>
      </div>
      {failure !== undefined && (
        <div className="mt">
          <ErrorState error={failure} />
        </div>
      )}
    </Panel>
  );
}
