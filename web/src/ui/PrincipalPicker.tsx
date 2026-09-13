import { useMemo, useState } from "react";
import { useT } from "../i18n";
import { useAuth } from "../app/auth";
import { ApiError, api } from "../lib/api";
import { useResource } from "../lib/resource";
import { Note, SelectField, TextField } from "./index";

/**
 * Choosing a person or an agent, or failing that, naming one by id.
 *
 * `kind` decides which id the chosen value is. They are different ids for
 * the same identity: an organization membership is keyed by the user, a
 * project membership by the principal (docs/adr/0006). Getting that wrong
 * produces a foreign-key error nobody can read, which is why this component
 * exists at all rather than each screen rendering its own field.
 *
 * The directory is installation-admin only, and most people using these
 * screens are not admins. So this offers a real choice when it can and an id
 * field when it cannot, rather than showing an empty picker or giving
 * everyone the id field because someone might lack access.
 */
export function PrincipalPicker({
  kind,
  value,
  onChange,
  label,
  includeAgents,
}: {
  kind: "user" | "principal";
  value: string;
  onChange: (v: string) => void;
  label: string;
  /** Agents can hold a project role; they cannot hold an organization one. */
  includeAgents?: boolean;
}) {
  const t = useT();
  const { me } = useAuth();
  const [denied, setDenied] = useState(false);

  const admin = Boolean(me?.is_installation_admin);
  const refused = (err: unknown) => {
    // An admin flag that is true and a read that is refused must not blank
    // the form: fall back to the id field.
    if (err instanceof ApiError) setDenied(true);
    return { items: [] };
  };

  const users = useResource(
    () => (admin ? api.users(undefined, 200).catch(refused) : Promise.resolve({ items: [] })),
    [admin],
  );
  const agents = useResource(
    () =>
      admin && includeAgents
        ? api.servicePrincipals(undefined, 200).catch(refused)
        : Promise.resolve({ items: [] }),
    [admin, includeAgents],
  );

  const options = useMemo(() => {
    const people = (users.data?.items ?? []).flatMap((u) => {
      const id = kind === "user" ? u.id : u.principal_id;
      if (!id) return [];
      return [{ value: id, label: u.display_name ? `${u.display_name} · ${u.email}` : u.email }];
    });
    const bots = (agents.data?.items ?? []).flatMap((a) =>
      a.principal_id
        ? [
            {
              value: a.principal_id,
              label: `${a.name} · ${t("picker.agent")}`,
            },
          ]
        : [],
    );
    return [...people, ...bots];
  }, [users.data, agents.data, kind, t]);

  if (denied || options.length === 0) {
    return (
      <TextField
        label={label}
        value={value}
        onChange={onChange}
        placeholder={kind === "user" ? t("picker.userId") : t("members.principalPlaceholder")}
        grow
      />
    );
  }

  return (
    <SelectField
      label={label}
      value={value}
      onChange={onChange}
      options={[{ value: "", label: t("picker.choose") }, ...options]}
    />
  );
}

/** Says why a picker is a plain id field, where that is worth saying. */
export function PickerNote() {
  const t = useT();
  const { me } = useAuth();
  if (me?.is_installation_admin) return null;
  return <Note tone="info">{t("picker.noDirectory")}</Note>;
}
