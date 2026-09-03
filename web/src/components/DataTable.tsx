import type { ReactNode } from "react";

interface Column<T> {
  key: string;
  header: string;
  render?: (row: T) => ReactNode;
}

export default function DataTable<T>({
  columns,
  rows,
  getRowKey,
  emptyState,
}: {
  columns: Column<T>[];
  rows: T[] | null;
  getRowKey: (row: T) => string;
  emptyState: ReactNode;
}) {
  return (
    <div className="overflow-x-auto rounded border border-cc-border">
      <table className="w-full text-left text-sm">
        <thead className="bg-cc-surface-1 text-xs uppercase tracking-wide text-cc-text-muted">
          <tr>
            {columns.map((c) => (
              <th key={c.key} className="px-3 py-2">
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-cc-border">
          {rows === null && (
            <tr>
              <td className="px-3 py-2 text-cc-text-muted" colSpan={columns.length}>
                Loading…
              </td>
            </tr>
          )}
          {rows?.length === 0 && (
            <tr>
              <td className="px-3 py-2" colSpan={columns.length}>
                {emptyState}
              </td>
            </tr>
          )}
          {rows?.map((row) => (
            <tr key={getRowKey(row)}>
              {columns.map((c) => (
                <td key={c.key} className="px-3 py-2 text-cc-text">
                  {c.render ? c.render(row) : String((row as Record<string, unknown>)[c.key] ?? "")}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
