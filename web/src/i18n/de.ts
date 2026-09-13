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
  "auth.sameToken":
    "Dasselbe Token, das auch eine Kommandozeile oder ein MCP-Client verwendet. Eine eigene Web-Sitzung gibt es nicht.",
  "auth.indistinct":
    "Ein widerrufenes Token und eines, das es nie gab, lesen sich hier gleich. Zu sagen, welches von beidem vorliegt, würde jemandem mit einem gestohlenen Token verraten, dass die Hälfte seiner Vermutung stimmte.",

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
  "dashboard.noneWaitingBody":
    "Keine offenen Übergaben, keine Ablehnungen, die einen Blick lohnen.",
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

  "members.addHelp":
    "Über die Principal-UUID. Kein Endpunkt listet die Principals einer Installation auf, es gibt also nichts zur Auswahl — kopieren Sie die UUID aus dem Konto der betreffenden Person.",
  "members.principalPlaceholder": "Principal-UUID",
  "members.noRemoval":
    "Mitglieder zu entfernen und Rollen zu ändern ist nicht gebaut: die API hat dafür keinen Schreibpfad, also bietet dieser Bildschirm auch keinen an.",

  "orgs.title": "Organisationen",
  "orgs.lead":
    "Die Ebene über Ihren Projekten. In einer Organisation wird ein Skill oder ein Gate einmal vorgeschrieben statt in jedem Projekt einzeln.",
  "orgs.column.name": "Organisation",
  "orgs.column.role": "Ihre Rolle",
  "orgs.column.members": "Mitglieder",
  "orgs.empty": "Sie gehören zu keiner Organisation",
  "orgs.emptyBody":
    "Die Mitgliedschaft hängt am Benutzerkonto, ein Dienst-Principal hat deshalb nie eine — das ist für ihn die richtige Antwort und keine Ablehnung.",
  "orgs.members": "Mitglieder",
  "orgs.membersLead":
    "Die Mitgliedschaft gilt für Benutzerkonten, nicht für Principals: eine Organisationsrolle betrifft einen Menschen, während ein Dienst-Principal innerhalb eines Projekts handelt.",
  "orgs.audit": "Aktivität",
  "orgs.column.user": "Benutzerkonto",
  "orgs.select": "Organisation auswählen",
  "orgs.selectBody": "Wählen Sie links eine aus, um ihre Mitglieder und ihre Aktivität zu sehen.",

  "admin.title": "Verwaltung",
  "admin.lead":
    "Die Installation selbst: wer sie verwaltet, wer abgelehnt wurde und die Identitäten, an denen alles andere hängt.",
  "admin.tab.overview": "Überblick",
  "admin.tab.identities": "Identitäten",
  "admin.tab.security": "Sicherheitsprotokoll",
  "admin.admins": "Administration der Installation",
  "admin.adminsLead":
    "Wer die Installation administriert, kann projektübergreifend lesen. Das Erteilen und Entziehen dieser Berechtigung gibt es hier bewusst nicht — es ist eine Operation der Kommandozeile.",
  "admin.column.user": "Benutzerkonto",
  "admin.column.role": "Rolle",
  "admin.column.granted": "Erteilt",
  "admin.column.grantedBy": "Erteilt von",
  "admin.createUser": "Benutzerkonto anlegen",
  "admin.createUserLead":
    "Eine Person, die ein Token halten und zu Projekten hinzugefügt werden kann.",
  "admin.email": "E-Mail",
  "admin.displayName": "Anzeigename",
  "admin.createService": "Dienst-Principal anlegen",
  "admin.createServiceLead":
    "Eine nicht-menschliche Identität — ein CI-Lauf, ein geplanter Agent. Sie handelt in Projekten und erscheint im Audit wie alle anderen.",
  "admin.name": "Name",
  "admin.description": "Beschreibung",
  "admin.created": "Angelegt: {name}",
  "admin.credentials": "Zugangsmerkmale",
  "admin.credentialsLead":
    "Die Token eines Principals. Fügen Sie eine Principal-UUID ein, um sie nachzuschlagen.",
  "admin.lookup": "Nachschlagen",
  "admin.security": "Sicherheitsprotokoll",
  "admin.identitiesLead":
    "Hier entstehen die Identitäten, an denen alles andere hängt. Kein Endpunkt der API listet Benutzerkonten oder Dienst-Principals auf, diese Seite legt sie also an und zeigt, was gerade angelegt wurde — ein Verzeichnis kann sie nicht zeigen.",
  "admin.securityLead":
    "Was der Server abgelehnt hat: ein Token, das sich nicht auflösen ließ, eine verweigerte Berechtigung, eine gedrosselte Anfrage. Bewusst getrennt vom Projekt-Audit — eine Ablehnung betrifft oft ein Projekt, das die anfragende Seite gar nicht sehen kann, und das eigentlich interessante Muster, ein Zugangsmerkmal, das mehrere Projekte abklopft, ist aus jedem einzelnen Projekt heraus unsichtbar.",
  "admin.securityNote":
    "Eine erfolgreiche Anmeldung ist keine Ablehnung und wird hier nicht aufgezeichnet. Wann ein Zugangsmerkmal zuletzt etwas getan hat, steht am Token selbst als Zeitpunkt der letzten Verwendung.",
  "admin.column.kind": "Art",
  "admin.column.reason": "Grund",
  "admin.column.capability": "Berechtigung",
  "admin.column.credential": "Zugangsmerkmal",
  "admin.securityEmpty": "Nichts aufgezeichnet",
  "admin.securityEmptyBody":
    "Keine Ablehnungen, keine unbekannten Zugangsmerkmale, nichts anzusehen. Das ist der gute Fall.",

  "tokens.title": "Zugriffstoken",
  "tokens.lead":
    "Die Zugangsmerkmale, die Sie aus einer Kommandozeile, einem Editor oder einem MCP-Client verwenden. Jedes gehört Ihnen allein — ein Token für jemand anderen lässt sich hier nicht ausstellen.",
  "tokens.column.type": "Client",
  "tokens.column.created": "Erstellt",
  "tokens.column.lastUsed": "Zuletzt verwendet",
  "tokens.column.expires": "Läuft ab",
  "tokens.issue": "Token ausstellen",
  "tokens.clientType": "Art des Clients",
  "tokens.expiry": "Gültig für",
  "tokens.expiry.never": "Unbegrenzt",
  "tokens.expiry.30d": "30 Tage",
  "tokens.expiry.90d": "90 Tage",
  "tokens.expiry.1y": "1 Jahr",
  "tokens.revoke": "Widerrufen",
  "tokens.revoked": "Widerrufen",
  "tokens.expired": "Abgelaufen",
  "tokens.active": "Aktiv",
  "tokens.empty": "Noch keine Token",
  "tokens.emptyBody":
    "Stellen Sie eines aus, um einen Agenten mit dieser Installation zu verbinden.",
  "tokens.justIssued": "Kopieren Sie dieses Token jetzt",
  "tokens.justIssuedBody":
    "Gespeichert wird nur sein Hash, dies ist also das einzige Mal, dass es angezeigt werden kann. Dieses Feld zu schließen widerruft es nicht — es bedeutet nur, dass es nie wieder lesbar ist.",
  "tokens.confirmRevoke":
    "Dieses Zugangsmerkmal widerrufen? Alles, was es noch verwendet, hört sofort auf zu funktionieren.",

  "artifacts.title": "Artefakte",
  "artifacts.lead":
    "Der gemeinsame Speicher, aus dem Agenten lesen und in den sie schreiben. Jeder Schreibvorgang legt eine neue Version an; nichts wird an Ort und Stelle überschrieben, ein schlechter Schreibvorgang lässt sich also überlesen statt aus einem Backup zurückholen.",
  "artifacts.column.path": "Pfad",
  "artifacts.column.type": "Inhaltstyp",
  "artifacts.column.version": "Version",
  "artifacts.column.size": "Größe",
  "artifacts.column.hash": "Prüfsumme des Inhalts",
  "artifacts.empty": "Noch keine Artefakte",
  "artifacts.emptyBody":
    "Das erste entsteht, sobald ein Agent über die API, die Kommandozeile oder MCP schreibt.",
  "artifacts.history": "Versionen",
  "artifacts.content": "Inhalt",
  "artifacts.select": "Artefakt auswählen",
  "artifacts.selectBody": "Wählen Sie links einen Pfad, um Versionen und Inhalt zu sehen.",
  "artifacts.binary": "Das ist kein Text",
  "artifacts.binaryBody":
    "Laut Inhaltstyp ist das kein Text und wird deshalb nicht dargestellt. Version, Größe und Prüfsumme oben sind das, was dieser Bildschirm darüber sagen kann.",
  "artifacts.viewing": "Version {version} wird angezeigt",
  "artifacts.latest": "Neueste",
  "artifacts.noWrite":
    "Artefakte von hier aus zu schreiben ist nicht gebaut. Agenten schreiben über die API, die Kommandozeile und MCP, und ein Editor, der einen gleichzeitigen Schreibvorgang unbemerkt verlieren kann, ist schlechter als gar keiner.",

  "secrets.title": "Geheimnisse",
  "secrets.lead":
    "Nur Namen und Verweise. Ein Wert geht einmal hinein und kommt über diese Oberfläche nie wieder heraus — ein Projekt erhält einen Verweis, den es einem Gate übergibt.",
  "secrets.column.name": "Name",
  "secrets.column.reference": "Verweis",
  "secrets.add": "Geheimnis hinzufügen",
  "secrets.value": "Wert",
  "secrets.delete": "Löschen",
  "secrets.empty": "Keine Geheimnisse in diesem Projekt",
  "secrets.emptyBody": "Fügen Sie hier eines hinzu, oder über die Kommandozeile.",
  "secrets.neverShown":
    "Der Wert wird einmal geschrieben und ist danach für niemanden und über keinen Adapter wieder lesbar. Löschen und neu anlegen ist der einzige Weg, ihn zu ändern.",
  "secrets.confirmDelete":
    "Das Geheimnis {name} löschen? Alles, was seinen Verweis verwendet, beginnt zu scheitern.",

  "gates.title": "Richtlinien",
  "gates.lead":
    "Die an dieses Projekt gebundenen Gates und die, die es erbt. Ein Gate läuft an einem Hook und kann die Operation verweigern — die Ablehnung landet auf der Zeitleiste der Aktivität.",
  "gates.column.gate": "Gate",
  "gates.column.hook": "Hook",
  "gates.column.scope": "Gebunden an",
  "gates.column.enabled": "Zustand",
  "gates.enabled": "Durchgesetzt",
  "gates.disabled": "Aus",
  "gates.empty": "Hier greift kein Gate",
  "gates.emptyBody": "In diesem Projekt greift nichts in Operationen ein.",
  "gates.config": "Konfiguration",

  "events.title": "Ereignisse",
  "events.lead":
    "Der Strom aus der Outbox. Ein Ereignis ist das, was andere Systeme konsumieren; der Audit-Eintrag daneben ist das, was ein Mensch liest.",
  "events.column.seq": "Nr.",
  "events.column.type": "Typ",
  "events.column.resource": "Ressource",
  "events.column.payload": "Nutzlast",
  "events.empty": "Noch keine Ereignisse",

  "skill.versions": "Versionen",
  "skill.versionsLead":
    "Jede jemals unter diesem Namen veröffentlichte Version. Eine veröffentlichte Version ist unveränderlich — erneutes Veröffentlichen fügt hier eine Zeile hinzu und bearbeitet nie eine.",
  "skill.content": "Inhalt",
  "skill.contentLead": "Was ein Agent bekommt, wenn diese Version aufgelöst wird.",
  "skill.provenance": "Herkunft",
  "skill.checksum": "Prüfsumme",
  "skill.capabilities": "Deklarierte Berechtigungen",
  "skill.author": "Urheberin oder Urheber",
  "skill.origin": "Herkunftsort",
  "skill.promote": "Hochstufen",
  "skill.promoteLead":
    "Den Skill auf eine breitere Ebene heben, damit mehr Projekte ihn sehen. Nur in eine Richtung: ihn wieder zu verengen würde ihn unter Bindungen wegziehen, die bei ihrer Entstehung berechtigt waren.",
  "skill.promoteTo": "Verschieben nach",
  "skill.promoted": "Liegt jetzt auf der Ebene {scope}",
  "skill.install": "Installieren",
  "skill.installed": "Installiert",
  "skill.installLead":
    "Eine Version eines Skills an dieses Projekt binden. Die Bindung nennt genau eine Version — es gibt keinen Auflöser für Versionsbereiche, es ändert sich also nichts unter Ihnen.",
  "skill.notFound": "Diesen Skill gibt es nicht",
  "skill.notFoundBody":
    "Er wurde vielleicht an einer Stelle veröffentlicht, die Sie nicht sehen können.",
  "skill.backToRegistry": "Zurück zur Registry",
  "skill.pin": "Festsetzen",
  "skill.unpin": "Lösen",
  "skill.pinned": "Festgesetzt",

  "handoff.createLead":
    "Was die nächste Person oder der nächste Agent zum Weiterarbeiten braucht. Hier von einem Menschen geschrieben; Agenten schreiben dasselbe über MCP.",
  "handoff.summaryHelp": "Eine Zeile. Worum geht es bei dieser Arbeit.",
  "handoff.completedHelp": "Was bereits erledigt ist, damit es niemand zweimal tut.",
  "handoff.nextHelp": "Was als Nächstes zu tun ist, konkret genug zum Anfangen.",
  "handoff.created": "Übergabe angelegt",
  "handoff.confirmClose":
    "Diese Übergabe abschließen? Sie ist dann nicht mehr die aktuelle und niemand kann sie übernehmen.",
  "handoff.openExists": "Hier ist bereits eine Übergabe offen",
  "handoff.openExistsBody":
    "Schließen Sie sie, bevor Sie eine weitere hinterlassen — zwei offene Übergaben machen die Frage „was soll ich aufgreifen“ mehrdeutig, und genau die sollen sie beantworten.",

  "projects.create": "Neues Projekt",
  "projects.createLead":
    "Ein Pfad und ein Name. Der Pfad ist, wie Menschen darauf verweisen, und lässt sich später nicht ändern; Sie werden seine Eigentümerin oder sein Eigentümer.",
  "projects.path": "Pfad",
  "projects.pathHelp": "namensraum/kurzname",
  "projects.name": "Anzeigename",
  "projects.created": "{path} angelegt",
};
