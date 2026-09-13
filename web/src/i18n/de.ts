import type { MessageKey } from "./en";

/**
 * German.
 *
 * Translated from en.ts and from nothing else. `npm run i18n:check` fails on
 * a key that no longer exists upstream, on a dropped or invented
 * {placeholder}, and on a plural without an `other` form.
 *
 * German runs roughly 30% longer than English and these screens are dense —
 * if something below breaks a layout, the component is too rigid, not the
 * translation too long. See .claude/skills/ui-design.
 */
export const de: Partial<Record<MessageKey, string>> = {
  "app.name": "Context Commander",
  "app.skipToContent": "Zum Inhalt springen",

  "nav.dashboard": "Übersicht",
  "nav.projects": "Projekte",
  "nav.organizations": "Organisationen",
  "nav.registry": "Skill-Registry",
  "nav.administration": "Verwaltung",
  "nav.workspace": "Arbeitsbereich",
  "nav.installation": "Installation",
  "nav.preferences": "Einstellungen",
  "nav.search": "Suchen",
  "nav.signOut": "Abmelden",

  "theme.dark": "Dunkel",
  "theme.light": "Hell",
  "theme.label": "Erscheinungsbild",

  "auth.title": "Anmelden",
  "auth.lead": "Fügen Sie ein persönliches Zugriffstoken ein. Erstellen Sie eines mit {command}.",
  "auth.token": "Zugriffstoken",
  "auth.submit": "Anmelden",
  "auth.failed": "Dieses Token wurde nicht akzeptiert.",
  "auth.signedInAs": "Angemeldet als {name}",

  "common.loading": "Wird geladen…",
  "common.retry": "Erneut versuchen",
  "common.cancel": "Abbrechen",
  "common.save": "Speichern",
  "common.close": "Schließen",
  "common.loadMore": "Mehr laden",
  "common.perPage": "{count} pro Seite",
  "common.showingMore": "{count} angezeigt · weitere verfügbar",
  "common.showingAll": "{count} angezeigt · vollständig",
  "common.endOfList": "Ende der Liste",
  "common.none": "Keine",
  "common.never": "Nie",
  "common.unknown": "Unbekannt",
  "common.you": "Sie",

  "error.forbidden.title": "Sie haben darauf keinen Zugriff",
  "error.forbidden.body":
    "Ihre Rolle in diesem Projekt umfasst nicht die Berechtigung, die dieser Bildschirm benötigt. Fragen Sie eine Eigentümerin oder einen Administrator, falls Sie sie haben sollten.",
  "error.notFound.title": "Nicht gefunden",
  "error.notFound.body": "Das existiert entweder nicht oder ist für Sie nicht sichtbar.",
  "error.conflict.title": "Das wurde geändert, während Sie es gelesen haben",
  "error.conflict.body":
    "Seit Sie diese Fassung geladen haben, hat jemand eine neuere geschrieben. Lesen Sie die neueste Fassung, führen Sie Ihre Änderung darin zusammen und versuchen Sie es erneut — Überschreiben würde die Arbeit der anderen Person löschen.",
  "error.unavailable.title": "Der Server antwortet nicht",
  "error.unavailable.body": "Es wurde nichts geändert. Versuchen Sie es gleich noch einmal.",
  "error.rateLimited.title": "Zu viele Anfragen",
  "error.rateLimited.body":
    "Dieses Zugangsmerkmal stellt Anfragen zu schnell. Warten Sie einen Moment und versuchen Sie es erneut.",
  "error.generic.title": "Etwas ist schiefgelaufen",

  "empty.noResults": "Dazu passt nichts",
  "empty.noResultsBody": "Versuchen Sie eine andere Suche oder setzen Sie die Filter zurück.",

  "dashboard.title": "Übersicht",
  "dashboard.lead":
    "Was sich geändert hat, woran Sie gearbeitet haben und ob etwas auf Sie wartet.",
  "dashboard.waiting": "Wartet auf Sie",
  "dashboard.recent": "Letzte Aktivität",
  "dashboard.yourProjects": "Ihre Projekte",
  "dashboard.clients": "Verbundene Clients",
  "dashboard.openHandoff": "Offene Übergabe",
  "dashboard.noneWaiting": "Nichts wartet auf Sie",
  "dashboard.noneWaitingBody": "Keine offenen Übergaben, keine Ablehnungen, die einen Blick lohnen.",
  "dashboard.noProjects": "Sie sind noch in keinem Projekt",
  "dashboard.activityHere": "Aktivität erscheint hier, sobald es ein Projekt zu beobachten gibt.",

  "projects.title": "Projekte",
  "projects.lead":
    "Alle Projekte, in denen Sie handeln können. Der Pfad ist, wie Menschen darüber sprechen; die UUID ist, was das System meint.",
  "projects.filter": "Projekte filtern…",
  "projects.column.project": "Projekt",
  "projects.column.namespace": "Namensraum",
  "projects.column.role": "Ihre Rolle",
  "projects.column.members": "Mitglieder",
  "projects.column.activity": "Letzte Aktivität",
  "projects.count": "{count, plural, one {# Projekt} other {# Projekte}}",
  "projects.empty": "Sie sind in keinem Projekt Mitglied",
  "projects.emptyBody": "Ein Projekt erscheint hier, sobald jemand Sie hinzufügt.",

  "project.overview": "Überblick",
  "project.members": "Mitglieder",
  "project.skills": "Skills",
  "project.policies": "Richtlinien",
  "project.secrets": "Geheimnisse",
  "project.activity": "Aktivität",
  "project.settings": "Einstellungen",
  "project.identity": "Identität",
  "project.path": "Pfad",
  "project.uuid": "UUID",
  "project.yourRole": "Sie sind",
  "project.created": "Erstellt",
  "project.tab.overview": "Überblick",
  "project.tab.members": "Mitglieder",
  "project.tab.skills": "Skills",
  "project.tab.activity": "Aktivität",
  "project.notBuilt.title": "Noch nicht gebaut",
  "project.notBuilt.artifacts":
    "Für Artefakte, Notizen, Richtlinien und Geheimnisse gibt es Entwürfe, aber in der API noch keinen Lesepfad.",

  "handoff.current": "Aktuelle Übergabe",
  "handoff.summary": "Zusammenfassung",
  "handoff.completed": "Erledigte Arbeit",
  "handoff.next": "Nächste Schritte",
  "handoff.references": "Verweise",
  "handoff.claim": "Übernehmen",
  "handoff.close": "Übergabe abschließen",
  "handoff.create": "Übergabe anlegen",
  "handoff.status.open": "Offen",
  "handoff.status.claimed": "Übernommen",
  "handoff.status.closed": "Abgeschlossen",
  "handoff.claimedBy": "Übernommen von {name}",
  "handoff.claimedByYou": "Von Ihnen übernommen",
  "handoff.none": "Keine Übergabe ist offen",
  "handoff.noneBody": "Hier wartet nichts darauf, übernommen zu werden.",
  "handoff.alreadyClaimed": "Ein anderer Client war zuerst da",
  "handoff.alreadyClaimedBody":
    "Das ist die Antwort der Koordination, kein Fehler in Ihrer Anfrage. Lesen Sie sie erneut und entscheiden Sie, ob die Arbeit noch Ihre ist.",
  "handoff.history": "Frühere Übergaben",
  "handoff.column.created": "Erstellt",
  "handoff.column.status": "Status",
  "handoff.column.summary": "Zusammenfassung",

  "members.title": "Mitglieder",
  "members.lead":
    "Wer in diesem Projekt handeln darf und in welcher Rolle. Ein Client-Zugangsmerkmal ist kein Mitglied — das Principal dahinter ist es.",
  "members.column.principal": "Principal",
  "members.column.type": "Typ",
  "members.column.role": "Rolle",
  "members.column.added": "Hinzugefügt",
  "members.type.user": "Benutzerin oder Benutzer",
  "members.type.service": "Dienst-Principal",
  "members.add": "Principal hinzufügen",
  "members.count": "{count, plural, one {# Mitglied} other {# Mitglieder}}",

  "skills.title": "Skills",
  "skills.lead":
    "Die wirksame Menge: Bindungen aus Installation, Organisation und Projekt zusammengesetzt, die engste gewinnt je Skill. Jede Überschreibung ist sichtbar.",
  "skills.column.skill": "Skill",
  "skills.column.livesAt": "Liegt in",
  "skills.column.version": "Aufgelöst",
  "skills.column.trust": "Vertrauen",
  "skills.column.state": "Zustand",
  "skills.browse": "Registry durchsuchen",
  "skills.install": "Skill installieren",
  "skills.empty": "Hier gelten keine Skills",
  "skills.emptyBody":
    "Installieren Sie einen aus der Registry oder veröffentlichen Sie einen aus diesem Projekt.",

  "registry.title": "Skill-Registry",
  "registry.lead":
    "Alles, was Sie sehen können: Skills in Ihren Projekten, in Ihren Organisationen und im installationsweiten Bestand. Agenten durchsuchen denselben Katalog.",
  "registry.search": "Skills nach Namen suchen…",
  "registry.column.latest": "Neueste",
  "registry.column.checksum": "Prüfsumme",
  "registry.column.author": "Urheberin oder Urheber",
  "registry.publish": "Neuer Skill",
  "registry.results": "{count, plural, one {# Treffer} other {# Treffer}} für „{query}“",
  "registry.livesAt": "Liegt in",
  "registry.empty": "Der Katalog ist leer",
  "registry.emptyBody":
    "Veröffentlichen Sie einen Skill aus einem Projekt oder heben Sie einen in den installationsweiten Bestand.",

  "scope.installation": "Installation",
  "scope.organization": "Organisation",
  "scope.project": "Projekt",

  "trust.signed": "Signiert",
  "trust.unverified": "Ungeprüft",

  "role.OWNER": "Eigentum",
  "role.ADMIN": "Administration",
  "role.CONTRIBUTOR": "Mitarbeit",
  "role.READER": "Lesezugriff",

  "activity.title": "Aktivität",
  "activity.lead":
    "Ereignisse und Audit auf einer Zeitleiste. Sie beantworten dieselbe Frage — was ist hier passiert — und wer sie trennt, zwingt die Lesenden, sie wieder zusammenzusetzen.",
  "activity.column.time": "Zeit",
  "activity.column.action": "Aktion",
  "activity.column.resource": "Ressource",
  "activity.column.principal": "Principal",
  "activity.column.client": "Client",
  "activity.column.result": "Ergebnis",
  "activity.result.allow": "Erlaubt",
  "activity.result.deny": "Abgelehnt",
  "activity.result.error": "Fehler",
  "activity.empty": "Hier ist noch nichts passiert",
  "activity.emptyBody":
    "Lese- und Schreibzugriffe über API, CLI und MCP landen alle auf dieser Zeitleiste.",

  "prefs.title": "Einstellungen",
  "prefs.lead":
    "Wie diese Installation mit Ihnen spricht. Diese Angaben gehören zu Ihrem Konto, nicht zu diesem Browser.",
  "prefs.language": "Sprache der Oberfläche",
  "prefs.languageHelp":
    "Alles, was das Produkt schreibt. Inhalte, die andere geschrieben haben, werden nie übersetzt.",
  "prefs.dateFormat": "Datumsformat",
  "prefs.timeFormat": "Zeitformat",
  "prefs.timeZone": "Zeitzone",
  "prefs.showUTC": "UTC zusätzlich anzeigen",
  "prefs.numberFormat": "Zahlenformat",
  "prefs.storageUnits": "Speichereinheiten",
  "prefs.units.decimal": "GB · dezimal, Basis 1000",
  "prefs.units.binary": "GiB · binär, Basis 1024",
  "prefs.notStored":
    "Einstellungen werden noch nicht auf dem Server gespeichert, sie liegen vorerst in diesem Browser.",
  "prefs.section.language": "Sprache",
  "prefs.section.dates": "Datum und Zeit",
  "prefs.section.numbers": "Zahlen und Einheiten",
  "prefs.dateFormatHelp":
    "Wie ein Kalenderdatum geschrieben wird. 03-09-2026 bezeichnet je nach Lesart zwei verschiedene Tage.",
  "prefs.timeFormatHelp": "Konvention der Uhrzeit.",
  "prefs.timeZoneHelp": "Zeitstempel werden in UTC gespeichert und in dieser Zone angezeigt.",
  "prefs.showUTCHelp": "Für Audit- und Sicherheitsbildschirme, wo ein gemeinsamer Bezug zählt.",
  "prefs.storageUnitsHelp":
    "Wie Byte-Angaben geschrieben werden. Der gespeicherte Wert ändert sich nie — nur seine Lesart.",
  "prefs.date.locale": "Der Sprache folgen",
  "prefs.date.ymd": "JJJJ-MM-TT",
  "prefs.date.dmy": "TT-MM-JJJJ",
  "prefs.date.mdy": "MM-TT-JJJJ",
  "prefs.time.locale": "Der Sprache folgen",
  "prefs.time.h24": "24 Stunden",
  "prefs.time.h12": "12 Stunden",
  "prefs.tz.device": "Diesem Gerät folgen ({zone})",
  "prefs.on": "An",
  "prefs.off": "Aus",
  "prefs.serverFormat":
    "Der Server sendet nie eine formatierte Zeichenkette. Er sendet den Wert; die Oberfläche liest ihn.",
};
