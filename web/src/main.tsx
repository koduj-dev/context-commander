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
