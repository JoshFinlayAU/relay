import { test, expect } from "@playwright/test";
import { collectConsoleErrors, login } from "./helpers";

test("create and manage an SMTP credential with one-time secret", async ({ page }) => {
  const errors = collectConsoleErrors(page);
  await login(page);

  const domain = `cred-${Date.now()}.example`;

  // Create a domain to attach the credential to.
  await page.goto("/domains");
  await page.getByRole("button", { name: "Add domain" }).click();
  const addDom = page.getByRole("dialog", { name: "add-domain" });
  await addDom.getByLabel("Domain name").fill(domain);
  await addDom.getByRole("button", { name: "Create" }).click();
  await page.getByRole("link", { name: domain }).click();

  // Add a credential.
  await page.getByTestId("add-credential").click();
  const dialog = page.getByRole("dialog", { name: "add-credential" });
  await dialog.getByLabel("App name").fill("orders");
  await dialog.getByLabel("Max recipients/msg").fill("25");
  await dialog.getByRole("button", { name: "Create" }).click();

  // One-time secret is revealed.
  const reveal = page.getByRole("dialog", { name: "secret-reveal" });
  await expect(reveal).toBeVisible();
  await expect(reveal).toContainText("shown only once");
  const secret = await reveal.getByTestId("secret-value").textContent();
  expect(secret && secret.length).toBeGreaterThan(20);
  await reveal.getByRole("button", { name: "Done" }).click();

  // Credential appears in the list, active.
  const row = page.getByTestId("credential-row").filter({ hasText: `orders@${domain}` });
  await expect(row).toBeVisible();
  await expect(row).toContainText("active");

  // Secret is NOT retrievable again: reloading shows the row but no secret anywhere.
  await page.reload();
  await expect(page.getByTestId("secret-value")).toHaveCount(0);
  if (secret) {
    await expect(page.getByText(secret, { exact: false })).toHaveCount(0);
  }

  // Per-credential stats panel toggles open (uses /v1/credentials/{id}/stats).
  await row.getByTestId("credential-stats-toggle").click();
  await expect(page.getByTestId("credential-stats")).toBeVisible();
  await expect(page.getByTestId("credential-stats")).toContainText("Submitted");
  await row.getByTestId("credential-stats-toggle").click();

  // Suspend then resume.
  await row.getByRole("button", { name: "Suspend" }).click();
  await expect(row).toContainText("suspended");
  await row.getByRole("button", { name: "Resume" }).click();
  await expect(row).toContainText("active");

  // Delete now confirms first (destructive-action guard).
  page.once("dialog", (d) => d.accept());
  await row.getByRole("button", { name: "Delete" }).click();
  await expect(page.getByTestId("credential-row").filter({ hasText: `orders@${domain}` })).toHaveCount(0);

  expect(errors(), errors().join("\n")).toEqual([]);
});
