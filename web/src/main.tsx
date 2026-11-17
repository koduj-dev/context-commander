import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { App } from "./App";
import { I18nProvider } from "./i18n";
import { AuthProvider } from "./app/auth";
import { ErrorBoundary } from "./app/ErrorBoundary";
import { CrumbsProvider } from "./app/crumbs";
import { PrefsProvider } from "./app/prefs";
import { ThemeProvider } from "./app/theme";
// Inter, bundled rather than fetched from Google Fonts: a self-hosted
// product should not tell a third party who opened it (CRA Annex I (2)(g)),
// and the Content-Security-Policy then allows fonts from this origin only.
// SIL Open Font License 1.1.
import "@fontsource/inter/400.css";
import "@fontsource/inter/500.css";
import "@fontsource/inter/600.css";
import "./styles/app.css";

const root = document.getElementById("root");
if (!root) throw new Error("#root is missing from index.html");

createRoot(root).render(
  <StrictMode>
    <I18nProvider>
      <ErrorBoundary>
        <PrefsProvider>
          <ThemeProvider>
            <AuthProvider>
              <BrowserRouter>
                <CrumbsProvider>
                  <App />
                </CrumbsProvider>
              </BrowserRouter>
            </AuthProvider>
          </ThemeProvider>
        </PrefsProvider>
      </ErrorBoundary>
    </I18nProvider>
  </StrictMode>,
);
