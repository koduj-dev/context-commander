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
  "nav.signOut": "Sign out",
  "find.open": "Search",
  "find.placeholder": "Jump to a project, a skill, a screen…",
  "find.project": "Project",
  "find.skill": "Skill",
  "find.screen": "Screen",
  "find.nothing": "Nothing matches that",
  "find.hint": "{shortcut} opens this. ↑↓ to move, Enter to go, Esc to close.",

  "theme.dark": "Dark",
  "theme.light": "Light",
  "theme.label": "Theme",

  "auth.title": "Sign in",
  "auth.submit": "Sign in",
  "auth.signedInAs": "Signed in as {name}",
  "auth.indistinct":
    "A wrong password, an address with no account, and an account with no password all read the same here. Saying which is which would tell whoever is guessing that half their guess was right.",

  "auth.withPassword": "Email and password",
  "auth.withToken": "Access token",
  "auth.email": "Email",
  "auth.emailOrUsername": "Email or username",
  "auth.directoryUnavailable":
    "The directory could not be reached. This is not a problem with your password — try again shortly, or tell whoever runs this installation.",
  "auth.directoryNoEmail":
    "Your directory entry has no email address, so an account cannot be created for it. An administrator needs to add one.",
  "auth.password": "Password",
  "auth.failed": "Incorrect email or password.",
  "auth.tokenLabel": "Access token",
  "auth.tokenLead":
    "A personal access token from {command}. It is exchanged for a session and not stored in this browser.",
  "auth.tokenFailed": "That token was not accepted.",
  "auth.rateLimited": "Too many attempts. Wait a moment and try again.",

  "setup.title": "Create the first administrator",
  "setup.lead":
    "This installation has no users yet. The server printed a one-time setup code to its log when it started — paste it here.",
  "setup.code": "Setup code",
  "setup.email": "Email",
  "setup.name": "Display name",
  "setup.password": "Password",
  "setup.passwordHint": "At least {min} characters. Length is the only rule.",
  "setup.submit": "Create administrator",
  "setup.codeFailed":
    "That setup code is not valid. The current one is in the server's log; restarting the server prints a new one.",
  "setup.alreadyDone": "This installation already has users. Sign in instead.",
  "setup.whereIsTheCode":
    "The code exists only in the running server's memory and only while there are no users, so it never reaches a backup and cannot be read out of the database.",

  "account.password": "Password",
  "account.passwordLead":
    "Used to sign in to this interface. Agents and the CLI use access tokens instead, which are managed separately.",
  "account.currentPassword": "Current password",
  "account.newPassword": "New password",
  "account.confirmPassword": "Repeat new password",
  "account.setPassword": "Set password",
  "account.changePassword": "Change password",
  "account.passwordSet": "Password changed.",
  "account.passwordMismatch": "The two new passwords do not match.",
  "account.currentWrong": "The current password is not correct.",
  "account.noPasswordYet":
    "This account has no password yet. You are signed in with an access token, which is proof enough to set one.",

  "account.sessions": "Sessions",
  "account.sessionsLead":
    "Where this account is signed in. Ending a session makes its cookie stop working immediately, on whatever machine holds it.",
  "account.sessionCurrent": "This browser",
  "account.sessionStarted": "Started",
  "account.sessionLastUsed": "Last used",
  "account.sessionExpires": "Expires",
  "account.endSession": "End session",
  "account.noSessions": "No active sessions.",

  "common.loading": "Loading…",
  "common.retry": "Try again",
  "common.cancel": "Cancel",
  "common.close": "Close",
  "common.loadMore": "Load more",
  "common.showingMore": "Showing {count} · more available",
  "common.showingAll": "Showing {count} · complete",
  "common.endOfList": "End of list",
  "common.none": "None",
  "common.never": "Never",
  "common.unknown": "Unknown",
  "common.you": "You",
  "common.copy": "Copy",
  "common.copied": "Copied",
  "common.copyFailed": "Could not copy — select the value and copy it yourself",

  "error.forbidden.title": "You do not have access to this",
  "error.forbidden.body":
    "Your role on this project does not include the capability this screen needs. Ask an owner or admin if you should.",
  "error.notFound.title": "Not found",
  "error.notFound.body": "This either does not exist or is not visible to you.",
  "error.conflict.generic": "That could not be done",
  "error.unavailable.title": "The server is not responding",
  "error.unavailable.body": "Nothing was changed. Try again in a moment.",
  "error.rateLimited.title": "Too many requests",
  "error.rateLimited.body":
    "This credential is making requests too quickly. Wait a moment and retry.",
  "error.generic.title": "Something went wrong",
  "error.crashed.title": "This screen stopped working",
  "error.crashed.body":
    "Something in the interface failed while drawing this page. Nothing you were looking at was changed on the server. Reloading usually clears it; if it does not, the detail below is what to report.",
  "error.crashed.reload": "Reload",
  "error.notFoundPage.title": "No such page",
  "error.notFoundPage.body": "That address does not match anything in this interface.",
  "error.backToDashboard": "Go to the dashboard",

  "empty.noResults": "Nothing matches that",
  "empty.noResultsBody": "Try a different search, or clear the filters.",

  "dashboard.title": "Dashboard",
  "dashboard.lead": "What changed, where you were working, and whether anything is waiting on you.",
  "dashboard.waiting": "Waiting on you",
  "dashboard.yourProjects": "Your projects",
  "dashboard.noneWaiting": "Nothing is waiting on you",
  "dashboard.noneWaitingBody": "No open handoffs, no refusals worth a look.",

  "projects.title": "Projects",
  "projects.lead":
    "Every project you can act in. The path is how people say it; the UUID is what the system means.",
  "projects.filter": "Filter projects…",
  "projects.column.project": "Project",
  "projects.column.role": "Your role",
  "projects.count": "{count, plural, one {# project} other {# projects}}",
  "projects.empty": "You are not a member of any project",
  "projects.emptyBody": "A project becomes visible here once someone adds you to it.",

  "project.identity": "Identity",
  "project.path": "Path",
  "project.uuid": "UUID",

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
  "members.column.role": "Role",
  "members.column.added": "Added",
  "members.add": "Add principal",
  "members.count": "{count, plural, one {# member} other {# members}}",
  "members.addHelp":
    "A project role belongs to a principal — a person or an agent. Both are offered here if you administer this installation; otherwise it takes the principal id, which is not the same as a user id.",
  "members.principalPlaceholder": "Principal UUID",
  "picker.choose": "Choose someone…",
  "picker.userId": "User id",
  "picker.agent": "agent",
  "picker.noDirectory":
    "Only an installation admin can list the people on this installation, so this asks for an id rather than offering a choice. Ask an admin for it, or paste it from the person's own account screen.",
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
  "registry.column.author": "Author",
  "registry.publish": "New skill",
  "registry.publishLead":
    "A skill is instruction text an agent is handed. Publishing creates an immutable version — editing later means publishing again, and both versions stay.",
  "registry.publishName": "Name",
  "registry.publishAuthor": "Author",
  "registry.publishOrigin": "Origin",
  "registry.publishScope": "Publish to",
  "registry.publishContent": "Instructions",
  "registry.published": "Published version {version}",
  "registry.publishScopeHelp":
    "Where it lives, which is who can see it in the catalogue — not where it applies. A project can install a skill that lives anywhere it can see.",
  "registry.results": "{count, plural, one {# result} other {# results}} for “{query}”",

  "orgs.title": "Organizations",
  "orgs.lead":
    "The scopes above your projects. An organization is where a skill or a gate is mandated once instead of project by project.",
  "orgs.column.name": "Organization",
  "orgs.column.role": "Your role",
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
  "activity.column.result": "Result",
  "activity.result.allow": "Allowed",
  "activity.result.deny": "Denied",
  "activity.result.error": "Error",

  "admin.title": "Administration",
  "admin.lead":
    "The installation itself: who administers it, who was refused, and the identities everything else hangs off.",
  "admin.tab.overview": "Overview",
  "admin.tab.people": "People",
  "admin.tab.agents": "Agents",
  "admin.column.registeredBy": "Registered by",
  "admin.unattributed": "not recorded",
  "admin.registeredByHelp":
    "Who created the agent. It is a record of where the agent came from and grants it nothing — an agent reaches a project only through its own membership there.",
  "admin.tab.usage": "Usage",
  "admin.usage": "Usage",
  "admin.usageLead":
    "How much this installation is actually used, and how much it holds. Counts only — an installation admin administers the installation and does not read its content, so there is nothing here that names a project, a path or a person.",
  "admin.usage.content": "What it holds",
  "admin.usage.coordination": "Coordination",
  "admin.usage.activity": "Activity",
  "admin.usage.credentials": "Credentials",
  // The leading noun sits OUTSIDE the plural block on purpose: a word
  // immediately after `{` followed by a comma is indistinguishable from a
  // placeholder — to a real ICU parser and to i18n:check, which caught it.
  "admin.usage.inOrganizations":
    "projects, in {count, plural, one {# organization} other {# organizations}}",
  "admin.usage.andAgents": "people, and {count, plural, one {# agent} other {# agents}}",
  "admin.usage.acrossArtifacts": "across {count, plural, one {# artifact} other {# artifacts}}",
  "admin.usage.ofIssued": "credentials used, of {count, plural, one {# issued} other {# issued}}",
  "admin.usage.artifacts": "Artifacts",
  "admin.usage.versions": "Versions",
  "admin.usage.stored": "Stored",
  "admin.usage.storedHelp":
    "The size of every artifact version ever written. Not disk usage — the database cannot see the object store, and a figure labelled “disk” that came from SQL would be a guess.",
  "admin.usage.secrets": "Secrets",
  "admin.usage.conversations": "Conversations",
  "admin.usage.historyObjects": "Recorded objects",
  "admin.usage.handoffsOpen": "Open",
  "admin.usage.handoffsClaimed": "Claimed",
  "admin.usage.handoffsClosed": "Closed",
  "admin.usage.handoffs": "Handoffs",
  "admin.usage.skills": "Skills",
  "admin.usage.skillVersions": "Skill versions",
  "admin.usage.skillBindings": "Skill bindings",
  "admin.usage.gateBindings": "Gate bindings",
  "admin.usage.events": "Events",
  "admin.usage.auditRecords": "Audit records",
  "admin.usage.securityEvents": "Security events",
  "admin.usage.newestActivity": "Last activity",
  "admin.usage.oldestProject": "First project",
  "admin.usage.active": "Active",
  "admin.usage.revoked": "Revoked",
  "admin.usage.expired": "Expired",
  "admin.usage.usedRecentlyHelp":
    "The one figure that answers “is this used” rather than “what is stored”. A credential that has not made a request in a week is either finished or forgotten.",
  "admin.usage.quiet": "Nothing has happened yet",
  "admin.usage.quietBody":
    "No events recorded. An installation with projects but no events is one nothing has written to.",
  "admin.tab.security": "Security log",
  "admin.admins": "Installation admins",
  "admin.adminsLead":
    "An installation admin can read across every project. There is no write for granting or revoking that here — it is a CLI operation on purpose.",
  "admin.column.user": "User",
  "admin.column.role": "Role",
  "admin.column.granted": "Granted",
  "admin.createUser": "Create user",
  "admin.initialPassword": "Initial password (optional)",
  "admin.initialPasswordLead":
    "Give one and the person can sign in straight away. Leave it empty and they will need a token, or a password set for them later. At least {min} characters.",
  "admin.setPassword": "Set password",
  "admin.setPasswordFor": "Set a password for {name}",
  "admin.passwordSetFor": "Password set. {name} can sign in with it now.",
  "admin.setPasswordLead":
    "This is the power to sign in as them — the same as issuing them a token, which this screen already does. Tell them the password out of band; it is shown nowhere after this.",
  "admin.createUserLead": "A person who can hold a token and be added to projects.",
  "admin.email": "Email",
  "admin.displayName": "Display name",
  "admin.createService": "Create service principal",
  "admin.createServiceLead":
    "A non-human identity — a CI job, a scheduled agent. It acts in projects and shows up in audit like anyone else.",
  "admin.name": "Name",
  "admin.description": "Description",
  "admin.users": "People",
  "admin.usersLead":
    "Everyone with an identity on this installation. Their project roles live on the project, not here.",
  "admin.servicePrincipals": "Agents",
  "admin.servicePrincipalsLead":
    "Non-human identities — a CI job, a scheduled agent. One acts in projects and shows up in audit exactly like a person does, which is the point: what an agent did is attributable to the agent rather than to whoever set it up.",
  "admin.column.name": "Name",
  "admin.column.created": "Created",
  "admin.noUsers": "No one but you yet",
  "admin.noServicePrincipals": "No agents registered",
  "admin.noServicePrincipalsBody":
    "Register one, then issue it a token — that token is what a CI job or a scheduled agent presents when it calls this server.",
  "admin.issueFor": "Issue a token",
  "admin.tokens": "Tokens",
  "admin.tokensLead":
    "Every credential issued to this identity. Revoking one takes effect on the next request it makes and changes no membership.",
  "admin.noTokens": "No tokens issued",
  "admin.confirmRevokeFor":
    "Revoke this credential? Whatever is using it stops working immediately, and if that is a running agent it will start failing mid-task.",
  "admin.issueForLead":
    "A token for this identity, not for you. It is shown once and never again, and whatever holds it acts as this identity.",
  "admin.security": "Security log",
  "admin.securityLead":
    "What the server refused: a token that did not resolve, a capability denied, a caller throttled. Kept apart from project audit on purpose — a refusal is often about a project the caller cannot even see, and the pattern worth catching, one credential probing several projects, is invisible from inside any one of them.",
  "admin.securityNote":
    "A successful sign-in is not a refusal and is not recorded here. What a credential last did is on the token itself, as its last-used time.",
  "admin.column.kind": "Kind",
  "admin.column.reason": "Reason",
  "admin.column.capability": "Capability",
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

  "project.tab.overview": "Overview",
  "project.tab.members": "Members",
  "project.tab.skills": "Skills",
  "project.tab.activity": "Activity",
  "project.notBuilt.title": "Not built yet",
  "project.notBuilt.artifacts":
    "Artifacts, notes, policies and secrets have screens in the mockups but no read path in the API yet.",

  "context.title": "Context",
  "context.lead":
    "Exactly what an agent is handed when it starts here — the same assembled view MCP returns, rendered for a person. If an agent is behaving oddly, this is the first place to look.",
  "context.role": "Acting as",
  "context.capabilities": "Can",
  "context.assembled": "Assembled",
  "context.budget": "Budget",
  "context.budgetUsed": "{used} of {limit}",
  "context.budgetLead":
    "A pack has a size limit, and what does not fit is named rather than cut. An agent that cannot tell “there is nothing else” from “there was more and you did not get it” will act confidently on half a picture.",
  "context.truncated": "Left out",
  "context.nothingTruncated": "Nothing was left out",
  "context.nothingTruncatedBody": "Everything this project holds fitted inside the budget.",
  "context.column.what": "What",
  "context.column.ref": "Which one",
  "context.column.why": "Why",
  "context.included": "Included",
  "context.artifacts": "{count, plural, one {# artifact} other {# artifacts}}",
  "context.skills": "{count, plural, one {# skill} other {# skills}}",
  "context.recentEvents": "Recent events",
  "context.noHandoff": "No handoff in the pack",
  "context.noHandoffBody": "An agent starting here is told there is nothing to continue.",
  "context.budgetLimit": "Size limit",
  "context.apply": "Reassemble",

  "artifacts.title": "Artifacts",
  "artifacts.lead":
    "The shared store agents read and write. Every write makes a new version; nothing is overwritten in place, so a bad write can be read past rather than recovered from a backup.",
  "artifacts.column.path": "Path",
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
  "artifacts.edit": "Edit",
  "artifacts.new": "New artifact",
  "artifacts.save": "Save",
  "artifacts.delete": "Delete",
  "artifacts.confirmDelete":
    "Delete {path}? Its versions go with it, and anything reading that path starts failing.",
  "artifacts.editingFrom": "Editing from version {version}",
  "artifacts.editingNew": "A new artifact at this path",
  "artifacts.newPath": "Path",
  "artifacts.newPathHelp": "notes/decisions.md",
  "artifacts.contentType": "Content type",
  "artifacts.staleWrite": "Someone wrote a newer version while you were editing",
  "artifacts.staleWriteBody":
    "Your save was refused rather than applied — applying it would have deleted their work. Reload to see the newest version, put your change into it, and save again.",
  "artifacts.reload": "Reload the newest version",
  "artifacts.concurrency":
    "A save names the version it started from. If someone else wrote in between, the server refuses it instead of overwriting them.",
  "artifacts.binaryNoEdit":
    "This is not text, so it cannot be edited here. Agents write it through the API, the CLI and MCP.",

  "secrets.title": "Secrets",
  "secrets.lead":
    "Names and references only. A value goes in once and never comes back out through this interface — what a project gets is a reference it hands to a gate.",
  "secrets.column.name": "Name",
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
  "gates.bind": "Bind a gate",
  "gates.bindLead":
    "Only gates the server actually registered can be bound; a binding naming an unregistered gate fails the whole operation rather than being ignored, so this list is what exists.",
  "gates.gateName": "Gate",
  "gates.hook": "Hook",
  "gates.unbind": "Unbind",
  "gates.confirmUnbind":
    "Unbind {gate} from {hook}? Operations it was refusing start going through.",
  "gates.inherited": "Inherited",

  "skills.override": "Override",
  "skills.overrideLead":
    "Bind a different version at this project, shadowing what a wider scope mandated. A binding marked not overridable refuses this — that is the point of the flag.",
  "skills.overrideVersion": "Version to bind",

  "orgs.create": "New organization",
  "orgs.createLead": "You become its first OWNER. Projects and skills can then be scoped to it.",
  "orgs.name": "Name",
  "orgs.addMember": "Add member",
  "orgs.addMemberLead":
    "An organization role is about a person, so this names a user and not a principal — an agent acts inside a project instead, and cannot hold one of these.",
  "orgs.userId": "User id",

  "settings.title": "Settings",
  "settings.lead": "What this project is, and what can be changed about it from here.",
  "settings.identity": "Identity",
  "settings.visibility": "Visibility",
  "settings.encryption": "Encryption",
  "settings.encryptionBody":
    "Content is encrypted at rest under a key belonging to this project, itself wrapped by the installation root key. Handoff bodies and secrets travel through that envelope; artifact bytes go to the object store.",
  "settings.danger": "Irreversible",
  "settings.noWrites":
    "Nothing on this screen can be changed here: the API has no update or delete for a project. Renaming, changing visibility and deleting are CLI operations, and a control that opened a dialog and then failed would be worse than its absence.",
  "settings.connect": "Connecting an agent",
  "settings.connectLead":
    "Point an MCP client at this server with a personal access token. The project is named per call, so one token reaches every project you are a member of.",
  "settings.mcpEndpoint": "MCP endpoint",
  "settings.restEndpoint": "REST",

  "dashboard.waitingLead":
    "Projects where a handoff is open or claimed — work someone left for whoever comes next.",

  "history.title": "Conversations",
  "history.lead":
    "What clients recorded of their own sessions, so a handoff can point at where the work actually happened. Nothing here is written by this interface — it is ingested by the clients themselves.",
  "history.column.conversation": "Conversation",
  "history.column.started": "Started",
  "history.column.by": "Started by",
  "history.column.type": "Type",
  "history.column.recorded": "Recorded",
  "history.objects": "Recorded objects",
  "history.lineage": "Lineage",
  "history.lineageLead":
    "How one object was derived from another — a summary from a transcript, a compaction from a summary. It is what keeps a derived object from being mistaken for an original.",
  "history.column.from": "Derived from",
  "history.column.to": "Produced",
  "history.column.kind": "Relation",
  "history.select": "Select a conversation",
  "history.selectBody": "Pick one on the left to see what was recorded in it.",
  "history.empty": "No conversations recorded",
  "history.emptyBody":
    "A client records these as it works. Nothing has ingested history into this project yet.",
  "history.noObjects": "Nothing recorded in this conversation",
  "history.noLineage": "No derivations",
  "history.noLineageBody": "Every object here was recorded directly, none derived from another.",
  "history.notText": "Not text",
  "history.notTextBody":
    "A history object is whatever the client recorded, and these bytes are not valid UTF-8. The hash and the type above are what this screen can tell you about it.",

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
  "handoff.confirmClose":
    "Close this handoff? It stops being the current one and nobody can claim it.",

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
  "skill.livesAtNow": "Lives at",
  "skill.alreadyWidest": "Already on the installation shelf",
  "skill.alreadyWidestBody":
    "That is the widest scope there is, so there is nowhere further to move it.",
  "skill.newVersion": "Publish a new version",
  "skill.newVersionLead":
    "A published version is immutable, so correcting a skill means publishing the next one. Both stay, and anything pinned to the old version keeps getting the old version until it is repointed.",
  "skill.newVersionFrom": "Starting from v{version}",
  "skill.publishedVersion": "Published v{version}",
  "skill.promoteTo": "Move to",
  "skill.promoted": "Now lives at {scope}",
  "skill.install": "Install",
  "skill.installed": "Installed",
  "skill.installLead":
    "Pin a version of a skill to this project. The binding names one concrete version — there is no version-range resolver, so nothing changes under you.",
  "skill.notFound": "No such skill",
  "skill.notFoundBody": "It may have been published somewhere you cannot see.",
  "skill.backToRegistry": "Back to the registry",
  "skill.pinLead":
    "A pinned binding is one this project decided on and does not want moved. Only a binding made here can be pinned — an inherited one is not this project's to hold.",
  "skill.inherited": "Inherited",
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
