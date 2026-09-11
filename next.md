# Další směr produktu

**Datum:** 2026-09-01
**Východisko:** aktuální implementace na `main` (`9c2cab2`) a zjištění v `code-review.md`

Tento dokument řeší strategii a pořadí produktu. Existující `NEXT.md` je převážně technický backlog; po odsouhlasení směru doporučuji oba dokumenty sloučit do jednoho kanonického roadmap souboru, protože názvy lišící se pouze velikostí písmen jsou problematické na case-insensitive filesystémech.

## Doporučení v jedné větě

Context Commander má být **self-hosted, vendor-neutral vrstva pro předání projektového kontextu mezi různými coding agenty**, nikoliv orchestrátor agentů ani obecný knowledge management systém.

Nejsilnější vstupní use case je konkrétní:

> Agent A skončí práci, uloží ověřitelný handoff a sdílený kontext; Agent B od jiného klienta naváže během několika minut bez ručního copy-paste a bez ztráty původu dat.

Architektura pro tuto pozici už z velké části existuje. Produkt ji zatím nedoručuje jako jeden dokončený, snadno objevitelný workflow.

## Co je dnes silné

- Vendor-neutral MCP/HTTP/CLI rozhraní a jedna doménová logika pod nimi.
- Explicitní projektová hranice, role, RLS a audit místo implicitního sdílení všeho.
- Verzované a šifrované artifacts, handoffs a history s provenance.
- Skills a gates dávají základ pro pozdější standardizaci práce a governance.
- Self-hosted model a oddělené credentials jsou srozumitelná odpověď na obavu „kam agent posílá kontext“.

To je kvalitní platformový základ. Riziko je, že šířka domén předběhla uživatelskou zkušenost: skill lze nainstalovat, ale ne načíst; PAT nelze revokovat; několik seznamů není stránkovaných; a není jedna operace, která novému agentovi sestaví právě relevantní startovní kontext.

## Strategické varianty

| Varianta | Hodnota | Riziko | Rozhodnutí |
|---|---|---|---|
| Sdílený kontext a handoff mezi agenty | Řeší častou, konkrétní bolest; odpovídá současné architektuře | Je nutné prokázat, že handoff je lepší než soubor v repu | **Primární směr** |
| Orchestrátor a scheduler multi-agent práce | Atraktivní demo a větší kontrola workflow | Velká šířka, konflikt s deklarovanými non-goals, rychle se měnící klienti | Teď ne |
| Enterprise governance/control plane | Role, audit, secrets a gates jsou dobrý základ | Dlouhý prodejní cyklus; bez aktivního workflow není co řídit | Pozdější monetizační vrstva |
| Obecná „AI memory“/vektorová databáze | Velký trh a snadno vysvětlitelný pojem | Rozmělnění diferenciace, relevance/search se stane hlavním produktem | Ne jako positioning |
| Registry skills a policy marketplace | Možný síťový efekt | Předčasné bez distribuce a fungující spotřeby skillu | Až po core loopu |

Klíčové rozhodnutí je držet hranici: Context Commander vlastní **trvalý, sdílený a auditovatelný stav**, klient vlastní **reasoning, plánování a vykonání práce**.

## Primární uživatel a job-to-be-done

První cílový uživatel není enterprise administrátor. Je to technický uživatel nebo malý engineering tým, který:

- používá více než jednoho coding agenta nebo klienta;
- přepíná mezi sezeními, stroji či dodavateli;
- opakovaně vysvětluje stav projektu a předchozí rozhodnutí;
- nechce citlivý kontext ukládat do dalšího SaaS;
- snese self-hosted Docker instalaci, pokud první hodnota přijde rychle.

Jeho job-to-be-done:

> „Když změním agenta nebo předám práci kolegovi, chci bezpečně obnovit stav, rozhodnutí, rozpracovanost a další krok bez ruční rekonstrukce konverzace.“

