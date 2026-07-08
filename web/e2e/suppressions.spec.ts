import { test, expect } from "@playwright/test";
import { collectConsoleErrors, login } from "./helpers";

test("manage a domain's suppression list", async ({ page }) => {
  const errors = collectConsoleErrors(page);
  await login(page);

  const domain = `supp-${Date.now()}.example`;
  await page.goto("/domains");
  await page.getByRole("button", { name: "Add domain" }).click();
  const add = page.getByRole("dialog", { name: "add-domain" });
  await add.getByLabel("Domain name").fill(domain);
  await add.getByRole("button", { name: "Create" }).click();
  await page.getByRole("link", { name: domain }).click();

  // Suppress an address.
  await expect(page.getByRole("heading", { name: "Suppressed addresses" })).toBeVisible();
  await page.getByLabel("Suppress address").fill("blocked@example.net");
  await page.getByRole("button", { name: "Suppress" }).click();

  const row = page.getByTestId("suppression-row").filter({ hasText: "blocked@example.net" });
  await expect(row).toBeVisible();

  // Remove (override).
  await row.getByRole("button", { name: "Remove" }).click();
  await expect(page.getByTestId("suppression-row").filter({ hasText: "blocked@example.net" })).toHaveCount(0);

  expect(errors(), errors().join("\n")).toEqual([]);
});
