export type Scope = "project" | "organization" | "installation";

const SCOPE_CLASSES: Record<Scope, string> = {
  project: "border-cc-border text-cc-text-muted",
  organization: "border-cc-relay-violet/40 text-cc-relay-violet",
  installation: "border-cc-border text-cc-text-muted",
};

export default function ScopeBadge({ scope }: { scope: string }) {
  const known = scope === "project" || scope === "organization" || scope === "installation" ? (scope as Scope) : null;
  return (
    <span
      className={`inline-flex items-center rounded border px-1.5 py-0.5 text-xs ${
        known ? SCOPE_CLASSES[known] : "border-cc-border text-cc-text-muted"
      }`}
    >
      {scope}
    </span>
  );
}
