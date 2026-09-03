import type { ReactNode } from "react";

export type StatusTone = "success" | "warning" | "danger" | "info" | "neutral";

const TONE_CLASSES: Record<StatusTone, string> = {
  success: "border-cc-success/40 text-cc-success",
  warning: "border-cc-warning/40 text-cc-warning",
  danger: "border-cc-danger/40 text-cc-danger",
  info: "border-cc-info/40 text-cc-info",
  neutral: "border-cc-border text-cc-text-muted",
};

export default function StatusBadge({ tone, children }: { tone: StatusTone; children: ReactNode }) {
  return (
    <span className={`inline-flex items-center rounded border px-1.5 py-0.5 text-xs ${TONE_CLASSES[tone]}`}>
      {children}
    </span>
  );
}
