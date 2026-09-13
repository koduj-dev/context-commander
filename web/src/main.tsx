import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { App } from "./App";
import { I18nProvider } from "./i18n";
import { AuthProvider } from "./app/auth";
import { CrumbsProvider } from "./app/crumbs";
import { PrefsProvider } from "./app/prefs";
import { ThemeProvider } from "./app/theme";
import "./styles/app.css";

const root = document.getElementById("root");
if (!root) throw new Error("#root is missing from index.html");

createRoot(root).render(
  <StrictMode>
    <I18nProvider>
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
    </I18nProvider>
  </StrictMode>,
);
