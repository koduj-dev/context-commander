/**
 * The terms that must not be reinvented per language.
 *
 * This product sits in a field that already has its own vocabulary, and a
 * translator reaching for a native-sounding word does real damage: someone
 * searching their own documentation for "prompt" will not find
 * "Eingabeaufforderung", and an operator reading about "Fähigkeiten" has no
 * way to connect them to the `skill.*` tools they call. The established term
 * wins over the natural one.
 *
 * Two kinds of entry:
 *
 *   `keep`  — the English word is what the field uses in every language.
 *             Inflect it if the language demands (Polish "tokenu", German
 *             "Skills"), but do not replace it.
 *   `use`   — the field DOES have an established word in that language and it
 *             is not the English one. German says "Kontext", Chinese says
 *             上下文. Inventing a third option is the mistake here, not
 *             translating at all.
 *
 * A form is written as the SHORTEST STEM that survives the language's own
 * inflection, not as the dictionary word. Polish palatalises — "agent"
 * becomes "agenci" in the nominative plural and "przekazanie" becomes
 * "przekazań" — so a check looking for the full word rejects correct Polish.
 * The language wins over the rule; the stem is how the rule bends to it. A
 * stem that is too short costs a false pass, which is cheap, while one that
 * is too long costs a false failure, which teaches people to delete the
 * check.
 *
 * Checked by scripts/check-translations.mjs: when an English message contains
 * a glossary term, the translation must contain the form named here. Matched
 * as a case-insensitive substring so inflected forms pass.
 */
export type GlossaryEntry = {
  /** As it appears in the English source. */
  en: string;
  /** Per-locale form. A locale absent from the map keeps the English word. */
  forms?: Partial<Record<string, string>>;
  /** Why this term is here, for whoever next argues with it. */
  note: string;
};

export const GLOSSARY: GlossaryEntry[] = [
  {
    en: "skill",
    note: "A packaged instruction set in the Claude/MCP sense. 'Fähigkeit', 'habilidad' or 'umiejętność' would each be a different concept to a reader, and none of them matches the skill.* tools.",
    forms: { "zh-CN": "skill" },
  },
  {
    en: "prompt",
    note: "The field's word in every language; a native equivalent reads as a UI dialog rather than as model input.",
    forms: { "zh-CN": "prompt" },
  },
  {
    en: "token",
    note: "Both senses in this product — a credential, and a unit of model input. Established everywhere; Polish and Czech inflect it and that is fine.",
    forms: { "zh-CN": "token" },
  },
  {
    en: "embedding",
    note: "No established native form in any of these languages. Kept even though this product does not compute them yet, so the first screen that does cannot invent one.",
    forms: { "zh-CN": "embedding" },
  },
  {
    en: "agent",
    note: "Every target language has taken this word directly. Inflect, do not replace.",
    // "agen" rather than "agent": Polish nominative plural is "agenci".
    forms: { de: "Agent", es: "agente", fr: "agent", pl: "agen", "zh-CN": "agent" },
  },
  {
    en: "context",
    note: "The one term with a genuine established translation in every language — and the Chinese one is a real word, not a loan.",
    forms: { de: "Kontext", es: "contexto", fr: "contexte", pl: "kontekst", "zh-CN": "上下文" },
  },
  {
    en: "MCP",
    note: "A protocol name. Never translated, never expanded — 'Model Context Protocol' spelled out in four languages is four things to search for.",
  },
  {
    en: "handoff",
    note: "This product's own domain noun, not field vocabulary — so it does get translated, and the point of listing it is that each language picks ONE word and keeps it. German shipped with 'Übergabe' and Spanish with 'traspaso'; a later translator switching to 'Handover' halfway through a catalogue is the failure this entry prevents.",
    forms: {
      de: "Übergabe",
      es: "traspaso",
      fr: "transfert",
      // "przekaza" rather than "przekazanie": the genitive plural is
      // "przekazań", which shares only that much.
      pl: "przekaza",
      "zh-CN": "交接",
    },
  },
  {
    en: "tool",
    note: "An MCP tool. No English message uses the word yet, so nothing is checked — it is here so the first screen that names one does not call it a 'Werkzeug'.",
    forms: { "zh-CN": "tool" },
  },
];

/** The English terms, for a quick membership test. */
export const GLOSSARY_TERMS = GLOSSARY.map((g) => g.en);

/** What `locale` must use for `term`, or the English word when unmapped. */
export function glossaryForm(term: string, locale: string): string {
  const entry = GLOSSARY.find((g) => g.en === term);
  return entry?.forms?.[locale] ?? term;
}
