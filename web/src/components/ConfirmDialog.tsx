import { createPortal } from "react-dom";
import type { ReactNode } from "react";

export default function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = "Confirm",
  danger,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  message: ReactNode;
  confirmLabel?: string;
  danger?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  if (!open) return null;

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="w-full max-w-sm rounded-lg border border-cc-border bg-cc-surface-1 p-5">
        <h2 className="mb-2 text-sm font-semibold text-cc-text">{title}</h2>
        <div className="mb-4 text-sm text-cc-text-secondary">{message}</div>
        <div className="flex justify-end gap-2">
          <button
            onClick={onCancel}
            className="rounded border border-cc-border px-3 py-1.5 text-sm text-cc-text-secondary hover:bg-cc-surface-2"
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            className={`rounded px-3 py-1.5 text-sm font-medium ${
              danger ? "bg-cc-danger text-cc-void" : "bg-cc-accent-primary text-cc-void"
            }`}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
