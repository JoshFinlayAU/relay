import { test, expect } from "@playwright/test";
import { collectConsoleErrors, login } from "./helpers";

test("create, reveal, and revoke an API key", async ({ page }) => {
  const errors = collectConsoleErrors(page);
  await login(page);

  await page.getByRole("link", { name: "API keys" }).click();
  await expect(page).toHaveURL(/\/api-keys$/);
  await expect(page.getByRole("heading", { name: "API keys" })).toBeVisible();

  // Create (unique name so the test is robust to any leftover keys).
  const name = `ci-bot-${Date.now()}`;
  await page.getByLabel("Name").fill(name);
  await page.getByTestId("create-api-key").click();

  // One-time secret reveal.
  const dialog = page.getByRole("dialog", { name: "api-key-secret" });
  await expect(dialog).toBeVisible();
  const secret = await dialog.getByTestId("api-key-secret").textContent();
  expect(secret && secret.startsWith("relay_")).toBeTruthy();
  await dialog.getByRole("button", { name: "Done" }).click();

  // Appears active; secret not shown again after reload.
  const row = page.getByTestId("api-key-row").filter({ hasText: name });
  await expect(row).toContainText("active");
  await page.reload();
  await expect(page.getByTestId("api-key-secret")).toHaveCount(0);

  // Revoke (confirm dialog).
  page.once("dialog", (d) => d.accept());
  await page.getByTestId("api-key-row").filter({ hasText: name }).getByRole("button", { name: "Revoke" }).click();
  await expect(page.getByTestId("api-key-row").filter({ hasText: name })).toContainText("revoked");

  expect(errors(), errors().join("\n")).toEqual([]);
});
