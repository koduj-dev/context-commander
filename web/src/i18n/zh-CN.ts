import type { MessageKey } from "./en";

/**
 * Simplified Chinese.
 *
 * Translated from en.ts and from nothing else. `npm run i18n:check` fails on
 * a key that no longer exists upstream, on a dropped or invented
 * {placeholder}, on a plural without an `other` form, and on a glossary term
 * that was replaced rather than kept (glossary.ts — `skill`, `token`,
 * `agent`, 上下文, 交接, MCP).
 *
 * One plural category, `other`, and that is the other end of the range from
 * Polish: a renderer that assumed an `one` form exists would produce an empty
 * string for every count here. The blocks are written with `other` alone
 * rather than with a redundant `one`, so the catalogue states what the
 * language actually has.
 *
 * Two things this language needs that the Latin ones do not, both handled in
 * app.css rather than here: a font stack that covers Han characters (Inter
 * has none), and typography that undoes Latin assumptions — negative
 * tracking, tight line-height, and a `ch`-based truncation width that cuts
 * Chinese at half the intended length.
 *
 * Terminology follows the field rather than the dictionary: 上下文 for
 * context is a real Chinese term and is used; skill, token, agent and MCP are
 * what Chinese-language documentation in this field actually writes, so they
 * stay in Latin script. 交接 for handoff is the natural word for handing work
 * over and is used consistently.
 */
