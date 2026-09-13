import { Link, useLocation } from "react-router-dom";
import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { Code, EmptyState, PageHead, Panel } from "../ui";

/**
 * An address that matches nothing.
 *
 * It used to redirect to the dashboard, which is worse than it sounds: a
 * mistyped or stale link then looks like it worked, and the reader concludes
 * the thing they were linked to no longer exists. Saying "no such page" and
 * showing the address is the difference between a typo they can fix and a
 * mystery.
 */
export function NotFound() {
  const t = useT();
  const { pathname } = useLocation();
  useCrumbs("not-found", () => ({ path: [], current: t("error.notFoundPage.title") }));

  return (
    <>
      <PageHead title={t("error.notFoundPage.title")} />
      <Panel>
        <EmptyState title={t("error.notFoundPage.title")} body={t("error.notFoundPage.body")} />
        <div className="mt">
          <Code>{pathname}</Code>
        </div>
        <div className="mt">
          <Link className="btn" to="/">
            {t("error.backToDashboard")}
          </Link>
        </div>
      </Panel>
    </>
  );
}
