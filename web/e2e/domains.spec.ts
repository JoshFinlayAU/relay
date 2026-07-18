import { test, expect } from "@playwright/test";
import { collectConsoleErrors, login } from "./helpers";

test("bad credentials are rejected at login", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("Username").fill("nobody");
  await page.getByLabel("Password").fill("wrong-password");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("alert")).toContainText("invalid username or password");
  await expect(page).toHaveURL(/\/login$/);
});

test("onboard a domain end to end", async ({ page }) => {
  const errors = collectConsoleErrors(page);
  await login(page);

  const name = `e2e-${Date.now()}.example`;

  await page.getByRole("link", { name: "Domains" }).click();
  await expect(page).toHaveURL(/\/domains$/);

  // Add-domain wizard.
  await page.getByRole("button", { name: "Add domain" }).click();
  const dialog = page.getByRole("dialog", { name: "add-domain" });
  await dialog.getByLabel("Domain name").fill(name);
  await dialog.getByRole("button", { name: "Create" }).click();

  // Appears in the list with a pending badge.
  const row = page.getByRole("row", { name: new RegExp(name) });
  await expect(row).toBeVisible();
  await expect(row.getByTestId("domain-status")).toHaveText("pending");

  // Open detail → DNS instructions render.
  await page.getByRole("link", { name }).click();
  await expect(page.getByRole("heading", { name })).toBeVisible();
  await expect(page.getByText("ownership", { exact: false })).toBeVisible();
  await expect(page.getByText("rly", { exact: false }).first()).toBeVisible(); // DKIM selector
  // (The one-time operator SPF-include note only appears when that record isn't
  // published yet, so it's not asserted here — it depends on live DNS.)

  // Copy buttons exist.
  await expect(page.getByRole("button", { name: "Copy value" }).first()).toBeVisible();

  // Toggle receiving → inbound MX record appears after the PATCH refetch.
  await page.getByRole("checkbox", { name: "Inbound receiving" }).click();
  await expect(page.getByText("inbound mx", { exact: false })).toBeVisible();

  // Cloudflare auto-configure surface opens with a token field (no live provision in e2e).
  await page.getByRole("button", { name: "Auto-configure" }).click();
  await expect(page.getByLabel("Cloudflare API token")).toBeVisible();
  await expect(page.getByRole("button", { name: "Configure DNS" })).toBeVisible();
  await page.getByRole("button", { name: "Close" }).click();

  // Per-domain delivery expiry: set 12h and confirm it persists.
  await page.getByLabel("Delivery expiry hours").fill("12");
  await page.getByTestId("save-expiry").click();
  await expect(page.getByText(/Currently 12h for this domain/)).toBeVisible();

  // DMARC analyzer renders (empty state until reports arrive).
  await expect(page.getByRole("heading", { name: "DMARC" })).toBeVisible();
  await expect(page.getByText(/No DMARC aggregate reports yet/)).toBeVisible();

  // Delete with confirmation.
  await page.getByRole("button", { name: "Delete" }).click();
  const confirm = page.getByRole("dialog", { name: "confirm-delete" });
  await confirm.getByRole("button", { name: "Delete" }).click();
  await expect(page).toHaveURL(/\/domains$/);
  await expect(page.getByRole("row", { name: new RegExp(name) })).toHaveCount(0);

  expect(errors(), errors().join("\n")).toEqual([]);
});
