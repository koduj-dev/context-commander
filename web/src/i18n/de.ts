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
  "nav.signOut": "Abmelden",

  "theme.dark": "Dunkel",
  "theme.light": "Hell",
  "theme.label": "Erscheinungsbild",

  "auth.title": "Anmelden",
  "auth.withPassword": "E-Mail und Passwort",
  "auth.withToken": "Zugriffstoken",
  "auth.email": "E-Mail",
  "auth.emailOrUsername": "E-Mail oder Benutzername",
  "auth.directoryUnavailable":
    "Das Verzeichnis war nicht erreichbar. Das liegt nicht an Ihrem Passwort — versuchen Sie es in Kürze erneut, oder sagen Sie es der Person, die diese Installation betreibt.",
  "auth.directoryNoEmail":
    "Ihr Verzeichniseintrag hat keine E-Mail-Adresse, daher kann dafür kein Konto angelegt werden. Eine Administration muss eine hinzufügen.",
  "auth.password": "Passwort",
  "auth.failed": "E-Mail oder Passwort ist falsch.",
  "auth.tokenLabel": "Zugriffstoken",
  "auth.tokenLead":
    "Ein persönliches Zugriffstoken aus {command}. Es wird gegen eine Sitzung getauscht und nicht in diesem Browser gespeichert.",
  "auth.tokenFailed": "Dieses Token wurde nicht akzeptiert.",
  "auth.rateLimited": "Zu viele Versuche. Warten Sie einen Moment und versuchen Sie es erneut.",
  "auth.withProvider": "Mit {provider} anmelden",
  "auth.oidcCancelled": "Die Anmeldung wurde beim Anmeldedienst abgebrochen.",
  "auth.oidcExpired":
    "Dieser Anmeldeversuch ist abgelaufen oder wurde in einem anderen Fenster gestartet. Beginnen Sie erneut.",
  "auth.oidcUnavailable":
    "Der Anmeldedienst war nicht erreichbar. Das liegt nicht an Ihrem Konto — versuchen Sie es in Kürze erneut, oder sagen Sie es der Person, die diese Installation betreibt.",
  "auth.oidcNoEmail":
    "Ihr Konto beim Anmeldedienst hat keine E-Mail-Adresse, daher kann dafür kein Konto angelegt werden. Die Administration des Anmeldedienstes muss eine hinzufügen.",
  "auth.oidcEmailUnverified":
    "Ihre E-Mail-Adresse beim Anmeldedienst ist nicht bestätigt und kann deshalb hier nicht zur Anmeldung dienen. Bestätigen Sie sie dort und versuchen Sie es erneut.",
  "auth.oidcFailed":
    "Die Anmeldung über den Anmeldedienst ist nicht gelungen. Beginnen Sie erneut.",
  "auth.oidcNoAccount":
    "Sie haben sich beim Anmeldedienst angemeldet, aber hier gibt es kein Konto für Sie, und diese Installation legt keine automatisch an. Bitten Sie die Administration, eines unter Ihrer E-Mail-Adresse anzulegen.",
  "auth.oidcDomainNotAllowed":
    "Ihre E-Mail-Adresse gehört zu keiner Domain, die diese Installation für die Anmeldung über diesen Anmeldedienst akzeptiert.",
  "auth.indistinct":
    "Ein falsches Passwort, eine Adresse ohne Konto und ein Konto ohne Passwort lesen sich hier gleich. Zu sagen, welcher Fall vorliegt, würde jedem, der rät, verraten, dass die Hälfte seiner Vermutung richtig war.",
  "setup.title": "Erstes Administratorkonto anlegen",
  "setup.lead":
    "Diese Installation hat noch keine Benutzer. Der Server hat beim Start einen einmaligen Einrichtungscode in sein Log geschrieben — fügen Sie ihn hier ein.",
  "setup.code": "Einrichtungscode",
  "setup.email": "E-Mail",
  "setup.name": "Anzeigename",
  "setup.password": "Passwort",
  "setup.passwordHint": "Mindestens {min} Zeichen. Die Länge ist die einzige Regel.",
  "setup.submit": "Administratorkonto anlegen",
  "setup.codeFailed":
    "Dieser Einrichtungscode ist nicht gültig. Der aktuelle steht im Log des Servers; ein Neustart gibt einen neuen aus.",
  "setup.alreadyDone": "Diese Installation hat bereits Benutzer. Melden Sie sich stattdessen an.",
  "setup.whereIsTheCode":
    "Der Code existiert nur im Speicher des laufenden Servers und nur solange es keine Benutzer gibt — er erreicht also kein Backup und lässt sich nicht aus der Datenbank lesen.",
  "account.password": "Passwort",
  "account.passwordLead":
    "Wird für die Anmeldung an dieser Oberfläche verwendet. Agenten und die Kommandozeile verwenden stattdessen Zugriffstoken, die separat verwaltet werden.",
  "account.currentPassword": "Aktuelles Passwort",
  "account.newPassword": "Neues Passwort",
  "account.confirmPassword": "Neues Passwort wiederholen",
  "account.setPassword": "Passwort festlegen",
  "account.changePassword": "Passwort ändern",
  "account.passwordSet": "Passwort geändert.",
  "account.passwordMismatch": "Die beiden neuen Passwörter stimmen nicht überein.",
  "account.currentWrong": "Das aktuelle Passwort ist nicht korrekt.",
  "account.noPasswordYet":
    "Dieses Konto hat noch kein Passwort. Sie sind mit einem Zugriffstoken angemeldet, und das genügt als Nachweis, um eines festzulegen.",
  "account.sessions": "Sitzungen",
  "account.sessionsLead":
    "Wo dieses Konto angemeldet ist. Eine Sitzung zu beenden macht ihr Cookie sofort ungültig, auf welchem Rechner es auch liegt.",
  "account.sessionCurrent": "Dieser Browser",
  "account.sessionStarted": "Begonnen",
  "account.sessionLastUsed": "Zuletzt verwendet",
  "account.sessionExpires": "Läuft ab",
  "account.endSession": "Sitzung beenden",
  "account.noSessions": "Keine aktiven Sitzungen.",
  "auth.submit": "Anmelden",
  "auth.signedInAs": "Angemeldet als {name}",

  "common.loading": "Wird geladen…",
  "common.retry": "Erneut versuchen",
  "common.cancel": "Abbrechen",
  "common.close": "Schließen",
  "common.loadMore": "Mehr laden",
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
  "dashboard.yourProjects": "Ihre Projekte",
  "dashboard.noneWaiting": "Nichts wartet auf Sie",
  "dashboard.noneWaitingBody":
    "Keine offenen Übergaben, keine Ablehnungen, die einen Blick lohnen.",
  "dashboard.noProjects": "Sie sind noch in keinem Projekt",

  "projects.title": "Projekte",
  "projects.lead":
    "Alle Projekte, in denen Sie handeln können. Der Pfad ist, wie Menschen darüber sprechen; die UUID ist, was das System meint.",
  "projects.filter": "Projekte filtern…",
  "projects.column.project": "Projekt",
  "projects.column.role": "Ihre Rolle",
  "projects.count": "{count, plural, one {# Projekt} other {# Projekte}}",
  "projects.empty": "Sie sind in keinem Projekt Mitglied",
  "projects.emptyBody": "Ein Projekt erscheint hier, sobald jemand Sie hinzufügt.",

  "project.identity": "Identität",
  "project.path": "Pfad",
  "project.uuid": "UUID",
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

  "members.title": "Mitglieder",
  "members.lead":
    "Wer in diesem Projekt handeln darf und in welcher Rolle. Ein Client-Zugangsmerkmal ist kein Mitglied — das Principal dahinter ist es.",
  "members.column.principal": "Principal",
  "members.column.role": "Rolle",
  "members.column.added": "Hinzugefügt",
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
    "Eine Projektrolle gehört einem Principal — einem Menschen oder einem Agenten. Beide stehen hier zur Auswahl, wenn Sie diese Installation administrieren; sonst nimmt das Feld die Principal-ID, die nicht dasselbe ist wie eine Benutzer-ID.",
  "members.principalPlaceholder": "Principal-UUID",
  "members.noRemoval":
    "Mitglieder zu entfernen und Rollen zu ändern ist nicht gebaut: die API hat dafür keinen Schreibpfad, also bietet dieser Bildschirm auch keinen an.",

  "orgs.title": "Organisationen",
  "orgs.lead":
    "Die Ebene über Ihren Projekten. In einer Organisation wird ein Skill oder ein Gate einmal vorgeschrieben statt in jedem Projekt einzeln.",
  "orgs.column.name": "Organisation",
  "orgs.column.role": "Ihre Rolle",
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
  "admin.tab.people": "Personen",
  "admin.tab.agents": "Agenten",
  "admin.column.registeredBy": "Registriert von",
  "admin.unattributed": "nicht erfasst",
  "admin.registeredByHelp":
    "Wer den Agenten angelegt hat. Es hält fest, woher der Agent kommt, und gewährt ihm nichts — ein Agent erreicht ein Projekt nur über seine eigene Mitgliedschaft darin.",
  "admin.tab.security": "Sicherheitsprotokoll",
  "admin.admins": "Administration der Installation",
  "admin.adminsLead":
    "Wer die Installation administriert, kann projektübergreifend lesen. Das Erteilen und Entziehen dieser Berechtigung gibt es hier bewusst nicht — es ist eine Operation der Kommandozeile.",
  "admin.column.user": "Benutzerkonto",
  "admin.column.role": "Rolle",
  "admin.column.granted": "Erteilt",
  "admin.createUser": "Benutzerkonto anlegen",
  "admin.initialPassword": "Anfangspasswort (optional)",
  "admin.initialPasswordLead":
    "Mit einem kann sich die Person sofort anmelden. Ohne braucht sie ein Token oder ein später gesetztes Passwort. Mindestens {min} Zeichen.",
  "admin.setPassword": "Passwort setzen",
  "admin.setPasswordFor": "Passwort für {name} setzen",
  "admin.passwordSetFor": "Passwort gesetzt. {name} kann sich damit jetzt anmelden.",
  "admin.setPasswordLead":
    "Das ist die Möglichkeit, sich als diese Person anzumelden — dasselbe wie ein Token für sie auszustellen, was dieser Bildschirm schon tut. Teilen Sie das Passwort auf einem anderen Weg mit; danach wird es nirgends mehr angezeigt.",
  "admin.createUserLead":
    "Eine Person, die ein Token halten und zu Projekten hinzugefügt werden kann.",
  "admin.email": "E-Mail",
  "admin.displayName": "Anzeigename",
  "admin.createService": "Dienst-Principal anlegen",
  "admin.createServiceLead":
    "Eine nicht-menschliche Identität — ein CI-Lauf, ein geplanter Agent. Sie handelt in Projekten und erscheint im Audit wie alle anderen.",
  "admin.name": "Name",
  "admin.description": "Beschreibung",
  "admin.security": "Sicherheitsprotokoll",
  "admin.securityLead":
    "Was der Server abgelehnt hat: ein Token, das sich nicht auflösen ließ, eine verweigerte Berechtigung, eine gedrosselte Anfrage. Bewusst getrennt vom Projekt-Audit — eine Ablehnung betrifft oft ein Projekt, das die anfragende Seite gar nicht sehen kann, und das eigentlich interessante Muster, ein Zugangsmerkmal, das mehrere Projekte abklopft, ist aus jedem einzelnen Projekt heraus unsichtbar.",
  "admin.securityNote":
    "Eine erfolgreiche Anmeldung ist keine Ablehnung und wird hier nicht aufgezeichnet. Wann ein Zugangsmerkmal zuletzt etwas getan hat, steht am Token selbst als Zeitpunkt der letzten Verwendung.",
  "admin.column.kind": "Art",
  "admin.column.reason": "Grund",
  "admin.column.capability": "Berechtigung",
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
  "artifacts.edit": "Bearbeiten",
  "artifacts.new": "Neues Artefakt",
  "artifacts.save": "Speichern",
  "artifacts.delete": "Löschen",
  "artifacts.confirmDelete":
    "{path} löschen? Die Versionen gehen mit, und alles, was diesen Pfad liest, beginnt zu scheitern.",
  "artifacts.editingFrom": "Bearbeitet ausgehend von Version {version}",
  "artifacts.editingNew": "Ein neues Artefakt an diesem Pfad",
  "artifacts.newPath": "Pfad",
  "artifacts.newPathHelp": "notizen/entscheidungen.md",
  "artifacts.contentType": "Inhaltstyp",
  "artifacts.staleWrite":
    "Während Sie bearbeitet haben, hat jemand eine neuere Version geschrieben",
  "artifacts.staleWriteBody":
    "Ihr Speichern wurde abgelehnt statt angewendet — anwenden hätte die Arbeit der anderen Person gelöscht. Laden Sie die neueste Version, tragen Sie Ihre Änderung dort ein und speichern Sie erneut.",
  "artifacts.reload": "Neueste Version laden",
  "artifacts.concurrency":
    "Ein Speichervorgang nennt die Version, von der er ausging. Hat jemand anderes dazwischen geschrieben, lehnt der Server ihn ab, statt die andere Person zu überschreiben.",
  "artifacts.binaryNoEdit":
    "Das ist kein Text und lässt sich hier nicht bearbeiten. Agenten schreiben es über die API, die Kommandozeile und MCP.",

  "secrets.title": "Geheimnisse",
  "secrets.lead":
    "Nur Namen und Verweise. Ein Wert geht einmal hinein und kommt über diese Oberfläche nie wieder heraus — ein Projekt erhält einen Verweis, den es einem Gate übergibt.",
  "secrets.column.name": "Name",
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
  "handoff.confirmClose":
    "Diese Übergabe abschließen? Sie ist dann nicht mehr die aktuelle und niemand kann sie übernehmen.",

  "projects.create": "Neues Projekt",
  "projects.createLead":
    "Ein Pfad und ein Name. Der Pfad ist, wie Menschen darauf verweisen, und lässt sich später nicht ändern; Sie werden seine Eigentümerin oder sein Eigentümer.",
  "projects.path": "Pfad",
  "projects.pathHelp": "namensraum/kurzname",
  "projects.name": "Anzeigename",
  "projects.created": "{path} angelegt",

  "context.title": "Kontext",
  "context.lead":
    "Genau das, was ein Agent bekommt, wenn er hier anfängt — dieselbe zusammengestellte Sicht, die MCP zurückgibt, nur für Menschen dargestellt. Wenn ein Agent sich seltsam verhält, ist das die erste Stelle zum Nachsehen.",
  "context.role": "Handelt als",
  "context.capabilities": "Darf",
  "context.assembled": "Zusammengestellt",
  "context.budget": "Budget",
  "context.budgetUsed": "{used} von {limit}",
  "context.budgetLead":
    "Ein Paket hat eine Größengrenze, und was nicht hineinpasst, wird benannt statt abgeschnitten. Ein Agent, der „es gibt nichts weiter“ nicht von „es gab mehr und Sie haben es nicht bekommen“ unterscheiden kann, handelt zuversichtlich auf einem halben Bild.",
  "context.truncated": "Weggelassen",
  "context.nothingTruncated": "Es wurde nichts weggelassen",
  "context.nothingTruncatedBody": "Alles, was dieses Projekt enthält, passte in das Budget.",
  "context.column.what": "Was",
  "context.column.ref": "Welches",
  "context.column.why": "Warum",
  "context.included": "Enthalten",
  "context.artifacts": "{count, plural, one {# Artefakt} other {# Artefakte}}",
  "context.skills": "{count, plural, one {# Skill} other {# Skills}}",
  "context.recentEvents": "Letzte Ereignisse",
  "context.noHandoff": "Keine Übergabe im Paket",
  "context.noHandoffBody":
    "Einem Agenten, der hier anfängt, wird gesagt, dass es nichts fortzusetzen gibt.",
  "context.budgetLimit": "Größengrenze",
  "context.apply": "Neu zusammenstellen",

  "skill.pinLead":
    "Eine festgesetzte Bindung ist eine, für die sich dieses Projekt entschieden hat und die nicht verschoben werden soll. Nur eine hier entstandene Bindung lässt sich festsetzen — eine geerbte gehört diesem Projekt nicht.",
  "skill.inherited": "Geerbt",

  "history.title": "Unterhaltungen",
  "history.lead":
    "Was Clients von ihren eigenen Sitzungen aufgezeichnet haben, damit eine Übergabe darauf zeigen kann, wo die Arbeit tatsächlich stattfand. Nichts davon schreibt diese Oberfläche — die Clients tragen es selbst ein.",
  "history.column.conversation": "Unterhaltung",
  "history.column.started": "Begonnen",
  "history.column.by": "Begonnen von",
  "history.column.type": "Typ",
  "history.column.recorded": "Aufgezeichnet",
  "history.objects": "Aufgezeichnete Objekte",
  "history.lineage": "Abstammung",
  "history.lineageLead":
    "Wie ein Objekt aus einem anderen hervorging — eine Zusammenfassung aus einem Mitschnitt, eine Verdichtung aus einer Zusammenfassung. Genau das verhindert, dass ein abgeleitetes Objekt für ein Original gehalten wird.",
  "history.column.from": "Abgeleitet aus",
  "history.column.to": "Ergab",
  "history.column.kind": "Beziehung",
  "history.select": "Unterhaltung auswählen",
  "history.selectBody": "Wählen Sie links eine aus, um zu sehen, was darin aufgezeichnet wurde.",
  "history.empty": "Keine Unterhaltungen aufgezeichnet",
  "history.emptyBody":
    "Ein Client zeichnet diese während der Arbeit auf. In dieses Projekt hat noch nichts eine Historie eingetragen.",
  "history.noObjects": "In dieser Unterhaltung wurde nichts aufgezeichnet",
  "history.noLineage": "Keine Ableitungen",
  "history.noLineageBody":
    "Jedes Objekt hier wurde direkt aufgezeichnet, keines ging aus einem anderen hervor.",
  "history.notText": "Kein Text",
  "history.notTextBody":
    "Ein Historienobjekt ist das, was der Client aufgezeichnet hat, und diese Bytes sind kein gültiges UTF-8. Prüfsumme und Typ oben sind das, was dieser Bildschirm darüber sagen kann.",

  "registry.publishLead":
    "Ein Skill ist Anweisungstext, den ein Agent bekommt. Veröffentlichen legt eine unveränderliche Version an — späteres Bearbeiten heißt erneut veröffentlichen, und beide Versionen bleiben bestehen.",
  "registry.publishName": "Name",
  "registry.publishAuthor": "Urheberin oder Urheber",
  "registry.publishOrigin": "Herkunftsort",
  "registry.publishScope": "Veröffentlichen nach",
  "registry.publishContent": "Anweisungen",
  "registry.published": "Version {version} veröffentlicht",
  "registry.publishScopeHelp":
    "Wo der Skill liegt, also wer ihn im Katalog sieht — nicht, wo er gilt. Ein Projekt kann einen Skill installieren, der irgendwo liegt, wohin es sehen kann.",

  "gates.bind": "Gate binden",
  "gates.bindLead":
    "Binden lassen sich nur Gates, die der Server tatsächlich registriert hat; eine Bindung auf ein nicht registriertes Gate lässt die gesamte Operation scheitern, statt ignoriert zu werden — diese Liste ist also das, was existiert.",
  "gates.gateName": "Gate",
  "gates.hook": "Hook",
  "gates.unbind": "Lösen",
  "gates.confirmUnbind":
    "{gate} von {hook} lösen? Operationen, die es verweigert hat, gehen danach durch.",
  "gates.inherited": "Geerbt",

  "skills.override": "Überschreiben",
  "skills.overrideLead":
    "Eine andere Version an dieses Projekt binden und damit überdecken, was eine breitere Ebene vorgeschrieben hat. Eine als nicht überschreibbar markierte Bindung verweigert das — genau dafür ist das Kennzeichen da.",
  "skills.overrideVersion": "Zu bindende Version",

  "orgs.create": "Neue Organisation",
  "orgs.createLead":
    "Sie werden ihre erste Eigentümerin oder ihr erster Eigentümer. Projekte und Skills lassen sich danach auf sie beziehen.",
  "orgs.name": "Name",
  "orgs.addMember": "Mitglied hinzufügen",
  "orgs.addMemberLead":
    "Eine Organisationsrolle betrifft einen Menschen, hier wird also ein Benutzerkonto benannt und kein Principal — ein Agent handelt stattdessen innerhalb eines Projekts und kann so eine Rolle nicht halten.",
  "orgs.userId": "Benutzer-ID",

  "settings.title": "Einstellungen",
  "settings.lead": "Was dieses Projekt ist und was sich hier daran ändern lässt.",
  "settings.identity": "Identität",
  "settings.visibility": "Sichtbarkeit",
  "settings.encryption": "Verschlüsselung",
  "settings.encryptionBody":
    "Inhalte sind im Ruhezustand unter einem Schlüssel dieses Projekts verschlüsselt, der selbst vom Wurzelschlüssel der Installation umschlossen wird. Übergabetexte und Geheimnisse gehen durch diesen Umschlag; die Bytes von Artefakten gehen in den Objektspeicher.",
  "settings.danger": "Nicht umkehrbar",
  "settings.noWrites":
    "Auf diesem Bildschirm lässt sich nichts ändern: die API hat weder ein Update noch ein Löschen für ein Projekt. Umbenennen, Sichtbarkeit ändern und Löschen sind Operationen der Kommandozeile, und ein Bedienelement, das einen Dialog öffnet und dann scheitert, wäre schlechter als sein Fehlen.",
  "settings.connect": "Einen Agenten verbinden",
  "settings.connectLead":
    "Richten Sie einen MCP-Client auf diesen Server, mit einem persönlichen Zugriffstoken. Das Projekt wird pro Aufruf benannt, ein Token erreicht also jedes Projekt, in dem Sie Mitglied sind.",
  "settings.mcpEndpoint": "MCP-Endpunkt",
  "settings.restEndpoint": "REST",

  "dashboard.waitingLead":
    "Projekte, in denen eine Übergabe offen oder übernommen ist — Arbeit, die jemand für die nächste Person oder den nächsten Agenten hinterlassen hat.",

  "find.open": "Suchen",
  "find.placeholder": "Zu einem Projekt, einem Skill, einem Bildschirm springen…",
  "find.project": "Projekt",
  "find.skill": "Skill",
  "find.screen": "Bildschirm",
  "find.nothing": "Dazu passt nichts",
  "find.hint": "{shortcut} öffnet dies. ↑↓ bewegt, Enter springt, Esc schließt.",

  "common.copy": "Kopieren",
  "common.copied": "Kopiert",

  "error.crashed.title": "Dieser Bildschirm funktioniert nicht mehr",
  "error.crashed.body":
    "Beim Zeichnen dieser Seite ist in der Oberfläche etwas fehlgeschlagen. An dem, was Sie angesehen haben, wurde auf dem Server nichts geändert. Neu laden behebt es meistens; wenn nicht, ist die Angabe unten das, was Sie melden sollten.",
  "error.crashed.reload": "Neu laden",
  "error.notFoundPage.title": "Diese Seite gibt es nicht",
  "error.notFoundPage.body": "Diese Adresse passt zu nichts in dieser Oberfläche.",
  "error.backToDashboard": "Zur Übersicht",

  "common.copyFailed":
    "Kopieren nicht möglich — markieren Sie den Wert und kopieren Sie ihn selbst",
  "error.conflict.generic": "Das ließ sich nicht ausführen",
  "skill.livesAtNow": "Liegt auf der Ebene",
  "skill.alreadyWidest": "Liegt bereits installationsweit",
  "skill.alreadyWidestBody": "Weiter geht es nicht — das ist die breiteste Ebene, die es gibt.",
  "skill.newVersion": "Neue Version veröffentlichen",
  "skill.newVersionLead":
    "Eine veröffentlichte Version ist unveränderlich, einen Skill zu korrigieren heißt also, die nächste zu veröffentlichen. Beide bleiben bestehen, und was an die alte Version gebunden ist, bekommt weiter die alte, bis es umgehängt wird.",
  "skill.newVersionFrom": "Ausgehend von v{version}",
  "skill.publishedVersion": "v{version} veröffentlicht",

  "picker.choose": "Jemanden auswählen…",
  "picker.userId": "Benutzer-ID",
  "picker.noDirectory":
    "Nur wer die Installation administriert, kann die Personen darauf auflisten — deshalb fragt dies nach einer ID, statt eine Auswahl anzubieten. Fragen Sie danach, oder kopieren Sie sie aus dem Konto der betreffenden Person.",

  "admin.users": "Personen",
  "admin.usersLead":
    "Alle mit einer Identität auf dieser Installation. Ihre Projektrollen liegen am Projekt, nicht hier.",
  "admin.servicePrincipals": "Agenten",
  "admin.servicePrincipalsLead":
    "Nicht-menschliche Identitäten — ein CI-Lauf, ein geplanter Agent. So eine handelt in Projekten und erscheint im Audit genauso wie ein Mensch, und genau darum geht es: was ein Agent getan hat, ist dem Agenten zuzuordnen und nicht der Person, die ihn eingerichtet hat.",
  "admin.column.name": "Name",
  "admin.column.created": "Angelegt",
  "admin.noUsers": "Außer Ihnen noch niemand",
  "admin.noServicePrincipals": "Keine Agenten registriert",
  "admin.noServicePrincipalsBody":
    "Registrieren Sie einen und stellen Sie ihm dann ein Token aus — dieses Token legt ein CI-Lauf oder ein geplanter Agent vor, wenn er diesen Server aufruft.",
  "admin.issueFor": "Token ausstellen",
  "admin.issueForLead":
    "Ein Token für diese Identität, nicht für Sie. Es wird einmal angezeigt und nie wieder, und wer es hat, handelt als diese Identität.",

  "picker.agent": "Agent",
  "admin.tokens": "Token",
  "admin.tokensLead":
    "Alle Zugangsmerkmale dieser Identität. Ein Widerruf wirkt ab der nächsten Anfrage und ändert keine Mitgliedschaft.",
  "admin.noTokens": "Keine Token ausgestellt",
  "admin.confirmRevokeFor":
    "Dieses Zugangsmerkmal widerrufen? Was immer es verwendet, hört sofort auf zu funktionieren — bei einem laufenden Agenten mitten in der Arbeit.",

  "admin.tab.usage": "Nutzung",
  "admin.usage": "Nutzung",
  "admin.usageLead":
    "Wie stark diese Installation tatsächlich genutzt wird und was sie enthält. Nur Anzahlen — wer die Installation administriert, verwaltet sie und liest nicht ihre Inhalte, hier steht also nichts, was ein Projekt, einen Pfad oder eine Person benennt.",
  "admin.usage.content": "Was sie enthält",
  "admin.usage.coordination": "Koordination",
  "admin.usage.activity": "Aktivität",
  "admin.usage.credentials": "Zugangsmerkmale",
  "admin.usage.inOrganizations":
    "Projekte, in {count, plural, one {# Organisation} other {# Organisationen}}",
  "admin.usage.andAgents": "Personen, und {count, plural, one {# Agent} other {# Agenten}}",
  "admin.usage.acrossArtifacts": "über {count, plural, one {# Artefakt} other {# Artefakte}}",
  "admin.usage.ofIssued":
    "Zugangsmerkmale verwendet, von {count, plural, one {# ausgestellten} other {# ausgestellten}}",
  "admin.usage.artifacts": "Artefakte",
  "admin.usage.versions": "Versionen",
  "admin.usage.stored": "Gespeichert",
  "admin.usage.storedHelp":
    "Die Größe jeder jemals geschriebenen Artefaktversion. Nicht der Plattenverbrauch — die Datenbank sieht den Objektspeicher nicht, und eine mit „Platte“ beschriftete Zahl aus SQL wäre geraten.",
  "admin.usage.secrets": "Geheimnisse",
  "admin.usage.conversations": "Unterhaltungen",
  "admin.usage.historyObjects": "Aufgezeichnete Objekte",
  "admin.usage.handoffsOpen": "Offen",
  "admin.usage.handoffsClaimed": "Übernommen",
  "admin.usage.handoffsClosed": "Abgeschlossen",
  "admin.usage.handoffs": "Übergaben",
  "admin.usage.skills": "Skills",
  "admin.usage.skillVersions": "Skill-Versionen",
  "admin.usage.skillBindings": "Skill-Bindungen",
  "admin.usage.gateBindings": "Gate-Bindungen",
  "admin.usage.events": "Ereignisse",
  "admin.usage.auditRecords": "Audit-Einträge",
  "admin.usage.securityEvents": "Sicherheitsereignisse",
  "admin.usage.newestActivity": "Letzte Aktivität",
  "admin.usage.oldestProject": "Erstes Projekt",
  "admin.usage.active": "Aktiv",
  "admin.usage.revoked": "Widerrufen",
  "admin.usage.expired": "Abgelaufen",
  "admin.usage.usedRecentlyHelp":
    "Die eine Zahl, die „wird das genutzt“ beantwortet und nicht „was liegt hier“. Ein Zugangsmerkmal, das eine Woche lang keine Anfrage gestellt hat, ist entweder fertig oder vergessen.",
  "admin.usage.quiet": "Es ist noch nichts passiert",
  "admin.usage.quietBody":
    "Keine Ereignisse aufgezeichnet. Eine Installation mit Projekten, aber ohne Ereignisse ist eine, in die nichts geschrieben wurde.",
};
