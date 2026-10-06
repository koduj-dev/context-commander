import { LOCALES, LOCALE_NAMES, useI18n, useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { useAuth } from "../app/auth";
import { PasswordPanel, SessionsPanel } from "./AccountSecurity";
import { useFormat, usePrefs } from "../app/prefs";
import { principalName } from "../lib/api";
import * as fmt from "../lib/format";
import {
  Btn,
  Code,
  Def,
  Defs,
  Grid,
  Mono,
  Note,
  PageHead,
  Panel,
  Pref,
  Seg,
  SelectField,
  Stack,
} from "../ui";

/**
 * How this installation speaks to the reader.
 *
 * Every control shows what it currently produces. A dropdown reading
 * "DD-MM-YYYY" states a rule; a preview reading "13-09-2026" answers the
 * question the reader actually has — and shows, next to it, what they are
 * moving away from.
 *
 * The panel at the bottom says out loud that none of this is stored on the
 * server yet. It is true, and a screen that implied otherwise would lose
 * someone's settings the first time they opened the product elsewhere.
 */
export function Preferences() {
  const t = useT();
  useCrumbs("preferences", () => ({ path: [], current: t("prefs.title") }));
  const f = useFormat();
  const { locale, setLocale } = useI18n();
  const { prefs, setPrefs } = usePrefs();
  const { me, signOut } = useAuth();

  // A fixed instant, so the previews do not shuffle while someone reads them.
  const sample = "2026-09-13T12:05:22Z";
  const deviceZone = fmt.localTimeZone();
  const zones = Array.from(
    new Set(["", "UTC", deviceZone, "Europe/Prague", "Europe/Berlin", "America/New_York"]),
  );

  const withOrder = (order: fmt.DateOrder) =>
    fmt.date(sample, locale, order, prefs.timeZone || undefined);
  const withCycle = (cycle: fmt.HourCycle) =>
    fmt.time(sample, locale, cycle, prefs.timeZone || undefined);
  const withUnits = (units: fmt.UnitSystem) => fmt.bytes(2_400_000_000, locale, units);

  return (
    <>
      <PageHead
        title={t("prefs.title")}
        lead={t("prefs.lead")}
        actions={
          <Btn onClick={signOut} title={me?.email}>
            {t("nav.signOut")}
          </Btn>
        }
      />

      <Grid cols={2}>
        <Stack>
          <Panel title={t("prefs.section.language")} tight>
            <Pref
              title={t("prefs.language")}
              help={t("prefs.languageHelp")}
              preview={LOCALE_NAMES[locale]}
            >
              <SelectField
                label={t("prefs.language")}
                value={locale}
                onChange={setLocale}
                options={LOCALES.map((l) => ({ value: l, label: LOCALE_NAMES[l] }))}
              />
            </Pref>
          </Panel>

          <Panel title={t("prefs.section.numbers")} tight>
            <Pref
              title={t("prefs.storageUnits")}
              help={t("prefs.storageUnitsHelp")}
              preview={withUnits(prefs.units)}
              was={withUnits(prefs.units === "decimal" ? "binary" : "decimal")}
            >
              <SelectField
                label={t("prefs.storageUnits")}
                value={prefs.units}
                onChange={(units) => setPrefs({ units })}
                options={[
                  { value: "decimal", label: t("prefs.units.decimal") },
                  { value: "binary", label: t("prefs.units.binary") },
                ]}
              />
            </Pref>
            <Pref title={t("prefs.numberFormat")} preview={f.number(1284.5)}>
              {/* Follows the language, and only the language: a grouping
                  separator that disagrees with the surrounding sentence is a
                  worse answer than one the reader did not choose. */}
              <span className="dim">{LOCALE_NAMES[locale]}</span>
            </Pref>
          </Panel>

          <PasswordPanel />
        </Stack>

        <Stack>
          <Panel title={t("prefs.section.dates")} tight>
            <Pref
              title={t("prefs.dateFormat")}
              help={t("prefs.dateFormatHelp")}
              preview={withOrder(prefs.dateOrder)}
            >
              <SelectField
                label={t("prefs.dateFormat")}
                value={prefs.dateOrder}
                onChange={(dateOrder) => setPrefs({ dateOrder })}
                options={[
                  { value: "locale", label: `${t("prefs.date.locale")} · ${withOrder("locale")}` },
                  { value: "ymd", label: `${t("prefs.date.ymd")} · ${withOrder("ymd")}` },
                  { value: "dmy", label: `${t("prefs.date.dmy")} · ${withOrder("dmy")}` },
                  { value: "mdy", label: `${t("prefs.date.mdy")} · ${withOrder("mdy")}` },
                ]}
              />
            </Pref>

            <Pref
              title={t("prefs.timeFormat")}
              help={t("prefs.timeFormatHelp")}
              preview={withCycle(prefs.hourCycle)}
            >
              <SelectField
                label={t("prefs.timeFormat")}
                value={prefs.hourCycle}
                onChange={(hourCycle) => setPrefs({ hourCycle })}
                options={[
                  { value: "locale", label: `${t("prefs.time.locale")} · ${withCycle("locale")}` },
                  { value: "h24", label: `${t("prefs.time.h24")} · ${withCycle("h24")}` },
                  { value: "h12", label: `${t("prefs.time.h12")} · ${withCycle("h12")}` },
                ]}
              />
            </Pref>

            <Pref
              title={t("prefs.timeZone")}
              help={t("prefs.timeZoneHelp")}
              preview={f.stamp(sample)}
            >
              <SelectField
                label={t("prefs.timeZone")}
                value={prefs.timeZone}
                onChange={(timeZone) => setPrefs({ timeZone })}
                options={zones.map((z) => ({
                  value: z,
                  label: z === "" ? t("prefs.tz.device", { zone: deviceZone }) : z,
                }))}
              />
            </Pref>

            <Pref
              title={t("prefs.showUTC")}
              help={t("prefs.showUTCHelp")}
              preview={prefs.showUTC ? fmt.utcStamp(sample) : t("common.none")}
            >
              <Seg
                label={t("prefs.showUTC")}
                value={prefs.showUTC ? "on" : "off"}
                onChange={(v) => setPrefs({ showUTC: v === "on" })}
                options={[
                  { value: "off", label: t("prefs.off") },
                  { value: "on", label: t("prefs.on") },
                ]}
              />
            </Pref>
          </Panel>

          <Panel title={t("prefs.serverFormat")}>
            <Code>
              {'"created_at": "2026-09-13T12:05:22Z"'}
              <br />
              {'"size_bytes":  2400000000'}
            </Code>
            <div className="mt">
              <Defs>
                <Def term={t("auth.signedInAs", { name: "" })}>
                  <Mono>{principalName(me)}</Mono>
                </Def>
              </Defs>
            </div>
            <div className="mt">
              <Note tone="warn">{t("prefs.notStored")}</Note>
            </div>
          </Panel>

          <SessionsPanel />
        </Stack>
      </Grid>
    </>
  );
}
