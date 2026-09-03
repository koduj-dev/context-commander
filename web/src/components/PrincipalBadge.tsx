export default function PrincipalBadge({
  principalId,
  email,
  displayName,
}: {
  principalId: string;
  email?: string;
  displayName?: string;
}) {
  const label = displayName ?? email ?? `${principalId.slice(0, 8)}…`;
  const isService = !email && !displayName;
  return (
    <span className="inline-flex items-center gap-1.5 text-cc-text">
      {label}
      {isService && (
        <span className="rounded border border-cc-border px-1 py-0.5 text-[10px] uppercase tracking-wide text-cc-text-muted">
          service
        </span>
      )}
    </span>
  );
}