export const zhCN: Partial<Record<MessageKey, string>> = {
  "app.name": "Context Commander",
  "app.skipToContent": "跳至内容",

  "nav.dashboard": "概览",
  "nav.projects": "项目",
  "nav.organizations": "组织",
  "nav.registry": "Skill 注册表",
  "nav.administration": "管理",
  "nav.workspace": "工作区",
  "nav.installation": "安装实例",
  "nav.preferences": "偏好设置",
  "nav.signOut": "退出登录",

  "find.open": "搜索",
  "find.placeholder": "跳转到项目、skill 或页面…",
  "find.project": "项目",
  "find.skill": "Skill",
  "find.screen": "页面",
  "find.nothing": "没有匹配项",
  "find.hint": "{shortcut} 打开此窗口。↑↓ 移动，Enter 前往，Esc 关闭。",

  "theme.dark": "深色",
  "theme.light": "浅色",
  "theme.label": "主题",

  "auth.title": "登录",
  "auth.withPassword": "邮箱和密码",
  "auth.withToken": "访问 token",
  "auth.email": "邮箱",
  "auth.password": "密码",
  "auth.failed": "邮箱或密码不正确。",
  "auth.tokenLabel": "访问 token",
  "auth.tokenLead": "用 {command} 创建的个人访问 token。它会被换成一个会话，不会保存在此浏览器中。",
  "auth.tokenFailed": "该 token 未被接受。",
  "auth.rateLimited": "尝试次数过多。请稍候再试。",
  "auth.indistinct":
    "密码错误、地址没有对应账号、账号没有设置密码，在这里读起来完全一样。说清是哪一种，等于告诉正在猜测的人他猜对了一半。",
  "setup.title": "创建第一个管理员账号",
  "setup.lead": "此安装还没有任何用户。服务器启动时已把一次性安装码写入自己的日志——请粘贴到这里。",
  "setup.code": "安装码",
  "setup.email": "邮箱",
  "setup.name": "显示名称",
  "setup.password": "密码",
  "setup.passwordHint": "至少 {min} 个字符。长度是唯一的规则。",
  "setup.submit": "创建管理员账号",
  "setup.codeFailed": "该安装码无效。当前的安装码在服务器日志里；重启服务器会打印一个新的。",
  "setup.alreadyDone": "此安装已有用户，请改为登录。",
  "setup.whereIsTheCode":
    "安装码只存在于正在运行的服务器内存中，而且只在没有用户时存在——它不会进入任何备份，也无法从数据库中读出。",
  "account.password": "密码",
  "account.passwordLead": "用于登录此界面。agent 和命令行使用访问 token，单独管理。",
  "account.currentPassword": "当前密码",
  "account.newPassword": "新密码",
  "account.confirmPassword": "再次输入新密码",
  "account.setPassword": "设置密码",
  "account.changePassword": "修改密码",
  "account.passwordSet": "密码已修改。",
  "account.passwordMismatch": "两次输入的新密码不一致。",
  "account.currentWrong": "当前密码不正确。",
  "account.noPasswordYet":
    "此账号还没有密码。你是用访问 token 登录的，这已足以作为设置密码的凭据。",
  "account.sessions": "会话",
  "account.sessionsLead":
    "此账号在哪些地方处于登录状态。结束一个会话会立即使它的 cookie 失效，无论它在哪台机器上。",
  "account.sessionCurrent": "此浏览器",
  "account.sessionStarted": "开始于",
  "account.sessionLastUsed": "最近使用",
  "account.sessionExpires": "到期",
  "account.endSession": "结束会话",
  "account.noSessions": "没有活动的会话。",
  "auth.submit": "登录",
  "auth.signedInAs": "已登录为 {name}",

  "common.loading": "加载中…",
  "common.retry": "重试",
  "common.cancel": "取消",
  "common.close": "关闭",
  "common.loadMore": "加载更多",
  "common.showingMore": "已显示 {count} 条 · 还有更多",
  "common.showingAll": "已显示 {count} 条 · 全部",
  "common.endOfList": "列表结束",
  "common.none": "无",
  "common.never": "从未",
  "common.unknown": "未知",
  "common.you": "你",
  "common.copy": "复制",
  "common.copied": "已复制",
  "common.copyFailed": "无法复制 — 请选中该值自行复制",

  "error.forbidden.title": "你没有访问权限",
  "error.forbidden.body":
    "你在该项目中的角色不包含此页面所需的能力。如果你认为应该拥有，请联系所有者或管理员。",
  "error.notFound.title": "未找到",
  "error.notFound.body": "它不存在，或者对你不可见。",
  "error.conflict.generic": "该操作无法完成",
  "error.unavailable.title": "服务器没有响应",
  "error.unavailable.body": "什么都没有改动。请稍后重试。",
  "error.rateLimited.title": "请求过多",
  "error.rateLimited.body": "该凭据的请求速度过快。请稍等一会儿再试。",
  "error.generic.title": "出了点问题",
  "error.crashed.title": "此页面已停止工作",
  "error.crashed.body":
    "界面在绘制此页面时出错。你刚才看到的内容在服务器上没有任何改动。重新加载通常就能恢复；如果无效，下面的细节就是需要上报的内容。",
  "error.crashed.reload": "重新加载",
  "error.notFoundPage.title": "没有这个页面",
  "error.notFoundPage.body": "该地址在此界面中不对应任何内容。",
  "error.backToDashboard": "前往概览",

  "empty.noResults": "没有匹配项",
  "empty.noResultsBody": "换一个搜索词，或清除筛选条件。",

  "dashboard.title": "概览",
  "dashboard.lead": "有什么变化、你之前在哪里工作，以及是否有事情在等你。",
  "dashboard.waiting": "等待你处理",
  "dashboard.yourProjects": "你的项目",
  "dashboard.noneWaiting": "没有事情在等你",
  "dashboard.noneWaitingBody": "没有未完成的交接，也没有值得一看的拒绝记录。",

  "projects.title": "项目",
  "projects.lead":
    "你能在其中操作的所有项目。路径是人们口头称呼它的方式；UUID 才是系统真正指向的东西。",
  "projects.filter": "筛选项目…",
  "projects.column.project": "项目",
  "projects.column.role": "你的角色",
  "projects.count": "{count, plural, other {# 个项目}}",
  "projects.empty": "你还不是任何项目的成员",
  "projects.emptyBody": "有人把你加入某个项目后，它就会出现在这里。",

  "project.identity": "标识",
  "project.path": "路径",
  "project.uuid": "UUID",

  "handoff.current": "当前交接",
  "handoff.summary": "摘要",
  "handoff.completed": "已完成的工作",
  "handoff.next": "接下来要做的事",
  "handoff.references": "引用",
  "handoff.claim": "接手",
  "handoff.close": "关闭交接",
  "handoff.create": "创建交接",
  "handoff.status.open": "未接手",
  "handoff.status.claimed": "已接手",
  "handoff.status.closed": "已关闭",
  "handoff.claimedBy": "交接已由 {name} 接手",
  "handoff.claimedByYou": "交接已由你接手",
  "handoff.none": "没有未完成的交接",
  "handoff.noneBody": "这里没有等待他人接手的工作。",
  "handoff.alreadyClaimed": "另一个客户端先接手了",
  "handoff.alreadyClaimedBody":
    "这是协调机制给出的答案，不是你的请求出错。重新读一遍交接内容，再判断这件事是否仍然该由你来做。",

  "members.title": "成员",
  "members.lead":
    "谁能在这个项目中操作，以及以什么身份。客户端凭据本身不是成员 — 它背后的 principal 才是。",
  "members.column.principal": "Principal",
  "members.column.role": "角色",
  "members.column.added": "加入时间",
  "members.add": "添加 principal",
  "members.count": "{count, plural, other {# 名成员}}",
  "members.addHelp":
    "项目角色属于某个 principal — 可以是人，也可以是 agent。如果你管理这个安装实例，两者都可以在这里选择；否则此处需要填 principal 的 ID，它与账号 ID 不是一回事。",
  "members.principalPlaceholder": "Principal 的 UUID",

  "picker.choose": "选择一个人…",
  "picker.userId": "账号 ID",
  "picker.agent": "agent",
  "picker.noDirectory":
    "只有管理该安装实例的人才能列出其中的用户，因此这里要求填 ID，而不是给出可选项。向管理员索取，或从当事人自己的账号页面复制。",
  "members.noRemoval": "移除成员和修改角色尚未实现：API 对两者都没有写入接口，所以此页面也不提供。",

  "skills.title": "Skill",
  "skills.lead":
    "实际生效的集合：安装实例、组织和项目层面的绑定叠加在一起，每个 skill 以最窄的层级为准。任何覆盖都可见。",
  "skills.column.skill": "Skill",
  "skills.column.livesAt": "所在层级",
  "skills.column.version": "已解析为",
  "skills.column.trust": "可信度",
  "skills.column.state": "状态",
  "skills.browse": "浏览注册表",
  "skills.install": "安装 skill",
  "skills.empty": "这里没有生效的 skill",
  "skills.emptyBody": "从注册表安装一个，或从这个项目发布一个。",

  "registry.title": "Skill 注册表",
  "registry.lead":
    "你能看到的全部内容：你的项目中的 skill、你的组织中的 skill，以及安装实例层面的 skill。Agent 检索的是同一个目录。",
  "registry.search": "按名称搜索 skill…",
  "registry.column.latest": "最新版本",
  "registry.column.author": "作者",
  "registry.publish": "新建 skill",
  "registry.publishLead":
    "Skill 是交给 agent 的指令文本。发布会创建一个不可变的版本 — 之后要修改就得再发布一个，而两个版本都会保留。",
  "registry.publishName": "名称",
  "registry.publishAuthor": "作者",
  "registry.publishOrigin": "来源",
  "registry.publishScope": "发布到",
  "registry.publishContent": "指令内容",
  "registry.published": "已发布版本 {version}",
  "registry.publishScopeHelp":
    "指它存放在哪里，也就是谁能在目录里看到它 — 而不是它在哪里生效。一个项目可以安装任何它看得见的 skill。",
  "registry.results": "{count, plural, other {# 条结果}}，搜索“{query}”",

  "orgs.title": "组织",
  "orgs.lead":
    "位于项目之上的层级。在组织层面，一个 skill 或 gate 只需规定一次，不必逐个项目设置。",
  "orgs.column.name": "组织",
  "orgs.column.role": "你的角色",
  "orgs.empty": "你不属于任何组织",
  "orgs.emptyBody":
    "组织成员身份是按人的账号记录的，所以服务 principal 永远不会有 — 对它来说这是正确答案，而不是拒绝。",
  "orgs.members": "成员",
  "orgs.membersLead":
    "成员身份针对人的账号，而不是 principal：组织角色说的是人，而服务 principal 是在项目内部工作的。",
  "orgs.audit": "活动",
  "orgs.column.user": "账号",
  "orgs.select": "选择一个组织",
  "orgs.selectBody": "在左侧选一个，查看它的成员和活动。",

  "scope.installation": "安装实例",
  "scope.organization": "组织",
  "scope.project": "项目",

  "trust.signed": "已签名",
  "trust.unverified": "未验证",

  "role.OWNER": "所有者",
  "role.ADMIN": "管理员",
  "role.CONTRIBUTOR": "贡献者",
  "role.READER": "只读",

  "activity.title": "活动",
  "activity.lead":
    "事件与审计在同一条时间线上。它们回答的是同一个问题 — 这里发生了什么 — 把它们分开只会让读的人再自己拼回去。",
  "activity.column.time": "时间",
  "activity.column.action": "操作",
  "activity.column.resource": "资源",
  "activity.column.principal": "Principal",
  "activity.column.result": "结果",
  "activity.result.allow": "已允许",
  "activity.result.deny": "已拒绝",
  "activity.result.error": "错误",

  "admin.title": "管理",
  "admin.lead": "安装实例本身：谁在管理它、谁被拒绝过，以及其他一切所依附的身份。",
  "admin.tab.overview": "总览",
  "admin.tab.people": "人员",
  "admin.tab.agents": "agent",
  "admin.column.registeredBy": "注册者",
  "admin.unattributed": "未记录",
  "admin.registeredByHelp":
    "创建该 agent 的人。它只记录 agent 的来源，不授予任何权限——agent 只能通过自己在项目中的成员身份访问该项目。",
  "admin.tab.usage": "使用情况",
  "admin.usage": "使用情况",
  "admin.usageLead":
    "这个安装实例实际被使用的程度，以及它存了多少东西。只有计数 — 管理安装实例的人是在管理它，而不是读取它的内容，所以这里没有任何东西会点出某个项目、某个路径或某个人。",
  "admin.usage.content": "存了什么",
  "admin.usage.coordination": "协作",
  "admin.usage.activity": "活动",
  "admin.usage.credentials": "凭据",
  "admin.usage.inOrganizations": "个项目，分布在 {count, plural, other {# 个组织}}",
  "admin.usage.andAgents": "个人，以及 {count, plural, other {# 个 agent}}",
  "admin.usage.acrossArtifacts": "分布在 {count, plural, other {# 个工件}}",
  "admin.usage.ofIssued": "个凭据被使用，共签发 {count, plural, other {# 个}}",
  "admin.usage.artifacts": "工件",
  "admin.usage.versions": "版本",
  "admin.usage.stored": "已存储",
  "admin.usage.storedHelp":
    "所有曾写入的工件版本的总大小。不是磁盘占用 — 数据库看不到对象存储，标着“磁盘”却来自 SQL 的数字只是猜测。",
  "admin.usage.secrets": "密钥",
  "admin.usage.conversations": "会话",
  "admin.usage.historyObjects": "已记录的对象",
  "admin.usage.handoffsOpen": "未接手",
  "admin.usage.handoffsClaimed": "已接手",
  "admin.usage.handoffsClosed": "已关闭",
  "admin.usage.handoffs": "交接",
  "admin.usage.skills": "Skill",
  "admin.usage.skillVersions": "Skill 版本",
  "admin.usage.skillBindings": "Skill 绑定",
  "admin.usage.gateBindings": "Gate 绑定",
  "admin.usage.events": "事件",
  "admin.usage.auditRecords": "审计记录",
  "admin.usage.securityEvents": "安全事件",
  "admin.usage.newestActivity": "最近一次活动",
  "admin.usage.oldestProject": "最早的项目",
  "admin.usage.active": "有效",
  "admin.usage.revoked": "已吊销",
  "admin.usage.expired": "已过期",
  "admin.usage.usedRecentlyHelp":
    "唯一能回答“这东西有人用吗”而不是“这里存了什么”的数字。一个凭据一周没发过任何请求，要么任务已结束，要么被忘了。",
  "admin.usage.quiet": "还什么都没发生",
  "admin.usage.quietBody":
    "没有记录到任何事件。有项目却没有事件的安装实例，说明还没有东西往里写过。",
  "admin.tab.security": "安全日志",
  "admin.admins": "安装实例管理员",
  "admin.adminsLead":
    "管理安装实例的人可以跨所有项目读取。这里故意不提供授予或撤销这项权限的入口 — 那是命令行的操作。",
  "admin.column.user": "账号",
  "admin.column.role": "角色",
  "admin.column.granted": "授予时间",
  "admin.createUser": "创建账号",
  "admin.createUserLead": "一个可以持有 token、并被加入项目的人。",
  "admin.email": "电子邮箱",
  "admin.displayName": "显示名称",
  "admin.createService": "创建服务 principal",
  "admin.createServiceLead":
    "非人类身份 — 一个 CI 任务，一个按计划运行的 agent。它在项目中操作，并像其他人一样出现在审计里。",
  "admin.name": "名称",
  "admin.description": "描述",
  "admin.users": "人员",
  "admin.usersLead": "在这个安装实例中拥有身份的所有人。他们的项目角色记在项目上，不在这里。",
  "admin.servicePrincipals": "Agent",
  "admin.servicePrincipalsLead":
    "非人类身份 — 一个 CI 任务，一个按计划运行的 agent。它在项目中操作，并像人一样出现在审计里，这正是重点：agent 做过什么，责任归于这个 agent，而不是归于配置它的人。",
  "admin.column.name": "名称",
  "admin.column.created": "创建时间",
  "admin.noUsers": "目前除你之外还没有别人",
  "admin.noServicePrincipals": "还没有注册任何 agent",
  "admin.noServicePrincipalsBody":
    "先注册一个，然后给它签发 token — CI 任务或按计划运行的 agent 调用这个服务器时出示的就是这个 token。",
  "admin.issueFor": "签发 token",
  "admin.tokens": "Token",
  "admin.tokensLead":
    "签发给这个身份的所有凭据。吊销从它的下一次请求起生效，不会改变任何成员关系。",
  "admin.noTokens": "尚未签发任何 token",
  "admin.confirmRevokeFor":
    "吊销这个凭据吗？正在使用它的东西会立即停止工作，如果那是一个正在运行的 agent，它会在任务中途失败。",
  "admin.issueForLead":
    "这是给这个身份的 token，不是给你的。它只显示一次，而持有它的东西就以这个身份行事。",
  "admin.security": "安全日志",
  "admin.securityLead":
    "服务器拒绝了什么：无法解析的 token、被拒的能力、被限流的调用方。故意与项目审计分开 — 一次拒绝往往关于调用方甚至看不到的项目，而真正值得抓住的模式，也就是一个凭据在多个项目上试探，从任何单个项目内部都看不见。",
  "admin.securityNote":
    "成功登录不是拒绝，不会记在这里。一个凭据最后做了什么，记在 token 本身上，作为它的最后使用时间。",
  "admin.column.kind": "类型",
  "admin.column.reason": "原因",
  "admin.column.capability": "能力",
  "admin.securityEmpty": "没有任何记录",
  "admin.securityEmptyBody": "没有拒绝、没有未知凭据、没什么可看。这是好情况。",

  "tokens.title": "访问 token",
  "tokens.lead":
    "你从命令行、编辑器或 MCP 客户端使用的凭据。每一个都只属于你 — 这里无法为别人签发 token。",
  "tokens.column.type": "客户端",
  "tokens.column.created": "创建时间",
  "tokens.column.lastUsed": "最后使用",
  "tokens.column.expires": "过期时间",
  "tokens.issue": "签发 token",
  "tokens.clientType": "客户端类型",
  "tokens.expiry": "有效期",
  "tokens.expiry.never": "永不过期",
  "tokens.expiry.30d": "30 天",
  "tokens.expiry.90d": "90 天",
  "tokens.expiry.1y": "1 年",
  "tokens.revoke": "吊销",
  "tokens.revoked": "已吊销",
  "tokens.expired": "已过期",
  "tokens.active": "有效",
  "tokens.empty": "还没有 token",
  "tokens.emptyBody": "签发一个，把 agent 接入这个安装实例。",
  "tokens.justIssued": "现在就复制这个 token",
  "tokens.justIssuedBody":
    "服务器只保存它的哈希，所以这是唯一一次能显示它的机会。关闭这个面板不会吊销它 — 但意味着再也读不到了。",
  "tokens.confirmRevoke": "吊销这个凭据吗？任何仍在使用它的东西都会立即停止工作。",

  "prefs.title": "偏好设置",
  "prefs.lead": "这个安装实例如何与你对话。这些设置跟着你的账号，而不是这个浏览器。",
  "prefs.language": "界面语言",
  "prefs.languageHelp": "产品自己写的一切。别人写的内容永远不会被翻译。",
  "prefs.dateFormat": "日期格式",
  "prefs.timeFormat": "时间格式",
  "prefs.timeZone": "时区",
  "prefs.showUTC": "同时显示 UTC",
  "prefs.numberFormat": "数字格式",
  "prefs.storageUnits": "存储单位",
  "prefs.units.decimal": "GB · 十进制，以 1000 为基数",
  "prefs.units.binary": "GiB · 二进制，以 1024 为基数",
  "prefs.notStored": "偏好设置还没有存到服务器上，所以目前存在这个浏览器里。",
  "prefs.section.language": "语言",
  "prefs.section.dates": "日期与时间",
  "prefs.section.numbers": "数字与单位",
  "prefs.dateFormatHelp": "日期怎么写。03-09-2026 在不同人眼里是两个不同的日子。",
  "prefs.timeFormatHelp": "时钟惯例。",
  "prefs.timeZoneHelp": "时间戳以 UTC 存储，按这个时区显示。",
  "prefs.showUTCHelp": "用于审计和安全页面，那里需要一个共同的参照。",
  "prefs.storageUnitsHelp": "字节数怎么写。存储的数值永远不变 — 只是读法不同。",
  "prefs.date.locale": "跟随语言",
  "prefs.date.ymd": "YYYY-MM-DD",
  "prefs.date.dmy": "DD-MM-YYYY",
  "prefs.date.mdy": "MM-DD-YYYY",
  "prefs.time.locale": "跟随语言",
  "prefs.time.h24": "24 小时制",
  "prefs.time.h12": "12 小时制",
  "prefs.tz.device": "跟随此设备（{zone}）",
  "prefs.on": "开",
  "prefs.off": "关",
  "prefs.serverFormat": "服务器从不发送已格式化的字符串。它发送数值；界面负责读出来。",

  "dashboard.noProjects": "你还不属于任何项目",
  "project.tab.overview": "总览",
  "project.tab.members": "成员",
  "project.tab.skills": "Skill",
  "project.tab.activity": "活动",
  "project.notBuilt.title": "尚未实现",
  "project.notBuilt.artifacts": "工件、笔记、策略和密钥都有设计稿，但 API 目前还没有读取路径。",

  "context.title": "上下文",
  "context.lead":
    "Agent 在这里启动时拿到的东西，一字不差 — 就是 MCP 返回的那份组装好的视图，只是渲染给人看。如果某个 agent 行为古怪，这里是第一个该看的地方。",
  "context.role": "以此身份行事",
  "context.capabilities": "可以",
  "context.assembled": "组装于",
  "context.budget": "预算",
  "context.budgetUsed": "{limit} 中的 {used}",
  "context.budgetLead":
    "一个上下文包有大小上限，装不下的部分会被点名，而不是被截断。分不清“没有别的了”和“本来还有但你没拿到”的 agent，会在只有一半画面的情况下自信地行动。",
  "context.truncated": "被排除的部分",
  "context.nothingTruncated": "没有任何东西被排除",
  "context.nothingTruncatedBody": "这个项目里的所有内容都装进了预算。",
  "context.column.what": "什么",
  "context.column.ref": "哪一个",
  "context.column.why": "为什么",
  "context.included": "已包含",
  "context.artifacts": "{count, plural, other {# 个工件}}",
  "context.skills": "{count, plural, other {# 个 skill}}",
  "context.recentEvents": "最近的事件",
  "context.noHandoff": "包里没有交接",
  "context.noHandoffBody": "在这里启动的 agent 会被告知没有需要接着做的事。",
  "context.budgetLimit": "大小上限",
  "context.apply": "重新组装",

  "artifacts.title": "工件",
  "artifacts.lead":
    "Agent 读写的共享存储。每次写入都会产生一个新版本；没有任何内容被原地覆盖，所以写错了可以往前读上一版，而不必从备份里恢复。",
  "artifacts.column.path": "路径",
  "artifacts.column.version": "版本",
  "artifacts.column.size": "大小",
  "artifacts.column.hash": "内容哈希",
  "artifacts.empty": "还没有工件",
  "artifacts.emptyBody": "当 agent 通过 API、命令行或 MCP 写入时，第一个就出现了。",
  "artifacts.history": "版本",
  "artifacts.content": "内容",
  "artifacts.select": "选择一个工件",
  "artifacts.selectBody": "在左侧选一个路径，查看它的版本和内容。",
  "artifacts.binary": "这不是文本",
  "artifacts.binaryBody":
    "内容类型表明这不是文本，因此不做渲染。上面的版本、大小和哈希就是这个页面能告诉你的全部。",
  "artifacts.viewing": "正在查看版本 {version}",
  "artifacts.latest": "最新",
  "artifacts.edit": "编辑",
  "artifacts.new": "新建工件",
  "artifacts.save": "保存",
  "artifacts.delete": "删除",
  "artifacts.confirmDelete":
    "删除 {path} 吗？它的版本会一起消失，任何读取该路径的东西都会开始失败。",
  "artifacts.editingFrom": "基于版本 {version} 编辑",
  "artifacts.editingNew": "该路径上的一个新工件",
  "artifacts.newPath": "路径",
  "artifacts.newPathHelp": "notes/decisions.md",
  "artifacts.contentType": "内容类型",
  "artifacts.staleWrite": "你编辑期间，有人写入了更新的版本",
  "artifacts.staleWriteBody":
    "你的保存被拒绝，而不是被应用 — 应用它会删掉对方的工作。请重新加载最新版本，把你的改动放进去，再保存一次。",
  "artifacts.reload": "重新加载最新版本",
  "artifacts.concurrency":
    "每次保存都会点明它基于哪个版本。如果期间有别人写入，服务器会拒绝这次保存，而不是覆盖对方。",
  "artifacts.binaryNoEdit": "这不是文本，所以无法在这里编辑。Agent 通过 API、命令行和 MCP 写入它。",

  "secrets.title": "密钥",
  "secrets.lead":
    "只有名称和引用。值进去一次，就再也不会通过这个界面出来 — 项目拿到的是一个引用，交给 gate 使用。",
  "secrets.column.name": "名称",
  "secrets.add": "添加密钥",
  "secrets.value": "值",
  "secrets.delete": "删除",
  "secrets.empty": "这个项目里没有密钥",
  "secrets.emptyBody": "在这里添加一个，或者用命令行。",
  "secrets.neverShown":
    "值只写入一次，之后任何人通过任何适配器都再也读不到。删掉再重新添加，是修改它的唯一办法。",
  "secrets.confirmDelete": "删除密钥 {name} 吗？任何使用它引用的东西都会开始失败。",

  "gates.title": "策略",
  "gates.lead":
    "绑定到这个项目的 gate，以及它继承来的 gate。Gate 在某个 hook 上运行，可以拒绝该操作 — 拒绝会出现在活动时间线上。",
  "gates.column.gate": "Gate",
  "gates.column.hook": "Hook",
  "gates.column.scope": "绑定层级",
  "gates.column.enabled": "状态",
  "gates.enabled": "强制执行",
  "gates.disabled": "已关闭",
  "gates.empty": "这里没有生效的 gate",
  "gates.emptyBody": "这个项目里没有任何东西拦截操作。",
  "gates.bind": "绑定 gate",
  "gates.bindLead":
    "只有服务器确实注册过的 gate 才能绑定；绑定一个未注册的 gate 会让整个操作失败，而不是被忽略，所以这份列表就是现有的全部。",
  "gates.gateName": "Gate",
  "gates.hook": "Hook",
  "gates.unbind": "解除绑定",
  "gates.confirmUnbind": "把 {gate} 从 {hook} 上解除绑定吗？它此前拒绝的操作会开始通过。",
  "gates.inherited": "继承而来",

  "skills.override": "覆盖",
  "skills.overrideLead":
    "在这个项目层面绑定另一个版本，盖住更宽层级规定的那个。被标记为不可覆盖的绑定会拒绝这个操作 — 那个标记存在的意义就在此。",
  "skills.overrideVersion": "要绑定的版本",

  "orgs.create": "新建组织",
  "orgs.createLead": "你会成为它的第一位所有者。之后项目和 skill 就可以归到它名下。",
  "orgs.name": "名称",
  "orgs.addMember": "添加成员",
  "orgs.addMemberLead":
    "组织角色说的是人，所以这里填的是账号而不是 principal — agent 是在项目内部工作的，不能持有这种角色。",
  "orgs.userId": "账号 ID",

  "settings.title": "项目设置",
  "settings.lead": "这个项目是什么，以及从这里能改动它的哪些部分。",
  "settings.identity": "标识",
  "settings.visibility": "可见性",
  "settings.encryption": "加密",
  "settings.encryptionBody":
    "内容在静态存储时用属于这个项目的密钥加密，而该密钥本身又被安装实例的根密钥包裹。交接正文和密钥走这层信封；工件的字节进入对象存储。",
  "settings.danger": "不可撤销",
  "settings.noWrites":
    "这个页面上的内容在这里都改不了：API 对项目既没有更新也没有删除。改名、改可见性和删除都是命令行的操作，而一个打开对话框然后失败的按钮，比没有这个按钮更糟。",
  "settings.connect": "接入 agent",
  "settings.connectLead":
    "用个人访问 token 把 MCP 客户端指向这个服务器。项目在每次调用时指定，所以一个 token 就能到达你所属的所有项目。",
  "settings.mcpEndpoint": "MCP 端点",
  "settings.restEndpoint": "REST",

  "dashboard.waitingLead": "有交接处于未接手或已接手状态的项目 — 是有人留给后来者的工作。",

  "history.title": "会话",
  "history.lead":
    "客户端记录下的它们自己的会话内容，这样一次交接就能指向工作真正发生的地方。这里的一切都不是这个界面写的 — 是客户端自己送进来的。",
  "history.column.conversation": "会话",
  "history.column.started": "开始时间",
  "history.column.by": "开始者",
  "history.column.type": "类型",
  "history.column.recorded": "记录时间",
  "history.objects": "已记录的对象",
  "history.lineage": "衍生关系",
  "history.lineageLead":
    "一个对象是怎么从另一个来的 — 摘要来自记录稿，压缩稿来自摘要。正是这层关系让衍生对象不会被当成原件。",
  "history.column.from": "衍生自",
  "history.column.to": "产生了",
  "history.column.kind": "关系",
  "history.select": "选择一个会话",
  "history.selectBody": "在左侧选一个，查看其中记录了什么。",
  "history.empty": "没有记录任何会话",
  "history.emptyBody": "客户端在工作过程中记录这些内容。目前还没有东西往这个项目里送过历史。",
  "history.noObjects": "这个会话里没有记录任何内容",
  "history.noLineage": "没有衍生关系",
  "history.noLineageBody": "这里的每个对象都是直接记录的，没有一个是从别的对象衍生出来的。",
  "history.notText": "这不是文本",
  "history.notTextBody":
    "历史对象就是客户端记录下来的东西，而这些字节不是有效的 UTF-8。上面的哈希和类型就是这个页面能告诉你的全部。",

  "events.title": "事件",
  "events.lead": "发件箱流。事件是给其他系统消费的；旁边的审计记录才是给人读的。",
  "events.column.seq": "序号",
  "events.column.type": "类型",
  "events.column.resource": "资源",
  "events.column.payload": "载荷",
  "events.empty": "还没有事件",

  "handoff.createLead":
    "下一个 agent 接着做下去所需要的东西。这里由人来写；agent 通过 MCP 写的是同一份内容。",
  "handoff.summaryHelp": "一行话。这项工作是关于什么的。",
  "handoff.completedHelp": "哪些已经做完了，免得有人重做一遍。",
  "handoff.nextHelp": "接下来做什么，写得够具体到可以直接动手。",
  "handoff.confirmClose": "关闭这个交接吗？它将不再是当前交接，谁也无法再接手。",

  "projects.create": "新建项目",
  "projects.createLead":
    "一个路径和一个名称。路径是人们引用它的方式，之后无法更改；你会成为它的所有者。",
  "projects.path": "路径",
  "projects.pathHelp": "命名空间/短名",
  "projects.name": "显示名称",
  "projects.created": "已创建 {path}",

  "handoff.history": "此前的交接",
  "handoff.column.created": "创建时间",
  "handoff.column.status": "状态",

  "skill.versions": "版本",
  "skill.versionsLead":
    "以这个名字发布过的所有版本。已发布的版本不可变 — 再次发布是在这里加一行，从不会改动已有的那一行。",
  "skill.content": "内容",
  "skill.contentLead": "这个版本被解析出来时，agent 拿到的就是这些。",
  "skill.provenance": "来源信息",
  "skill.checksum": "校验和",
  "skill.capabilities": "声明的能力",
  "skill.author": "作者",
  "skill.origin": "来源",
  "skill.promote": "提升层级",
  "skill.promoteLead":
    "把 skill 移到更宽的层级，让更多项目能看到它。只能单向：再收窄回去，会把它从那些当初完全正当的绑定底下抽走。",
  "skill.livesAtNow": "所在层级",
  "skill.alreadyWidest": "已经在安装实例层级",
  "skill.alreadyWidestBody": "那已经是最宽的层级，没有更远的地方可以移了。",
  "skill.newVersion": "发布新版本",
  "skill.newVersionLead":
    "已发布的版本不可变，所以修正一个 skill 就是发布下一个版本。两个版本都会保留，而钉在旧版本上的绑定会继续拿到旧版本，直到有人重新指过去。",
  "skill.newVersionFrom": "基于 v{version}",
  "skill.publishedVersion": "已发布 v{version}",
  "skill.promoteTo": "移动到",
  "skill.promoted": "现在位于 {scope} 层级",
  "skill.install": "安装",
  "skill.installed": "已安装",
  "skill.installLead":
    "把某个 skill 的一个版本钉到这个项目上。绑定指明一个具体版本 — 系统没有版本范围解析器，所以不会有东西在你脚下悄悄变动。",
  "skill.notFound": "没有这个 skill",
  "skill.notFoundBody": "它可能发布在你看不到的地方。",
  "skill.backToRegistry": "返回注册表",
  "skill.pinLead":
    "被钉住的绑定，是这个项目已经拿定主意、不希望被挪动的那个。只有在这里建立的绑定才能钉住 — 继承来的绑定不属于这个项目。",
  "skill.inherited": "继承而来",
  "skill.pin": "钉住",
  "skill.unpin": "取消钉住",
  "skill.pinned": "已钉住",

  "registry.livesAt": "所在层级",
  "registry.empty": "目录是空的",
  "registry.emptyBody": "从某个项目发布一个 skill，或把某个提升到安装实例层级。",

  "activity.empty": "这里还什么都没发生",
  "activity.emptyBody": "通过 API、命令行和 MCP 的读写，都会落在这条时间线上。",
};
