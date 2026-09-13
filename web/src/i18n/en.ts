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
  "auth.sameToken": "The same token a CLI or an MCP client uses. There is no separate web session.",
  "auth.indistinct":
    "A token that has been revoked and one that never existed read the same here. Saying which is which would tell whoever is holding a stolen token that half their guess was right.",

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
  "error.rateLimited.body":
    "This credential is making requests too quickly. Wait a moment and retry.",
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
  "projects.lead":
    "Every project you can act in. The path is how people say it; the UUID is what the system means.",
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
  "members.addHelp":
    "By principal UUID. Nothing in the API lists the principals of an installation, so there is nothing to pick from — copy the UUID from the person's own account screen.",
  "members.principalPlaceholder": "Principal UUID",
  "members.noRemoval":
    "Removing a member and changing a role are not built: the API has no write for either, so this screen does not offer one.",

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

  "orgs.title": "Organizations",
  "orgs.lead":
    "The scopes above your projects. An organization is where a skill or a gate is mandated once instead of project by project.",
  "orgs.column.name": "Organization",
  "orgs.column.role": "Your role",
  "orgs.column.members": "Members",
  "orgs.empty": "You are not in any organization",
  "orgs.emptyBody":
    "Organization membership is keyed by user, so a service principal never has one — that is the correct answer for it, not a refusal.",
  "orgs.members": "Members",
  "orgs.membersLead":
    "Membership is by user, not by principal: an organization role is about a person, and a service principal acts inside a project instead.",
  "orgs.audit": "Activity",
  "orgs.column.user": "User",
  "orgs.select": "Select an organization",
  "orgs.selectBody": "Pick one on the left to see its members and its activity.",

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

  "admin.title": "Administration",
  "admin.lead":
    "The installation itself: who administers it, who was refused, and the identities everything else hangs off.",
  "admin.tab.overview": "Overview",
  "admin.tab.identities": "Identities",
  "admin.tab.security": "Security log",
  "admin.admins": "Installation admins",
  "admin.adminsLead":
    "An installation admin can read across every project. There is no write for granting or revoking that here — it is a CLI operation on purpose.",
  "admin.column.user": "User",
  "admin.column.role": "Role",
  "admin.column.granted": "Granted",
  "admin.column.grantedBy": "Granted by",
  "admin.createUser": "Create user",
  "admin.createUserLead": "A person who can hold a token and be added to projects.",
  "admin.email": "Email",
  "admin.displayName": "Display name",
  "admin.createService": "Create service principal",
  "admin.createServiceLead":
    "A non-human identity — a CI job, a scheduled agent. It acts in projects and shows up in audit like anyone else.",
  "admin.name": "Name",
  "admin.description": "Description",
  "admin.created": "Created {name}",
  "admin.credentials": "Credentials",
  "admin.credentialsLead":
    "Tokens issued to one principal. Paste a principal UUID to look them up.",
  "admin.lookup": "Look up",
  "admin.security": "Security log",
  "admin.identitiesLead":
    "Creating the identities everything else hangs off. Nothing in the API lists users or service principals, so this page creates them and shows what it just created — it has no directory to show you.",
  "admin.securityLead":
    "What the server refused: a token that did not resolve, a capability denied, a caller throttled. Kept apart from project audit on purpose — a refusal is often about a project the caller cannot even see, and the pattern worth catching, one credential probing several projects, is invisible from inside any one of them.",
  "admin.securityNote":
    "A successful sign-in is not a refusal and is not recorded here. What a credential last did is on the token itself, as its last-used time.",
  "admin.column.kind": "Kind",
  "admin.column.reason": "Reason",
  "admin.column.capability": "Capability",
  "admin.column.credential": "Credential",
  "admin.securityEmpty": "Nothing recorded",
  "admin.securityEmptyBody":
    "No refusals, no unknown credentials, nothing to look at. That is the good case.",

  "tokens.title": "Access tokens",
  "tokens.lead":
    "The credentials you use from a CLI, an editor or an MCP client. Each one is yours alone — there is no way to issue a token for someone else from here.",
  "tokens.column.type": "Client",
  "tokens.column.created": "Created",
  "tokens.column.lastUsed": "Last used",
  "tokens.column.expires": "Expires",
  "tokens.issue": "Issue token",
  "tokens.clientType": "Client type",
  "tokens.expiry": "Expires in",
  "tokens.expiry.never": "Never",
  "tokens.expiry.30d": "30 days",
  "tokens.expiry.90d": "90 days",
  "tokens.expiry.1y": "1 year",
  "tokens.revoke": "Revoke",
  "tokens.revoked": "Revoked",
  "tokens.expired": "Expired",
  "tokens.active": "Active",
  "tokens.empty": "No tokens yet",
  "tokens.emptyBody": "Issue one to connect an agent to this installation.",
  "tokens.justIssued": "Copy this token now",
  "tokens.justIssuedBody":
    "Only its hash is stored, so this is the one and only time it can be shown. Closing this panel does not revoke it — but it does mean it cannot be read again.",
  "tokens.confirmRevoke":
    "Revoke this credential? Anything still using it stops working immediately.",

  "prefs.title": "Preferences",
  "prefs.lead": "How this installation speaks to you. These follow your account, not this browser.",
  "prefs.language": "Interface language",
  "prefs.languageHelp":
    "Everything the product writes. Content other people wrote is never translated.",
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
  "prefs.dateFormatHelp":
    "How a calendar date is written. 03-09-2026 means two different days depending on who reads it.",
  "prefs.timeFormatHelp": "Clock convention.",
  "prefs.timeZoneHelp": "Timestamps are stored in UTC and shown in this zone.",
  "prefs.showUTCHelp": "For audit and security screens, where a shared reference matters.",
  "prefs.storageUnitsHelp":
    "How byte counts are written. The stored value never changes — only how it reads.",
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

  "artifacts.title": "Artifacts",
  "artifacts.lead":
    "The shared store agents read and write. Every write makes a new version; nothing is overwritten in place, so a bad write can be read past rather than recovered from a backup.",
  "artifacts.column.path": "Path",
  "artifacts.column.type": "Content type",
  "artifacts.column.version": "Version",
  "artifacts.column.size": "Size",
  "artifacts.column.hash": "Content hash",
  "artifacts.empty": "No artifacts yet",
  "artifacts.emptyBody": "An agent writing through the API, the CLI or MCP creates the first one.",
  "artifacts.history": "Versions",
  "artifacts.content": "Content",
  "artifacts.select": "Select an artifact",
  "artifacts.selectBody": "Pick a path on the left to see its versions and content.",
  "artifacts.binary": "This is not text",
  "artifacts.binaryBody":
    "The content type says this is not text, so it is not rendered. The version, the size and the hash above are what this screen can tell you about it.",
  "artifacts.viewing": "Viewing version {version}",
  "artifacts.latest": "Latest",
  "artifacts.noWrite":
    "Writing an artifact from here is not built. Agents write through the API, the CLI and MCP, and an editor that could silently lose a concurrent write is worse than no editor.",

  "secrets.title": "Secrets",
  "secrets.lead":
    "Names and references only. A value goes in once and never comes back out through this interface — what a project gets is a reference it hands to a gate.",
  "secrets.column.name": "Name",
  "secrets.column.reference": "Reference",
  "secrets.add": "Add secret",
  "secrets.value": "Value",
  "secrets.delete": "Delete",
  "secrets.empty": "No secrets in this project",
  "secrets.emptyBody": "Add one here, or with the CLI.",
  "secrets.neverShown":
    "The value is written once and is never readable again, by anyone, through any adapter. Deleting and re-adding is the only way to change one.",
  "secrets.confirmDelete":
    "Delete the secret {name}? Anything using its reference will start failing.",

  "gates.title": "Policies",
  "gates.lead":
    "Gates bound to this project, and the ones it inherits. A gate runs on a hook and can refuse the operation — the refusal lands on the activity timeline.",
  "gates.column.gate": "Gate",
  "gates.column.hook": "Hook",
  "gates.column.scope": "Bound at",
  "gates.column.enabled": "State",
  "gates.enabled": "Enforcing",
  "gates.disabled": "Off",
  "gates.empty": "No gates apply here",
  "gates.emptyBody": "Nothing is intercepting operations in this project.",
  "gates.config": "Configuration",

  "events.title": "Events",
  "events.lead":
    "The outbox stream. An event is what other systems consume; the audit record next to it is what a person reads.",
  "events.column.seq": "Seq",
  "events.column.type": "Type",
  "events.column.resource": "Resource",
  "events.column.payload": "Payload",
  "events.empty": "No events yet",

  "handoff.createLead":
    "What the next agent needs to carry on. Written by a person here; agents write the same thing through MCP.",
  "handoff.summaryHelp": "One line. What is this work.",
  "handoff.completedHelp": "What is already done, so it is not done twice.",
  "handoff.nextHelp": "What to do next, concretely enough to start on.",
  "handoff.created": "Handoff created",
  "handoff.confirmClose":
    "Close this handoff? It stops being the current one and nobody can claim it.",
  "handoff.openExists": "A handoff is already open here",
  "handoff.openExistsBody":
    "Close it before leaving another — two open handoffs make “what should I pick up” ambiguous, which is the question they exist to answer.",

  "projects.create": "New project",
  "projects.createLead":
    "A path and a name. The path is how people refer to it and cannot be changed afterwards; you become its OWNER.",
  "projects.path": "Path",
  "projects.pathHelp": "namespace/slug",
  "projects.name": "Display name",
  "projects.created": "Created {path}",

  "handoff.history": "Earlier handoffs",
  "handoff.column.created": "Created",
  "handoff.column.status": "Status",
  "handoff.column.summary": "Summary",

  "skill.versions": "Versions",
  "skill.versionsLead":
    "Every version ever published under this name. A published version is immutable — publishing again adds a row here, it never edits one.",
  "skill.content": "Content",
  "skill.contentLead": "What an agent is handed when this version resolves.",
  "skill.provenance": "Provenance",
  "skill.checksum": "Checksum",
  "skill.capabilities": "Declared capabilities",
  "skill.author": "Author",
  "skill.origin": "Origin",
  "skill.promote": "Promote",
  "skill.promoteLead":
    "Move the skill onto a wider shelf so more projects can see it. One way only: narrowing again would pull it out from under bindings that were legitimate when they were made.",
  "skill.promoteTo": "Move to",
  "skill.promoted": "Now lives at {scope}",
  "skill.install": "Install",
  "skill.installed": "Installed",
  "skill.installLead":
    "Pin a version of a skill to this project. The binding names one concrete version — there is no version-range resolver, so nothing changes under you.",
  "skill.notFound": "No such skill",
  "skill.notFoundBody": "It may have been published somewhere you cannot see.",
  "skill.backToRegistry": "Back to the registry",
  "skill.pin": "Pin",
  "skill.unpin": "Unpin",
  "skill.pinned": "Pinned",

  "registry.livesAt": "Lives at",
  "registry.empty": "The catalogue is empty",
  "registry.emptyBody":
    "Publish a skill from a project, or promote one onto the installation shelf.",

  "activity.empty": "Nothing has happened here yet",
  "activity.emptyBody":
    "Reads and writes through the API, the CLI and MCP all land on this timeline.",
} as const;

export type MessageKey = keyof typeof en;
