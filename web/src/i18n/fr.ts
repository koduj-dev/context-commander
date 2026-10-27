import type { MessageKey } from "./en";

/**
 * French.
 *
 * Translated from en.ts and from nothing else. `npm run i18n:check` fails on
 * a key that no longer exists upstream, on a dropped or invented
 * {placeholder}, on a plural without an `other` form, and on a glossary term
 * that was replaced rather than kept (see glossary.ts — `skill`, `token`,
 * `agent`, `contexte`, `transfert`, MCP).
 *
 * Typography: a no-break space precedes `:` `;` `!` `?`, which French
 * requires and which a plain space gets wrong by letting the mark start a
 * line. It is U+00A0 throughout, deliberately not the narrow variant — the
 * narrow one is correct typography and is missing from enough system fonts
 * that it renders as a box.
 *
 * French runs longer than English, further than German in places, so this is
 * the catalogue to check a layout against.
 */
export const fr: Partial<Record<MessageKey, string>> = {
  "app.name": "Context Commander",
  "app.skipToContent": "Aller au contenu",

  "nav.dashboard": "Tableau de bord",
  "nav.projects": "Projets",
  "nav.organizations": "Organisations",
  "nav.registry": "Registre de skills",
  "nav.administration": "Administration",
  "nav.workspace": "Espace de travail",
  "nav.installation": "Installation",
  "nav.preferences": "Préférences",
  "nav.signOut": "Se déconnecter",

  "find.open": "Rechercher",
  "find.placeholder": "Aller à un projet, un skill, un écran…",
  "find.project": "Projet",
  "find.skill": "Skill",
  "find.screen": "Écran",
  "find.nothing": "Aucun résultat",
  "find.hint":
    "{shortcut} ouvre cette fenêtre. ↑↓ pour se déplacer, Entrée pour y aller, Échap pour fermer.",

  "theme.dark": "Sombre",
  "theme.light": "Clair",
  "theme.label": "Thème",

  "auth.title": "Se connecter",
  "auth.withPassword": "E-mail et mot de passe",
  "auth.withToken": "Token d’accès",
  "auth.email": "E-mail",
  "auth.emailOrUsername": "E-mail ou nom d’utilisateur",
  "auth.directoryUnavailable":
    "L’annuaire n’a pas pu être joint. Cela ne vient pas de votre mot de passe : réessayez dans un instant, ou prévenez la personne qui administre cette installation.",
  "auth.directoryNoEmail":
    "Votre entrée d’annuaire n’a pas d’adresse e-mail ; aucun compte ne peut donc être créé pour elle. Une personne avec les droits d’administration doit en ajouter une.",
  "auth.password": "Mot de passe",
  "auth.failed": "E-mail ou mot de passe incorrect.",
  "auth.tokenLabel": "Token d’accès",
  "auth.tokenLead":
    "Un token d’accès personnel créé avec {command}. Il est échangé contre une session et n’est pas conservé dans ce navigateur.",
  "auth.tokenFailed": "Ce token n’a pas été accepté.",
  "auth.rateLimited": "Trop de tentatives. Patientez un instant puis réessayez.",
  "auth.withProvider": "Se connecter avec {provider}",
  "auth.oidcCancelled": "La connexion a été annulée chez le fournisseur.",
  "auth.oidcExpired":
    "Cette tentative de connexion a expiré ou a été lancée dans une autre fenêtre. Recommencez.",
  "auth.oidcUnavailable":
    "Le fournisseur de connexion n’a pas pu être joint. Cela ne vient pas de votre compte : réessayez dans un instant, ou prévenez la personne qui administre cette installation.",
  "auth.oidcNoEmail":
    "Votre compte chez le fournisseur de connexion n’a pas d’adresse e-mail ; aucun compte ne peut donc être créé pour lui. La personne qui administre le fournisseur doit en ajouter une.",
  "auth.oidcEmailUnverified":
    "Votre adresse e-mail chez le fournisseur de connexion n’est pas vérifiée ; elle ne peut donc pas servir à vous connecter ici. Vérifiez-la là-bas puis réessayez.",
  "auth.oidcFailed": "La connexion via le fournisseur n’a pas abouti. Recommencez.",
  "auth.oidcNoAccount":
    "Vous vous êtes connecté chez le fournisseur, mais aucun compte n’existe pour vous ici, et cette installation n’en crée pas automatiquement. Demandez à une personne avec les droits d’administration d’en créer un à votre adresse e-mail.",
  "auth.oidcDomainNotAllowed":
    "Votre adresse e-mail n’appartient à aucun domaine que cette installation accepte pour la connexion via ce fournisseur.",
  "auth.indistinct":
    "Un mot de passe erroné, une adresse sans compte et un compte sans mot de passe se lisent de la même façon ici. Préciser lequel reviendrait à dire à celui qui devine que la moitié de sa supposition était juste.",
  "setup.title": "Créer le premier compte d’administration",
  "setup.lead":
    "Cette installation n’a encore aucun utilisateur. Au démarrage, le serveur a écrit dans son journal un code d’installation à usage unique : collez-le ici.",
  "setup.code": "Code d’installation",
  "setup.email": "E-mail",
  "setup.name": "Nom affiché",
  "setup.password": "Mot de passe",
  "setup.passwordHint": "Au moins {min} caractères. La longueur est la seule règle.",
  "setup.submit": "Créer le compte d’administration",
  "setup.codeFailed":
    "Ce code d’installation n’est pas valide. Le code actuel se trouve dans le journal du serveur ; un redémarrage en affiche un nouveau.",
  "setup.alreadyDone": "Cette installation a déjà des utilisateurs. Connectez-vous plutôt.",
  "setup.whereIsTheCode":
    "Le code n’existe que dans la mémoire du serveur en cours d’exécution et seulement tant qu’il n’y a aucun utilisateur : il n’atteint donc jamais une sauvegarde et ne peut pas être lu dans la base de données.",
  "account.password": "Mot de passe",
  "account.passwordLead":
    "Sert à se connecter à cette interface. Les agents et la ligne de commande utilisent des tokens d’accès, gérés séparément.",
  "account.currentPassword": "Mot de passe actuel",
  "account.newPassword": "Nouveau mot de passe",
  "account.confirmPassword": "Répétez le nouveau mot de passe",
  "account.setPassword": "Définir le mot de passe",
  "account.changePassword": "Changer le mot de passe",
  "account.passwordSet": "Mot de passe modifié.",
  "account.passwordMismatch": "Les deux nouveaux mots de passe ne correspondent pas.",
  "account.currentWrong": "Le mot de passe actuel est incorrect.",
  "account.noPasswordYet":
    "Ce compte n’a pas encore de mot de passe. Vous êtes connecté avec un token d’accès, ce qui suffit comme preuve pour en définir un.",
  "account.sessions": "Sessions",
  "account.sessionsLead":
    "Où ce compte est connecté. Mettre fin à une session invalide son cookie immédiatement, sur quelque machine qu’il se trouve.",
  "account.sessionCurrent": "Ce navigateur",
  "account.sessionStarted": "Ouverte",
  "account.sessionLastUsed": "Dernière utilisation",
  "account.sessionExpires": "Expire",
  "account.endSession": "Mettre fin à la session",
  "account.noSessions": "Aucune session active.",
  "auth.submit": "Se connecter",
  "auth.signedInAs": "Connecté en tant que {name}",

  "common.loading": "Chargement…",
  "common.retry": "Réessayer",
  "common.cancel": "Annuler",
  "common.close": "Fermer",
  "common.loadMore": "Charger plus",
  "common.showingMore": "{count} affichés · d’autres sont disponibles",
  "common.showingAll": "{count} affichés · liste complète",
  "common.endOfList": "Fin de la liste",
  "common.none": "Aucun",
  "common.never": "Jamais",
  "common.unknown": "Inconnu",
  "common.you": "Vous",
  "common.copy": "Copier",
  "common.copied": "Copié",
  "common.copyFailed": "Copie impossible — sélectionnez la valeur et copiez-la vous-même",

  "error.forbidden.title": "Vous n’avez pas accès à ceci",
  "error.forbidden.body":
    "Votre rôle sur ce projet n’inclut pas la capacité dont cet écran a besoin. Demandez à un propriétaire ou à un administrateur si cela devrait être le cas.",
  "error.notFound.title": "Introuvable",
  "error.notFound.body": "Ceci n’existe pas, ou ne vous est pas visible.",
  "error.conflict.generic": "Cette action n’a pas pu être effectuée",
  "error.unavailable.title": "Le serveur ne répond pas",
  "error.unavailable.body": "Rien n’a été modifié. Réessayez dans un instant.",
  "error.rateLimited.title": "Trop de requêtes",
  "error.rateLimited.body":
    "Ce justificatif envoie des requêtes trop vite. Attendez un instant et réessayez.",
  "error.generic.title": "Quelque chose s’est mal passé",
  "error.crashed.title": "Cet écran a cessé de fonctionner",
  "error.crashed.body":
    "Quelque chose a échoué dans l’interface pendant le rendu de cette page. Rien de ce que vous regardiez n’a été modifié sur le serveur. Recharger suffit en général ; sinon, le détail ci-dessous est ce qu’il faut signaler.",
  "error.crashed.reload": "Recharger",
  "error.notFoundPage.title": "Cette page n’existe pas",
  "error.notFoundPage.body": "Cette adresse ne correspond à rien dans cette interface.",
  "error.backToDashboard": "Aller au tableau de bord",

  "empty.noResults": "Aucun résultat",
  "empty.noResultsBody": "Essayez une autre recherche, ou retirez les filtres.",

  "dashboard.title": "Tableau de bord",
  "dashboard.lead": "Ce qui a changé, où vous travailliez, et si quelque chose vous attend.",
  "dashboard.waiting": "En attente de vous",
  "dashboard.yourProjects": "Vos projets",
  "dashboard.noneWaiting": "Rien ne vous attend",
  "dashboard.noneWaitingBody": "Aucun transfert ouvert, aucun refus qui mérite un coup d’œil.",

  "projects.title": "Projets",
  "projects.lead":
    "Tous les projets dans lesquels vous pouvez agir. Le chemin est la façon dont les gens en parlent ; l’UUID est ce que le système désigne.",
  "projects.filter": "Filtrer les projets…",
  "projects.column.project": "Projet",
  "projects.column.role": "Votre rôle",
  "projects.count": "{count, plural, one {# projet} other {# projets}}",
  "projects.empty": "Vous n’êtes membre d’aucun projet",
  "projects.emptyBody": "Un projet apparaît ici dès que quelqu’un vous y ajoute.",

  "project.identity": "Identité",
  "project.path": "Chemin",
  "project.uuid": "UUID",

  "handoff.current": "Transfert en cours",
  "handoff.summary": "Résumé",
  "handoff.completed": "Travail effectué",
  "handoff.next": "Prochaines actions",
  "handoff.references": "Références",
  "handoff.claim": "Le prendre",
  "handoff.close": "Clore le transfert",
  "handoff.create": "Créer un transfert",
  "handoff.status.open": "Ouvert",
  "handoff.status.claimed": "Pris",
  "handoff.status.closed": "Clos",
  "handoff.claimedBy": "Transfert pris par {name}",
  "handoff.claimedByYou": "Transfert pris par vous",
  "handoff.none": "Aucun transfert ouvert",
  "handoff.noneBody": "Rien n’attend ici d’être repris.",
  "handoff.alreadyClaimed": "Un autre client l’a pris avant vous",
  "handoff.alreadyClaimedBody":
    "C’est la réponse de la coordination, pas une erreur dans votre requête. Relisez le transfert et décidez si le travail vous revient encore.",

  "members.title": "Membres",
  "members.lead":
    "Qui peut agir dans ce projet, et à quel titre. Un justificatif de client n’est pas un membre — le principal derrière lui l’est.",
  "members.column.principal": "Principal",
  "members.column.role": "Rôle",
  "members.column.added": "Ajouté",
  "members.add": "Ajouter un principal",
  "members.count": "{count, plural, one {# membre} other {# membres}}",
  "members.addHelp":
    "Un rôle de projet appartient à un principal — une personne ou un agent. Les deux sont proposés ici si vous administrez cette installation ; sinon le champ prend l’identifiant du principal, qui n’est pas celui du compte.",
  "members.principalPlaceholder": "UUID du principal",

  "picker.choose": "Choisir quelqu’un…",
  "picker.userId": "Identifiant du compte",
  "picker.agent": "agent",
  "picker.noDirectory":
    "Seule la personne qui administre l’installation peut en lister les utilisateurs ; ce champ demande donc un identifiant au lieu de proposer un choix. Demandez-le à un administrateur, ou copiez-le depuis l’écran de compte de la personne concernée.",
  "members.noRemoval":
    "Retirer un membre et changer un rôle ne sont pas implémentés : l’API n’a d’écriture pour ni l’un ni l’autre, cet écran n’en propose donc pas.",

  "skills.title": "Skills",
  "skills.lead":
    "L’ensemble effectif : les liaisons d’installation, d’organisation et de projet composées, la plus étroite l’emportant pour chaque skill. Chaque substitution est visible.",
  "skills.column.skill": "Skill",
  "skills.column.livesAt": "Réside dans",
  "skills.column.version": "Résolu",
  "skills.column.trust": "Confiance",
  "skills.column.state": "État",
  "skills.browse": "Parcourir le registre",
  "skills.install": "Installer un skill",
  "skills.empty": "Aucun skill ne s’applique ici",
  "skills.emptyBody": "Installez-en un depuis le registre, ou publiez-en un depuis ce projet.",

  "registry.title": "Registre de skills",
  "registry.lead":
    "Tout ce que vous pouvez voir : les skills de vos projets, ceux de vos organisations, et ceux de l’étagère de l’installation. Les agents interrogent ce même catalogue.",
  "registry.search": "Rechercher un skill par son nom…",
  "registry.column.latest": "Dernière",
  "registry.column.author": "Auteur",
  "registry.publish": "Nouveau skill",
  "registry.publishLead":
    "Un skill est un texte d’instructions remis à un agent. Publier crée une version immuable — la corriger plus tard veut dire publier de nouveau, et les deux versions subsistent.",
  "registry.publishName": "Nom",
  "registry.publishAuthor": "Auteur",
  "registry.publishOrigin": "Origine",
  "registry.publishScope": "Publier vers",
  "registry.publishContent": "Instructions",
  "registry.published": "Version {version} publiée",
  "registry.publishScopeHelp":
    "Où il réside, c’est-à-dire qui le voit dans le catalogue — et non où il s’applique. Un projet peut installer un skill qui réside n’importe où dans son champ de visibilité.",
  "registry.results": "{count, plural, one {# résultat} other {# résultats}} pour « {query} »",

  "orgs.title": "Organisations",
  "orgs.lead":
    "Le niveau au-dessus de vos projets. Dans une organisation, un skill ou une gate est imposé une seule fois au lieu de projet par projet.",
  "orgs.column.name": "Organisation",
  "orgs.column.role": "Votre rôle",
  "orgs.empty": "Vous n’appartenez à aucune organisation",
  "orgs.emptyBody":
    "L’appartenance à une organisation est liée à un compte de personne ; un principal de service n’en a donc jamais — c’est pour lui la bonne réponse, pas un refus.",
  "orgs.members": "Membres",
  "orgs.membersLead":
    "L’appartenance concerne des comptes de personnes, pas des principaux : un rôle d’organisation parle d’une personne, alors qu’un principal de service agit à l’intérieur d’un projet.",
  "orgs.audit": "Activité",
  "orgs.column.user": "Compte",
  "orgs.select": "Choisissez une organisation",
  "orgs.selectBody": "Choisissez-en une à gauche pour voir ses membres et son activité.",

  "scope.installation": "Installation",
  "scope.organization": "Organisation",
  "scope.project": "Projet",

  "trust.signed": "Signé",
  "trust.unverified": "Non vérifié",

  "role.OWNER": "Propriété",
  "role.ADMIN": "Administration",
  "role.CONTRIBUTOR": "Contribution",
  "role.READER": "Lecture",

  "activity.title": "Activité",
  "activity.lead":
    "Les événements et l’audit sur une seule chronologie. Ils répondent à la même question — que s’est-il passé ici — et les séparer oblige le lecteur à les recoudre.",
  "activity.column.time": "Heure",
  "activity.column.action": "Action",
  "activity.column.resource": "Ressource",
  "activity.column.principal": "Principal",
  "activity.column.result": "Résultat",
  "activity.result.allow": "Autorisé",
  "activity.result.deny": "Refusé",
  "activity.result.error": "Erreur",

  "admin.title": "Administration",
  "admin.lead":
    "L’installation elle-même : qui l’administre, qui s’est vu refuser quelque chose, et les identités auxquelles tout le reste est rattaché.",
  "admin.tab.overview": "Vue d’ensemble",
  "admin.tab.people": "Personnes",
  "admin.tab.agents": "Agents",
  "admin.column.registeredBy": "Enregistré par",
  "admin.unattributed": "non consigné",
  "admin.registeredByHelp":
    "Qui a créé l’agent. C’est une trace de sa provenance et cela ne lui accorde rien : un agent n’atteint un projet que par sa propre appartenance à celui-ci.",
  "admin.tab.usage": "Utilisation",
  "admin.usage": "Utilisation",
  "admin.usageLead":
    "À quel point cette installation est réellement utilisée, et ce qu’elle contient. Des dénombrements uniquement — qui administre l’installation l’administre et ne lit pas son contenu, rien ici ne nomme donc un projet, un chemin ni une personne.",
  "admin.usage.content": "Ce qu’elle contient",
  "admin.usage.coordination": "Coordination",
  "admin.usage.activity": "Activité",
  "admin.usage.credentials": "Justificatifs",
  "admin.usage.inOrganizations":
    "projets, dans {count, plural, one {# organisation} other {# organisations}}",
  "admin.usage.andAgents": "personnes, et {count, plural, one {# agent} other {# agents}}",
  "admin.usage.acrossArtifacts": "sur {count, plural, one {# artefact} other {# artefacts}}",
  "admin.usage.ofIssued":
    "justificatifs utilisés, sur {count, plural, one {# émis} other {# émis}}",
  "admin.usage.artifacts": "Artefacts",
  "admin.usage.versions": "Versions",
  "admin.usage.stored": "Stocké",
  "admin.usage.storedHelp":
    "La taille de toutes les versions d’artefact jamais écrites. Pas l’occupation disque — la base de données ne voit pas le magasin d’objets, et un chiffre étiqueté « disque » venant de SQL serait une conjecture.",
  "admin.usage.secrets": "Secrets",
  "admin.usage.conversations": "Conversations",
  "admin.usage.historyObjects": "Objets enregistrés",
  "admin.usage.handoffsOpen": "Ouverts",
  "admin.usage.handoffsClaimed": "Pris",
  "admin.usage.handoffsClosed": "Clos",
  "admin.usage.handoffs": "Transferts",
  "admin.usage.skills": "Skills",
  "admin.usage.skillVersions": "Versions de skill",
  "admin.usage.skillBindings": "Liaisons de skill",
  "admin.usage.gateBindings": "Liaisons de gate",
  "admin.usage.events": "Événements",
  "admin.usage.auditRecords": "Entrées d’audit",
  "admin.usage.securityEvents": "Événements de sécurité",
  "admin.usage.newestActivity": "Dernière activité",
  "admin.usage.oldestProject": "Premier projet",
  "admin.usage.active": "Actifs",
  "admin.usage.revoked": "Révoqués",
  "admin.usage.expired": "Expirés",
  "admin.usage.usedRecentlyHelp":
    "Le seul chiffre qui répond à « est-ce utilisé » plutôt qu’à « qu’y a-t-il de stocké ». Un justificatif qui n’a fait aucune requête depuis une semaine est soit terminé, soit oublié.",
  "admin.usage.quiet": "Rien ne s’est encore passé",
  "admin.usage.quietBody":
    "Aucun événement enregistré. Une installation qui a des projets mais aucun événement est une installation dans laquelle rien n’a écrit.",
  "admin.usage.weekly": "{count, plural, one {# dernière semaine} other {# dernières semaines}}",
  "admin.usage.week": "Semaine",
  "admin.usage.people": "Personnes",
  "admin.usage.agents": "Agents",
  "admin.usage.handedOff": "Transmis",
  "admin.usage.claimed": "Pris",
  "admin.usage.byOtherClient": "Pris par un autre client",
  "admin.usage.medianToClaim": "Délai médian de prise",
  "admin.usage.weeklyHelp":
    "Les semaines commencent le lundi, en UTC. Personnes et agents comptent une fois par semaine s’ils ont fait une requête authentifiée ; cet écran peut les compter, jamais les nommer.",
  "admin.usage.loop": "Boucle de transfert",
  "admin.usage.loopHelp":
    "Mesuré à partir de ce que le serveur enregistre : quand un transfert a été créé, pris et clos, et par quel client. Que l’agent suivant ait compris ce qu’on lui a transmis, le serveur ne peut pas le voir.",
  "admin.usage.ofClaimed": "sur {count, plural, one {# pris} other {# pris}}",
  "admin.usage.ofProjects": "sur {count, plural, one {# projet} other {# projets}}",
  "admin.usage.projectsWithTakeover": "Projets avec une reprise",
  "admin.usage.firstTakeover": "Du début du projet à la première reprise (médiane)",
  "admin.usage.p90ToClaim": "Délai de prise, 90e centile",
  "admin.usage.claimToClose": "De la prise à la clôture (médiane)",
  "admin.usage.staleOpen": "Ouverts depuis plus de 7 jours",
  "admin.usage.notYet": "Pas encore mesuré",
  "admin.tab.security": "Journal de sécurité",
  "admin.admins": "Administration de l’installation",
  "admin.adminsLead":
    "Qui administre l’installation peut lire dans tous les projets. Accorder ou retirer ce droit ne se fait pas ici, volontairement — c’est une opération de la ligne de commande.",
  "admin.column.user": "Compte",
  "admin.column.role": "Rôle",
  "admin.column.granted": "Accordé",
  "admin.createUser": "Créer un compte",
  "admin.initialPassword": "Mot de passe initial (facultatif)",
  "admin.initialPasswordLead":
    "Avec un mot de passe, la personne peut se connecter tout de suite. Sans, il lui faudra un token ou un mot de passe défini plus tard. Au moins {min} caractères.",
  "admin.setPassword": "Définir le mot de passe",
  "admin.setPasswordFor": "Définir un mot de passe pour {name}",
  "admin.passwordSetFor": "Mot de passe défini. {name} peut s’en servir dès maintenant.",
  "admin.setPasswordLead":
    "C’est le pouvoir de se connecter à sa place — comme lui délivrer un token, ce que cet écran fait déjà. Transmettez-lui le mot de passe par un autre canal ; il n’est affiché nulle part ensuite.",
  "admin.createUserLead": "Une personne qui peut détenir un token et être ajoutée à des projets.",
  "admin.email": "Adresse e-mail",
  "admin.displayName": "Nom affiché",
  "admin.createService": "Créer un principal de service",
  "admin.createServiceLead":
    "Une identité non humaine — une tâche d’intégration continue, un agent planifié. Elle agit dans les projets et apparaît dans l’audit comme n’importe qui d’autre.",
  "admin.name": "Nom",
  "admin.description": "Description",
  "admin.users": "Personnes",
  "admin.usersLead":
    "Toutes les personnes ayant une identité sur cette installation. Leurs rôles de projet vivent sur le projet, pas ici.",
  "admin.servicePrincipals": "Agents",
  "admin.servicePrincipalsLead":
    "Des identités non humaines — une tâche d’intégration continue, un agent planifié. Une telle identité agit dans les projets et apparaît dans l’audit exactement comme une personne, et c’est tout l’intérêt : ce qu’un agent a fait est imputable à l’agent, non à qui l’a configuré.",
  "admin.column.name": "Nom",
  "admin.column.created": "Créé",
  "admin.noUsers": "Personne d’autre que vous pour l’instant",
  "admin.noServicePrincipals": "Aucun agent enregistré",
  "admin.noServicePrincipalsBody":
    "Enregistrez-en un, puis émettez-lui un token — c’est ce token qu’une tâche d’intégration continue ou un agent planifié présente en appelant ce serveur.",
  "admin.issueFor": "Émettre un token",
  "admin.tokens": "Tokens",
  "admin.tokensLead":
    "Tous les justificatifs émis pour cette identité. Une révocation prend effet à la requête suivante et ne change aucune appartenance.",
  "admin.noTokens": "Aucun token émis",
  "admin.confirmRevokeFor":
    "Révoquer ce justificatif ? Ce qui l’utilise cesse aussitôt de fonctionner, et s’il s’agit d’un agent en cours d’exécution, il échouera au milieu de sa tâche.",
  "admin.issueForLead":
    "Un token pour cette identité, pas pour vous. Il est affiché une seule fois, et qui le détient agit en tant que cette identité.",
  "admin.security": "Journal de sécurité",
  "admin.securityLead":
    "Ce que le serveur a refusé : un token qui ne s’est pas résolu, une capacité refusée, un appelant limité. Tenu à part de l’audit de projet à dessein — un refus porte souvent sur un projet que l’appelant ne peut même pas voir, et le motif qui compte vraiment, un justificatif qui sonde plusieurs projets, est invisible depuis l’intérieur de n’importe lequel d’entre eux.",
  "admin.securityNote":
    "Une connexion réussie n’est pas un refus et n’est pas enregistrée ici. Ce qu’un justificatif a fait en dernier figure sur le token lui-même, comme heure de dernière utilisation.",
  "admin.column.kind": "Type",
  "admin.column.reason": "Motif",
  "admin.column.capability": "Capacité",
  "admin.securityEmpty": "Rien d’enregistré",
  "admin.securityEmptyBody":
    "Aucun refus, aucun justificatif inconnu, rien à regarder. C’est le bon cas.",

  "tokens.title": "Tokens d’accès",
  "tokens.lead":
    "Les justificatifs que vous utilisez depuis une CLI, un éditeur ou un client MCP. Chacun n’est qu’à vous — il n’y a aucun moyen d’émettre ici un token pour quelqu’un d’autre.",
  "tokens.column.type": "Client",
  "tokens.column.created": "Créé",
  "tokens.column.lastUsed": "Dernière utilisation",
  "tokens.column.expires": "Expire",
  "tokens.issue": "Émettre un token",
  "tokens.clientType": "Type de client",
  "tokens.expiry": "Validité",
  "tokens.expiry.never": "Sans expiration",
  "tokens.expiry.30d": "30 jours",
  "tokens.expiry.90d": "90 jours",
  "tokens.expiry.1y": "1 an",
  "tokens.revoke": "Révoquer",
  "tokens.revoked": "Révoqué",
  "tokens.expired": "Expiré",
  "tokens.active": "Actif",
  "tokens.empty": "Aucun token pour l’instant",
  "tokens.emptyBody": "Émettez-en un pour connecter un agent à cette installation.",
  "tokens.justIssued": "Copiez ce token maintenant",
  "tokens.justIssuedBody":
    "Seule son empreinte est conservée, c’est donc la seule et unique fois où il peut être affiché. Fermer ce panneau ne le révoque pas — mais signifie qu’il ne sera plus jamais lisible.",
  "tokens.confirmRevoke":
    "Révoquer ce justificatif ? Tout ce qui l’utilise encore cesse aussitôt de fonctionner.",

  "prefs.title": "Préférences",
  "prefs.lead":
    "Comment cette installation s’adresse à vous. Ces réglages suivent votre compte, pas ce navigateur.",
  "prefs.language": "Langue de l’interface",
  "prefs.languageHelp":
    "Tout ce que le produit écrit. Le contenu écrit par d’autres personnes n’est jamais traduit.",
  "prefs.dateFormat": "Format de date",
  "prefs.timeFormat": "Format d’heure",
  "prefs.timeZone": "Fuseau horaire",
  "prefs.showUTC": "Afficher aussi UTC",
  "prefs.numberFormat": "Format des nombres",
  "prefs.storageUnits": "Unités de stockage",
  "prefs.units.decimal": "Go · décimal, base 1000",
  "prefs.units.binary": "Gio · binaire, base 1024",
  "prefs.notStored":
    "Les préférences ne sont pas encore stockées sur le serveur ; elles vivent pour l’instant dans ce navigateur.",
  "prefs.section.language": "Langue",
  "prefs.section.dates": "Date et heure",
  "prefs.section.numbers": "Nombres et unités",
  "prefs.dateFormatHelp":
    "Comment une date s’écrit. 03-09-2026 désigne deux jours différents selon qui le lit.",
  "prefs.timeFormatHelp": "Convention de l’horloge.",
  "prefs.timeZoneHelp": "Les horodatages sont stockés en UTC et affichés dans ce fuseau.",
  "prefs.showUTCHelp": "Pour les écrans d’audit et de sécurité, où une référence commune compte.",
  "prefs.storageUnitsHelp":
    "Comment les tailles en octets s’écrivent. La valeur stockée ne change jamais — seule sa lecture change.",
  "prefs.date.locale": "Suivre la langue",
  "prefs.date.ymd": "AAAA-MM-JJ",
  "prefs.date.dmy": "JJ-MM-AAAA",
  "prefs.date.mdy": "MM-JJ-AAAA",
  "prefs.time.locale": "Suivre la langue",
  "prefs.time.h24": "24 heures",
  "prefs.time.h12": "12 heures",
  "prefs.tz.device": "Suivre cet appareil ({zone})",
  "prefs.on": "Activé",
  "prefs.off": "Désactivé",
  "prefs.serverFormat":
    "Le serveur n’envoie jamais de chaîne formatée. Il envoie la valeur ; l’interface la lit.",

  "dashboard.noProjects": "Vous n’appartenez encore à aucun projet",
  "project.tab.overview": "Vue d’ensemble",
  "project.tab.members": "Membres",
  "project.tab.skills": "Skills",
  "project.tab.activity": "Activité",
  "project.notBuilt.title": "Pas encore implémenté",
  "project.notBuilt.artifacts":
    "Les artefacts, les notes, les politiques et les secrets ont des écrans maquettés, mais aucun chemin de lecture dans l’API pour l’instant.",

  "context.title": "Contexte",
  "context.lead":
    "Exactement ce qu’un agent reçoit lorsqu’il démarre ici — la même vue assemblée que renvoie MCP, rendue pour une personne. Si un agent se comporte étrangement, c’est le premier endroit à regarder.",
  "context.role": "Agit en tant que",
  "context.capabilities": "Peut",
  "context.assembled": "Assemblé",
  "context.budget": "Budget",
  "context.budgetUsed": "{used} sur {limit}",
  "context.budgetLead":
    "Un paquet a une limite de taille, et ce qui n’y entre pas est nommé au lieu d’être coupé. Un agent incapable de distinguer « il n’y a rien d’autre » de « il y en avait plus et vous ne l’avez pas reçu » agira avec assurance sur une moitié d’image.",
  "context.truncated": "Laissé de côté",
  "context.nothingTruncated": "Rien n’a été laissé de côté",
  "context.nothingTruncatedBody": "Tout ce que ce projet contient est entré dans le budget.",
  "context.column.what": "Quoi",
  "context.column.ref": "Lequel",
  "context.column.why": "Pourquoi",
  "context.included": "Inclus",
  "context.artifacts": "{count, plural, one {# artefact} other {# artefacts}}",
  "context.skills": "{count, plural, one {# skill} other {# skills}}",
  "context.recentEvents": "Événements récents",
  "context.noHandoff": "Aucun transfert dans le paquet",
  "context.noHandoffBody": "Il est dit à un agent qui démarre ici qu’il n’y a rien à poursuivre.",
  "context.budgetLimit": "Limite de taille",
  "context.apply": "Réassembler",

  "artifacts.title": "Artefacts",
  "artifacts.lead":
    "Le magasin partagé que les agents lisent et écrivent. Chaque écriture crée une nouvelle version ; rien n’est écrasé sur place, une mauvaise écriture se dépasse donc en lisant la précédente au lieu de se récupérer depuis une sauvegarde.",
  "artifacts.column.path": "Chemin",
  "artifacts.column.version": "Version",
  "artifacts.column.size": "Taille",
  "artifacts.column.hash": "Empreinte du contenu",
  "artifacts.empty": "Aucun artefact pour l’instant",
  "artifacts.emptyBody": "Le premier apparaît dès qu’un agent écrit via l’API, la CLI ou MCP.",
  "artifacts.history": "Versions",
  "artifacts.content": "Contenu",
  "artifacts.select": "Choisissez un artefact",
  "artifacts.selectBody": "Choisissez un chemin à gauche pour voir ses versions et son contenu.",
  "artifacts.binary": "Ce n’est pas du texte",
  "artifacts.binaryBody":
    "Le type de contenu indique que ce n’est pas du texte, il n’est donc pas affiché. La version, la taille et l’empreinte ci-dessus sont ce que cet écran peut en dire.",
  "artifacts.viewing": "Affichage de la version {version}",
  "artifacts.latest": "Dernière",
  "artifacts.edit": "Modifier",
  "artifacts.new": "Nouvel artefact",
  "artifacts.save": "Enregistrer",
  "artifacts.delete": "Supprimer",
  "artifacts.confirmDelete":
    "Supprimer {path} ? Ses versions partent avec lui, et tout ce qui lit ce chemin commencera à échouer.",
  "artifacts.editingFrom": "Modification à partir de la version {version}",
  "artifacts.editingNew": "Un nouvel artefact à ce chemin",
  "artifacts.newPath": "Chemin",
  "artifacts.newPathHelp": "notes/decisions.md",
  "artifacts.contentType": "Type de contenu",
  "artifacts.staleWrite": "Quelqu’un a écrit une version plus récente pendant votre modification",
  "artifacts.staleWriteBody":
    "Votre enregistrement a été refusé plutôt qu’appliqué — l’appliquer aurait effacé le travail de cette personne. Rechargez la version la plus récente, reportez-y votre modification, puis enregistrez de nouveau.",
  "artifacts.reload": "Recharger la version la plus récente",
  "artifacts.concurrency":
    "Un enregistrement nomme la version dont il est parti. Si quelqu’un d’autre a écrit entretemps, le serveur le refuse au lieu de l’écraser.",
  "artifacts.binaryNoEdit":
    "Ce n’est pas du texte, cela ne peut donc pas être modifié ici. Les agents l’écrivent via l’API, la CLI et MCP.",

  "secrets.title": "Secrets",
  "secrets.lead":
    "Des noms et des références seulement. Une valeur entre une fois et ne ressort jamais par cette interface — ce qu’un projet obtient est une référence qu’il remet à une gate.",
  "secrets.column.name": "Nom",
  "secrets.add": "Ajouter un secret",
  "secrets.value": "Valeur",
  "secrets.delete": "Supprimer",
  "secrets.empty": "Aucun secret dans ce projet",
  "secrets.emptyBody": "Ajoutez-en un ici, ou avec la CLI.",
  "secrets.neverShown":
    "La valeur est écrite une fois et n’est ensuite plus jamais lisible, par personne, via aucun adaptateur. La supprimer et la rajouter est le seul moyen de la changer.",
  "secrets.confirmDelete":
    "Supprimer le secret {name} ? Tout ce qui utilise sa référence commencera à échouer.",

  "gates.title": "Politiques",
  "gates.lead":
    "Les gates liées à ce projet et celles qu’il hérite. Une gate s’exécute sur un hook et peut refuser l’opération — le refus apparaît sur la chronologie d’activité.",
  "gates.column.gate": "Gate",
  "gates.column.hook": "Hook",
  "gates.column.scope": "Liée au niveau",
  "gates.column.enabled": "État",
  "gates.enabled": "Appliquée",
  "gates.disabled": "Désactivée",
  "gates.empty": "Aucune gate ne s’applique ici",
  "gates.emptyBody": "Rien n’intercepte les opérations de ce projet.",
  "gates.bind": "Lier une gate",
  "gates.bindLead":
    "Seules les gates réellement enregistrées par le serveur peuvent être liées ; une liaison nommant une gate non enregistrée fait échouer l’opération entière au lieu d’être ignorée, cette liste est donc ce qui existe.",
  "gates.gateName": "Gate",
  "gates.hook": "Hook",
  "gates.unbind": "Délier",
  "gates.confirmUnbind":
    "Délier {gate} de {hook} ? Les opérations qu’elle refusait commenceront à passer.",
  "gates.inherited": "Héritée",

  "skills.override": "Substituer",
  "skills.overrideLead":
    "Lier une autre version à ce projet, masquant ce qu’un niveau plus large a imposé. Une liaison marquée non substituable le refuse — c’est tout l’objet de cet indicateur.",
  "skills.overrideVersion": "Version à lier",

  "orgs.create": "Nouvelle organisation",
  "orgs.createLead":
    "Vous en devenez la première personne propriétaire. Des projets et des skills pourront ensuite y être rattachés.",
  "orgs.name": "Nom",
  "orgs.addMember": "Ajouter un membre",
  "orgs.addMemberLead":
    "Un rôle d’organisation parle d’une personne ; ce champ nomme donc un compte et non un principal — un agent agit à l’intérieur d’un projet et ne peut pas détenir un tel rôle.",
  "orgs.userId": "Identifiant du compte",

  "settings.title": "Paramètres",
  "settings.lead": "Ce qu’est ce projet, et ce qui peut en être changé depuis ici.",
  "settings.identity": "Identité",
  "settings.visibility": "Visibilité",
  "settings.encryption": "Chiffrement",
  "settings.encryptionBody":
    "Le contenu est chiffré au repos sous une clé propre à ce projet, elle-même enveloppée par la clé racine de l’installation. Les corps de transfert et les secrets passent par cette enveloppe ; les octets des artefacts vont dans le magasin d’objets.",
  "settings.danger": "Irréversible",
  "settings.noWrites":
    "Rien sur cet écran ne peut être changé ici : l’API n’a ni mise à jour ni suppression pour un projet. Renommer, changer la visibilité et supprimer sont des opérations de la ligne de commande, et un bouton qui ouvrirait une boîte de dialogue pour échouer ensuite serait pire que son absence.",
  "settings.connect": "Connecter un agent",
  "settings.connectLead":
    "Pointez un client MCP vers ce serveur avec un token d’accès personnel. Le projet est nommé à chaque appel, un seul token atteint donc tous les projets dont vous êtes membre.",
  "settings.mcpEndpoint": "Point d’accès MCP",
  "settings.restEndpoint": "REST",

  "dashboard.waitingLead":
    "Les projets où un transfert est ouvert ou pris — du travail laissé par quelqu’un pour la suite.",

  "history.title": "Conversations",
  "history.lead":
    "Ce que les clients ont enregistré de leurs propres sessions, pour qu’un transfert puisse désigner l’endroit où le travail a réellement eu lieu. Rien ici n’est écrit par cette interface — ce sont les clients eux-mêmes qui l’alimentent.",
  "history.column.conversation": "Conversation",
  "history.column.started": "Démarrée",
  "history.column.by": "Démarrée par",
  "history.column.type": "Type",
  "history.column.recorded": "Enregistré",
  "history.objects": "Objets enregistrés",
  "history.lineage": "Filiation",
  "history.lineageLead":
    "Comment un objet est issu d’un autre — un résumé à partir d’une transcription, une compaction à partir d’un résumé. C’est ce qui empêche de prendre un objet dérivé pour un original.",
  "history.column.from": "Dérivé de",
  "history.column.to": "A produit",
  "history.column.kind": "Relation",
  "history.select": "Choisissez une conversation",
  "history.selectBody": "Choisissez-en une à gauche pour voir ce qui y a été enregistré.",
  "history.empty": "Aucune conversation enregistrée",
  "history.emptyBody":
    "Un client les enregistre au fil de son travail. Rien n’a encore alimenté l’historique de ce projet.",
  "history.noObjects": "Rien n’a été enregistré dans cette conversation",
  "history.noLineage": "Aucune dérivation",
  "history.noLineageBody":
    "Tous les objets présents ici ont été enregistrés directement, aucun n’est issu d’un autre.",
  "history.notText": "Ce n’est pas du texte",
  "history.notTextBody":
    "Un objet d’historique est ce que le client a enregistré, et ces octets ne sont pas de l’UTF-8 valide. L’empreinte et le type ci-dessus sont ce que cet écran peut en dire.",

  "events.title": "Événements",
  "events.lead":
    "Le flux de la boîte d’envoi. Un événement est ce que consomment les autres systèmes ; l’entrée d’audit à côté est ce qu’une personne lit.",
  "events.column.seq": "N°",
  "events.column.type": "Type",
  "events.column.resource": "Ressource",
  "events.column.payload": "Charge utile",
  "events.empty": "Aucun événement pour l’instant",

  "handoff.createLead":
    "Ce dont le prochain agent a besoin pour continuer. Écrit ici par une personne ; les agents écrivent la même chose via MCP.",
  "handoff.summaryHelp": "Une ligne. De quoi ce travail traite.",
  "handoff.completedHelp": "Ce qui est déjà fait, pour que personne ne le refasse.",
  "handoff.nextHelp": "Quoi faire ensuite, assez concrètement pour s’y mettre.",
  "handoff.confirmClose":
    "Clore ce transfert ? Il cesse d’être le transfert en cours et personne ne peut plus le prendre.",

  "projects.create": "Nouveau projet",
  "projects.createLead":
    "Un chemin et un nom. Le chemin est la façon dont les gens y font référence et ne peut plus être changé ensuite ; vous en devenez la personne propriétaire.",
  "projects.path": "Chemin",
  "projects.pathHelp": "espace-de-noms/identifiant",
  "projects.name": "Nom affiché",
  "projects.created": "{path} créé",

  "handoff.history": "Transferts précédents",
  "handoff.column.created": "Créé",
  "handoff.column.status": "État",

  "skill.versions": "Versions",
  "skill.versionsLead":
    "Toutes les versions jamais publiées sous ce nom. Une version publiée est immuable — publier de nouveau ajoute une ligne ici, cela n’en modifie jamais une.",
  "skill.content": "Contenu",
  "skill.contentLead": "Ce qu’un agent reçoit lorsque cette version est résolue.",
  "skill.provenance": "Provenance",
  "skill.checksum": "Empreinte",
  "skill.capabilities": "Capacités déclarées",
  "skill.author": "Auteur",
  "skill.origin": "Origine",
  "skill.promote": "Promouvoir",
  "skill.promoteLead":
    "Déplacer le skill vers une étagère plus large afin que davantage de projets le voient. Dans un seul sens : le restreindre de nouveau le retirerait sous des liaisons qui étaient légitimes au moment où elles ont été faites.",
  "skill.livesAtNow": "Réside dans",
  "skill.alreadyWidest": "Déjà sur l’étagère de l’installation",
  "skill.alreadyWidestBody":
    "C’est le niveau le plus large qui existe, il n’y a donc nulle part plus loin où le déplacer.",
  "skill.newVersion": "Publier une nouvelle version",
  "skill.newVersionLead":
    "Une version publiée est immuable ; corriger un skill veut donc dire publier la suivante. Les deux subsistent, et ce qui est épinglé à l’ancienne continue de recevoir l’ancienne jusqu’à ce qu’on le repointe.",
  "skill.newVersionFrom": "À partir de la v{version}",
  "skill.publishedVersion": "v{version} publiée",
  "skill.promoteTo": "Déplacer vers",
  "skill.promoted": "Réside désormais au niveau {scope}",
  "skill.install": "Installer",
  "skill.installed": "Installé",
  "skill.installLead":
    "Épingler une version d’un skill à ce projet. La liaison nomme une version concrète — il n’y a pas de résolveur d’intervalles de versions, rien ne change donc sous vos pieds.",
  "skill.notFound": "Ce skill n’existe pas",
  "skill.notFoundBody": "Il a peut-être été publié à un endroit que vous ne pouvez pas voir.",
  "skill.backToRegistry": "Retour au registre",
  "skill.pinLead":
    "Une liaison épinglée est une liaison pour laquelle ce projet a tranché et qu’il ne veut pas voir bouger. Seule une liaison faite ici peut être épinglée — une liaison héritée n’appartient pas à ce projet.",
  "skill.inherited": "Héritée",
  "skill.pin": "Épingler",
  "skill.unpin": "Détacher",
  "skill.pinned": "Épinglée",

  "registry.livesAt": "Réside dans",
  "registry.empty": "Le catalogue est vide",
  "registry.emptyBody":
    "Publiez un skill depuis un projet, ou promouvez-en un vers l’étagère de l’installation.",

  "activity.empty": "Rien ne s’est encore passé ici",
  "activity.emptyBody":
    "Les lectures et les écritures via l’API, la CLI et MCP aboutissent toutes sur cette chronologie.",
};
