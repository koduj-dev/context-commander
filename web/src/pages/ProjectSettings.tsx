import { useT } from "../i18n";
import { Code, Copyable, Def, Defs, Grid, Mono, Note, PageHead, Panel, Stack } from "../ui";
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

          <Panel title={t("settings.danger")}>
            <Note tone="warn">{t("settings.noWrites")}</Note>
          </Panel>
        </Stack>
      </Grid>
    </>
  );
}
