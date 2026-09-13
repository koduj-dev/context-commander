import { useMemo, useState } from "react";
import { useT } from "../i18n";
import { useAuth } from "../app/auth";
import { ApiError, api } from "../lib/api";
import { useResource } from "../lib/resource";
import { Note, SelectField, TextField } from "./index";

/**
 * Choosing a person, or failing that, naming one.
 *
 * The directory (`/installation/users`) is installation-admin only, and most
 * people using these screens are not installation admins. So this offers a
 * real choice when it can and an id field when it cannot — rather than
 * showing a picker that would be empty, or an id field to everyone because
 * someone might not have access.
 *
 * `kind` decides what the value means: an organization membership is keyed by
 * user (docs/adr/0006), a project membership by principal. They are different
 * ids and picking the wrong one produces a foreign-key error nobody can read.
 */
export function PrincipalPicker({
  kind,
  value,
  onChange,
  label,
}: {
  kind: "user" | "principal";
  value: string;
  onChange: (v: string) => void;
  label: string;
}) {
  const t = useT();
  const { me } = useAuth();
  const [denied, setDenied] = useState(false);

  const directory = useResource(
    () =>
      me?.is_installation_admin
        ? api.users(undefined, 200).catch((err: unknown) => {
            // An admin flag that is true and a read that is refused should
            // not blank the form: fall back to the id field.
            if (err instanceof ApiError) setDenied(true);
            return { items: [] };
          })
        : Promise.resolve({ items: [] }),
    [me?.is_installation_admin],
  );

  const people = useMemo(() => directory.data?.items ?? [], [directory.data]);
  const canPick = kind === "user" && people.length > 0 && !denied;

  if (!canPick) {
    return (
      <>
        <TextField
          label={label}
          value={value}
          onChange={onChange}
          placeholder={kind === "user" ? t("picker.userId") : t("members.principalPlaceholder")}
          grow
        />
      </>
    );
  }

  return (
    <SelectField
      label={label}
      value={value}
      onChange={onChange}
      options={[
        { value: "", label: t("picker.choose") },
        ...people.map((u) => ({
          value: u.id,
          label: u.display_name ? `${u.display_name} · ${u.email}` : u.email,
        })),
      ]}
    />
  );
}

/** Says why a picker is a plain field, where that is worth saying. */
export function PickerNote() {
  const t = useT();
  const { me } = useAuth();
  if (me?.is_installation_admin) return null;
  return <Note tone="info">{t("picker.noDirectory")}</Note>;
}
