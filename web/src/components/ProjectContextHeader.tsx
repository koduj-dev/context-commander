import type { ReactNode } from "react";
import PageHeader from "./PageHeader";

export default function ProjectContextHeader({
  namespace,
  slug,
  name,
  visibility,
  role,
  actions,
}: {
  namespace: string;
  slug: string;
  name: string;
  visibility: string;
  role: string;
  actions?: ReactNode;
}) {
  return (
    <PageHeader
      title={`${namespace}/${slug}`}
      description={
        <>
          {name} · your role: {role}{" "}
          <span className="ml-1 rounded border border-cc-border px-1.5 py-0.5 text-xs text-cc-text-muted">{visibility}</span>
        </>
      }
      actions={actions}
    />
  );
}