Sekundární persona je platform/security lead. Pro ni budou později důležité policy gates, org scope, audit export a správa credentials, ale tato persona sama nevytvoří každodenní používání.

## Produktový core loop

První release musí předvést celý následující cyklus bez přímého SQL a bez ruční manipulace s object storage:

1. Operátor spustí server a bootstrapne prvního uživatele.
2. Uživatel připojí dva nezávislé klienty pomocí oddělených PAT.
3. První klient založí nebo otevře projekt a načte „context pack“.
4. Během práce zapisuje verzované artifacts/decisions a na konci vytvoří handoff.
5. Druhý klient načte aktuální context pack, převezme handoff a pokračuje.
6. Uživatel dohledá provenance změn, může token revokovat a bezpečně vyřešit konflikt verzí.

„Context pack“ nemá být nový zdroj pravdy. Má to být agregovaný read model nad existujícími doménami, například:

- identita projektu a role klienta;
- aktuální/open handoff;
- vybrané artifacts podle manifestu či konvence;
- obsah efektivních skills, ne pouze jejich UUID;
- poslední relevantní události a cursor pro pokračování;
- explicitní limity velikosti a informace o tom, co bylo oříznuto.

Pro MCP dává smysl jedna dobře popsaná operace typu `context.bootstrap`/`context.pull` a samostatné detailní tools. Agent tak nemusí před prvním užitečným krokem znát interní doménový model produktu.

## Pořadí práce

### Fáze 0 — důvěryhodný základ

Release je blokovaný, dokud nejsou opravené bezpečnostní a datově-integritní P1 z `code-review.md`:

1. MCP session svázat s aktuálním principalem, ideálně přejít na stateless režim.
2. Zakázat ADMIN → OWNER privilege escalation v doménové službě.
3. Serializovat souběžné zápisy stejného secretu.
4. Odvodit organization scope gate bindings z projektu, ne z klientského parametru.
5. Dodat PAT list/revoke a odmítat zápornou expiraci.

Součástí této fáze je jednotná keyset pagination. Bez ní nelze slíbit, že historie zůstane dostupná při reálném používání.

### Fáze 1 — první skutečně použitelný vertical slice

Cíl: dva klienty zvládnou kompletní cross-client handoff.

- Dodat načtení obsahu efektivního skillu (`skill.get`).
- Přidat agregovaný `context.bootstrap` s velikostním rozpočtem a cursory.
- U handoffu jasně definovat životní cyklus: open → claimed/acknowledged → closed, včetně „current handoff“ dotazu.
- Připravit jeden podporovaný Docker Compose quickstart obsahující server, PostgreSQL a persistence; bootstrap a připojení klienta musí být copy-paste postup.
- Otestovat dvě konkrétní MCP klientské implementace a jejich konfiguraci držet jako executable smoke test nebo alespoň versioned fixture.
- Přidat E2E scénář: klient A zapisuje, klient B bez sdíleného tokenu čte/přebírá, revoked token selže.
- Sladit dokumentaci s reálně zapojeným filesystem/S3 backendem.

V této fázi není potřeba webové UI. Dobré CLI, MCP tools a desetiminutový quickstart mají vyšší informační hodnotu.

### Fáze 2 — relevance a každodenní používání

Až po ověření core loopu:

- deklarativní projektový manifest určující „must-read“ artifacts a jejich rozpočty;
- metadata/full-text retrieval s filtry podle typu, autora, času a provenance;
- kompakce/sumarizace jako odvozený history object, nikdy jako přepsání zdroje;
- subscriptions nebo spolehlivé cursor polling pro změny během session;
- diagnostika „proč tento context pack obsahuje právě tyto položky“;
- export/import projektu pro backup a migraci.

Vektorové vyhledávání přidávat pouze tehdy, když měření ukáže, že metadata a textové hledání nestačí. Nemá být podmínkou první hodnoty.

### Fáze 3 — týmová a enterprise vrstva

Teprve nad používaným core loopem:

