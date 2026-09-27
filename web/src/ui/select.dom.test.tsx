import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "../i18n";
import { SelectField } from "./index";

/**
 * A custom select has to earn what the native one gave away.
 *
 * Replacing `<select>` buys the look and silently drops the keyboard, the
 * click-outside, and the focus return — none of which a screenshot shows and
 * all of which someone depends on. These are the behaviours that make it a
 * control rather than a picture of one.
 */
function Harness() {
  const [value, setValue] = useState("b");
  return (
    <I18nProvider>
      <SelectField
        label="Fruit"
        value={value}
        onChange={setValue}
        options={[
          { value: "a", label: "Apple" },
          { value: "b", label: "Banana" },
          { value: "c", label: "Cherry" },
        ]}
      />
      <button type="button">elsewhere</button>
    </I18nProvider>
  );
}

const trigger = () => screen.getByRole("combobox", { name: "Fruit" });

describe("SelectField", () => {
  it("shows the chosen option, not the first one", () => {
    render(<Harness />);
    expect(trigger().textContent).toContain("Banana");
    expect(trigger().getAttribute("aria-expanded")).toBe("false");
  });

  it("opens on click and closes on a second click", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    await user.click(trigger());
    expect(await screen.findByRole("listbox")).toBeTruthy();
    await user.click(trigger());
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("marks the chosen option rather than relying on its position", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    await user.click(trigger());
    const options = screen.getAllByRole("option");
    expect(options.map((o) => o.getAttribute("aria-selected"))).toEqual(["false", "true", "false"]);
  });

  it("moves with the arrow keys and chooses with Enter", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    trigger().focus();
    // Opening with a key starts on the chosen option, not at the top: arrowing
    // from the wrong place is how someone lands one row off every time.
    await user.keyboard("{ArrowDown}");
    expect(await screen.findByRole("listbox")).toBeTruthy();
    await user.keyboard("{ArrowDown}{Enter}");
    expect(trigger().textContent).toContain("Cherry");
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("jumps to the ends with Home and End", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    trigger().focus();
    await user.keyboard("{Enter}{Home}{Enter}");
    expect(trigger().textContent).toContain("Apple");
    await user.keyboard("{Enter}{End}{Enter}");
    expect(trigger().textContent).toContain("Cherry");
  });

  it("closes on Escape without choosing, and gives focus back", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    trigger().focus();
    await user.keyboard("{Enter}{ArrowDown}{Escape}");
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(trigger().textContent).toContain("Banana");
    // Focus must come back: losing it leaves a keyboard reader at the top of
    // the document.
    expect(document.activeElement).toBe(trigger());
  });

  it("closes without choosing when something else is clicked", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    await user.click(trigger());
    await user.click(screen.getByRole("button", { name: "elsewhere" }));
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(trigger().textContent).toContain("Banana");
  });

  it("escapes the ids it builds from the label", async () => {
    const user = userEvent.setup();
    render(
      <I18nProvider>
        <SelectField
          label="Date format / order"
          value="a"
          onChange={() => {}}
          options={[{ value: "a", label: "A" }]}
        />
      </I18nProvider>,
    );
    // A slash or a space in an id makes aria-controls point at nothing.
    const el = screen.getByRole("combobox", { name: "Date format / order" });
    await user.click(el);
    const id = el.getAttribute("aria-controls")!;
    expect(id).toMatch(/^[\w-]+$/);
    expect(document.getElementById(id)).toBeTruthy();
  });
});
