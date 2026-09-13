import { Component, type ErrorInfo, type ReactNode } from "react";
import { useT } from "../i18n";
import { Btn, Code, Note, Panel } from "../ui";

/**
 * The last line of defence.
 *
 * Without one of these, a single bad render — a field that turned out to be
 * optional, a list that came back null — replaces the entire application with
 * a blank page and no explanation. That is the worst failure mode a UI has,
 * because it looks identical to the server being down and tells the reader
 * nothing they can act on.
 *
 * It says explicitly that nothing was changed on the server. A reader who has
 * just pressed Save and then sees a blank screen has no way to know whether
 * their write landed, and will often do it again.
 *
 * A class component because React has no hook for this: `componentDidCatch`
 * has no functional equivalent.
 */
type Props = { children: ReactNode };
type State = { error: Error | null };

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // The console is where a developer will look, and the component stack is
    // the part React does not put in the error itself.
    console.error("render failed", error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return <Crashed error={this.state.error} onReset={() => this.setState({ error: null })} />;
  }
}

/**
 * Rendered inside the provider tree, so it is translated — the reader who
 * needs this message is no more likely to read English than any other.
 */
function Crashed({ error, onReset }: { error: Error; onReset: () => void }) {
  const t = useT();
  return (
    <div className="solo">
      <main className="page">
        <div className="solo-card">
          <Panel title={t("error.crashed.title")}>
            <Note tone="danger">{t("error.crashed.body")}</Note>
            <div className="mt">
              <Code>{error.message}</Code>
            </div>
            <div className="mt row row-sm">
              <Btn
                kind="primary"
                onClick={() => {
                  // Clearing the error re-renders the tree that just failed.
                  // That works when the cause was transient and does nothing
                  // when it was not, which is why reload is offered too.
                  onReset();
                  window.location.reload();
                }}
              >
                {t("error.crashed.reload")}
              </Btn>
            </div>
          </Panel>
        </div>
      </main>
    </div>
  );
}