- org-level správa credentials, policy a audit export;
- jednoduché admin UI pro memberships, tokeny, gates a incident response;
- SSO/OIDC a krátkodobé credentials;
- retenční pravidla, legal hold a řízené purge workflow;
- podepsané/trusted skill balíčky a schvalovací workflow;
- provozní telemetry bez úniku obsahu.

## Co nyní vědomě nedělat

- Nestavět vlastní agent runtime, planner, scheduler ani frontu úloh.
- Nepřidávat další doménové typy, dokud současné netvoří hotový workflow.
- Nestavět rozsáhlé webové UI před ověřením CLI/MCP core loopu.
- Neprodávat produkt jako obecnou „paměť pro AI“.
- Nezakládat veřejný skill marketplace před vyřešením trust, podpisů a distribuce.
- Neoptimalizovat multi-region/hosted SaaS, dokud self-hosted instalace nemá aktivní design partners.
- Nepoužívat počet implementovaných endpointů jako hlavní měřítko pokroku.

## Metriky a validace

Pro první design partners měřit několik výsledků, ne počet features:

- **Time to first handoff:** od startu quickstartu po převzetí druhým klientem; cíl pod 10 minut.
- **Resume success:** druhý klient správně popíše stav, omezení a další krok bez doplňujícího copy-paste.
- **Manual context transfers:** počet vložení textu mezi klienty; cílem je nula v referenčním scénáři.
- **Context efficiency:** kolik načtených bajtů/tokenů bylo pro pokračování skutečně užitečných.
- **Recovery:** uživatel umí revokovat token a dohledat původ změny bez SQL.
- **Retention:** alespoň několik design partners produkt používá opakovaně každý týden, ne jen jednou nainstaluje.

Doporučený validační experiment:

1. Vybrat jeden středně velký reálný repozitář a připravit rozpracovaný úkol.
2. Nechat klienta A implementovat první část a vytvořit handoff.
3. Klient B dostane pouze přístup k projektu v Context Commanderu, nikoliv transcript A.
4. Měřit čas do správného pokračování, chybějící informace a manuální zásahy.
5. Opakovat se 3–5 design partners; každý chybějící údaj se nejprve řeší lepším modelem/context packem, ne novou obecnou platformovou funkcí.

## Definition of Done pro v0.1

v0.1 je hotová, když:

- všechny P1 a scope-related P2 nálezy jsou opravené a mají regresní testy;
- nový uživatel zvládne podporovaný quickstart bez ručního SQL;
- dva oddělené credentials a dva klienty dokončí referenční handoff scénář;
- druhý klient načte skutečný obsah artifacts a effective skills;
- všechny rostoucí list endpointy mají limit a cursor;
- PAT lze vypsat a revokovat a revokace se projeví okamžitě;
- backup/restore postup pro databázi a storage je zdokumentovaný alespoň pro referenční deployment;
- dokumentace nepopisuje neaktivní storage backend nebo budoucí funkci jako hotovou.

## Otevřená rozhodnutí k diskusi

1. **První persona:** doporučení je power user/malý tým, nikoliv enterprise platform team.
2. **První garantovaná dvojice klientů:** vybrat až po krátkém compatibility spike; garantovat dvě konkrétní implementace místo obecného „funguje se vším MCP“.
3. **Handoff jako hlavní objekt vs. context pack jako hlavní objekt:** doporučení je prodávat výsledek „handoff“, zatímco context pack zůstane mechanismus.
4. **Filesystem vs. S3 v0.1:** doporučení je jeden výborně otestovaný filesystem quickstart; S3 označit experimental, dokud není skutečně wired do `serve` a integračně otestované.
5. **Název a positioning:** ponechat „Context Commander“, ale podtitul zúžit na „shared project context for coding agents“; vyhnout se dojmu orchestrátoru.

Nejdůležitější produktová změna tedy není přidat další subsystém. Je to sestavit z existujících částí jeden krátký, bezpečný a měřitelný příběh: **uložit stav → předat práci → přesně navázat**.
