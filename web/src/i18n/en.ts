/**
 * English is the source language.
 *
 * Every other locale is a translation of this file and nothing else: keys are
 * added here first, and `npm run i18n:check` fails when another locale has
 * drifted — a missing key, or one that no longer exists. Translators never
 * invent keys, and no screen ever reaches for a string that has no English
 * original.
 *
 * Rules for writing one:
 *  - Keep the sentence whole. Never assemble prose from fragments, because
 *    word order is not universal and the fragments cannot be reordered.
 *  - Put variables in {braces} rather than concatenating around the string.
 *  - Anything countable goes through a plural form, not "(s)".
 *  - Dates, times, numbers and byte sizes are NOT strings here. They are
 *    formatted from raw values in lib/format.ts against the reader's locale.
 */
export const en = {
  "app.name": "Context Commander",
  "app.skipToContent": "Skip to content",

  "nav.dashboard": "Dashboard",
  "nav.projects": "Projects",
  "nav.organizations": "Organizations",
  "nav.registry": "Skill Registry",
  "nav.administration": "Administration",
  "nav.workspace": "Workspace",
  "nav.installation": "Installation",
  "nav.preferences": "Preferences",
  "nav.search": "Search",
  "nav.signOut": "Sign out",

  "theme.dark": "Dark",
  "theme.light": "Light",
  "theme.label": "Theme",

  "auth.title": "Sign in",
  "auth.lead": "Paste a personal access token. Create one with {command}.",
  "auth.token": "Access token",
  "auth.submit": "Sign in",
  "auth.failed": "That token was not accepted.",
  "auth.signedInAs": "Signed in as {name}",

  "common.loading": "Loading…",
  "common.retry": "Try again",
  "common.cancel": "Cancel",
  "common.save": "Save",
  "common.close": "Close",
  "common.loadMore": "Load more",
  "common.perPage": "{count} per page",
  "common.showingMore": "Showing {count} · more available",
  "common.showingAll": "Showing {count} · complete",
  "common.endOfList": "End of list",
  "common.none": "None",
  "common.never": "Never",
  "common.unknown": "Unknown",
  "common.you": "You",

  "error.forbidden.title": "You do not have access to this",
  "error.forbidden.body":
    "Your role on this project does not include the capability this screen needs. Ask an owner or admin if you should.",
  "error.notFound.title": "Not found",
  "error.notFound.body": "This either does not exist or is not visible to you.",
  "error.conflict.title": "This changed while you were reading it",
  "error.conflict.body":
    "Someone wrote a newer version since you loaded this one. Read the newest version, merge your change into it and try again — saving over it would delete their work.",
  "error.unavailable.title": "The server is not responding",
  "error.unavailable.body": "Nothing was changed. Try again in a moment.",
  "error.rateLimited.title": "Too many requests",
  "error.rateLimited.body": "This credential is making requests too quickly. Wait a moment and retry.",
  "error.generic.title": "Something went wrong",

  "empty.noResults": "Nothing matches that",
  "empty.noResultsBody": "Try a different search, or clear the filters.",

  "dashboard.title": "Dashboard",
  "dashboard.lead": "What changed, where you were working, and whether anything is waiting on you.",
  "dashboard.waiting": "Waiting on you",
  "dashboard.recent": "Recent activity",
  "dashboard.yourProjects": "Your projects",
  "dashboard.clients": "Connected clients",
  "dashboard.openHandoff": "Open handoff",
  "dashboard.noneWaiting": "Nothing is waiting on you",
  "dashboard.noneWaitingBody": "No open handoffs, no refusals worth a look.",

  "projects.title": "Projects",
  "projects.lead": "Every project you can act in. The path is how people say it; the UUID is what the system means.",
  "projects.filter": "Filter projects…",
  "projects.column.project": "Project",
  "projects.column.namespace": "Namespace",
  "projects.column.role": "Your role",
  "projects.column.members": "Members",
  "projects.column.activity": "Last activity",
  "projects.count": "{count, plural, one {# project} other {# projects}}",
  "projects.empty": "You are not a member of any project",
  "projects.emptyBody": "A project becomes visible here once someone adds you to it.",

  "project.overview": "Overview",
  "project.members": "Members",
  "project.skills": "Skills",
  "project.policies": "Policies",
  "project.secrets": "Secrets",
  "project.activity": "Activity",
  "project.settings": "Settings",
  "project.identity": "Identity",
  "project.path": "Path",
  "project.uuid": "UUID",
  "project.yourRole": "You are",
  "project.created": "Created",

  "handoff.current": "Current handoff",
  "handoff.summary": "Summary",
  "handoff.completed": "Completed work",
  "handoff.next": "Next actions",
  "handoff.references": "References",
  "handoff.claim": "Claim",
  "handoff.close": "Close handoff",
  "handoff.create": "Create handoff",
  "handoff.status.open": "Open",
  "handoff.status.claimed": "Claimed",
  "handoff.status.closed": "Closed",
  "handoff.claimedBy": "Claimed by {name}",
  "handoff.claimedByYou": "Claimed by you",
  "handoff.none": "No handoff is open",
  "handoff.noneBody": "Nothing is waiting to be taken over here.",
  "handoff.alreadyClaimed": "Another client claimed this first",
  "handoff.alreadyClaimedBody":
    "That is the coordination answer, not an error in your request. Re-read it and decide whether the work is still yours to do.",

  "members.title": "Members",
  "members.lead":
    "Who may act in this project, and as what. A client credential is not a member — the principal behind it is.",
  "members.column.principal": "Principal",
  "members.column.type": "Type",
  "members.column.role": "Role",
  "members.column.added": "Added",
  "members.type.user": "User",
  "members.type.service": "Service principal",
  "members.add": "Add principal",
  "members.count": "{count, plural, one {# member} other {# members}}",

  "skills.title": "Skills",
  "skills.lead":
    "The effective set: installation, organization and project bindings composed, narrowest wins per skill. Every override is visible.",
  "skills.column.skill": "Skill",
  "skills.column.livesAt": "Lives at",
  "skills.column.version": "Resolved",
  "skills.column.trust": "Trust",
  "skills.column.state": "State",
  "skills.browse": "Browse registry",
  "skills.install": "Install skill",
  "skills.empty": "No skills apply here",
  "skills.emptyBody": "Install one from the registry, or publish one from this project.",

  "registry.title": "Skill Registry",
  "registry.lead":
    "Everything you can see: skills in your projects, in your organizations, and on the installation shelf. Agents search this same catalogue.",
  "registry.search": "Search skills by name…",
  "registry.column.latest": "Latest",
  "registry.column.checksum": "Checksum",
  "registry.column.author": "Author",
  "registry.publish": "New skill",
  "registry.results": "{count, plural, one {# result} other {# results}} for “{query}”",

  "scope.installation": "Installation",
  "scope.organization": "Organization",
  "scope.project": "Project",

  "trust.signed": "Signed",
  "trust.unverified": "Unverified",

  "role.OWNER": "Owner",
  "role.ADMIN": "Admin",
  "role.CONTRIBUTOR": "Contributor",
  "role.READER": "Reader",

  "activity.title": "Activity",
  "activity.lead":
    "Events and audit on one timeline. They answer the same question — what happened here — and splitting them makes a reader stitch them back together.",
  "activity.column.time": "Time",
  "activity.column.action": "Action",
  "activity.column.resource": "Resource",
  "activity.column.principal": "Principal",
  "activity.column.client": "Client",
  "activity.column.result": "Result",
  "activity.result.allow": "Allowed",
  "activity.result.deny": "Denied",
  "activity.result.error": "Error",

  "prefs.title": "Preferences",
  "prefs.lead":
    "How this installation speaks to you. These follow your account, not this browser.",
  "prefs.language": "Interface language",
  "prefs.languageHelp": "Everything the product writes. Content other people wrote is never translated.",
  "prefs.dateFormat": "Date format",
  "prefs.timeFormat": "Time format",
  "prefs.timeZone": "Time zone",
  "prefs.showUTC": "Show UTC alongside",
  "prefs.numberFormat": "Number format",
  "prefs.storageUnits": "Storage units",
  "prefs.units.decimal": "GB · decimal, 1000-based",
  "prefs.units.binary": "GiB · binary, 1024-based",
  "prefs.notStored":
    "Preferences are not stored on the server yet, so these live in this browser for now.",
  "prefs.section.language": "Language",
  "prefs.section.dates": "Dates and time",
  "prefs.section.numbers": "Numbers and units",
  "prefs.dateFormatHelp": "How a calendar date is written. 03-09-2026 means two different days depending on who reads it.",
  "prefs.timeFormatHelp": "Clock convention.",
  "prefs.timeZoneHelp": "Timestamps are stored in UTC and shown in this zone.",
  "prefs.showUTCHelp": "For audit and security screens, where a shared reference matters.",
  "prefs.storageUnitsHelp": "How byte counts are written. The stored value never changes — only how it reads.",
  "prefs.date.locale": "Follow language",
  "prefs.date.ymd": "YYYY-MM-DD",
  "prefs.date.dmy": "DD-MM-YYYY",
  "prefs.date.mdy": "MM-DD-YYYY",
  "prefs.time.locale": "Follow language",
  "prefs.time.h24": "24-hour",
  "prefs.time.h12": "12-hour",
  "prefs.tz.device": "Follow this device ({zone})",
  "prefs.on": "On",
  "prefs.off": "Off",
  "prefs.serverFormat":
    "The server never sends a formatted string. It sends the value; the interface reads it.",

  "dashboard.noProjects": "You are not in any project yet",
  "dashboard.activityHere": "Activity shows up once there is a project to watch.",

  "project.tab.overview": "Overview",
  "project.tab.members": "Members",
  "project.tab.skills": "Skills",
  "project.tab.activity": "Activity",
  "project.notBuilt.title": "Not built yet",
  "project.notBuilt.artifacts":
    "Artifacts, notes, policies and secrets have screens in the mockups but no read path in the API yet.",

  "handoff.history": "Earlier handoffs",
  "handoff.column.created": "Created",
  "handoff.column.status": "Status",
  "handoff.column.summary": "Summary",

  "registry.livesAt": "Lives at",
  "registry.empty": "The catalogue is empty",
  "registry.emptyBody": "Publish a skill from a project, or promote one onto the installation shelf.",

  "activity.empty": "Nothing has happened here yet",
  "activity.emptyBody": "Reads and writes through the API, the CLI and MCP all land on this timeline.",
} as const;

export type MessageKey = keyof typeof en;
