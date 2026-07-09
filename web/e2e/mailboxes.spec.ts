import { test, expect } from "@playwright/test";
import { collectConsoleErrors, login } from "./helpers";

test("create a mailbox with one-time webhook secret", async ({ page }) => {
  const errors = collectConsoleErrors(page);
  await login(page);

  const domain = `mbx-${Date.now()}.example`;
  await page.goto("/domains");
  await page.getByRole("button", { name: "Add domain" }).click();
  const add = page.getByRole("dialog", { name: "add-domain" });
  await add.getByLabel("Domain name").fill(domain);
  // Enable receiving so mailboxes make sense.
  await add.getByRole("checkbox").check();
  await add.getByRole("button", { name: "Create" }).click();
  await page.getByRole("link", { name: domain }).click();

  await expect(page.getByRole("heading", { name: "Mailboxes & webhooks" })).toBeVisible();
  await page.getByTestId("add-mailbox").click();
  const dialog = page.getByRole("dialog", { name: "add-mailbox" });
  await dialog.getByLabel("Local part (or * for catch-all)").fill("support");
  await dialog.getByLabel("Webhook URL").fill("https://example.test/inbound");
  await dialog.getByRole("button", { name: "Create" }).click();

  // One-time signing secret revealed.
  const reveal = page.getByRole("dialog", { name: "mailbox-secret" });
  await expect(reveal).toBeVisible();
  const secret = await reveal.getByTestId("mailbox-secret").textContent();
  expect(secret && secret.length).toBeGreaterThan(20);
  await reveal.getByRole("button", { name: "Done" }).click();

  // Listed.
  const row = page.getByTestId("mailbox-row").filter({ hasText: "support" });
  await expect(row).toBeVisible();
  await expect(row).toContainText("example.test/inbound");

  // Edit the webhook URL (set the webhook via the UI).
  await row.getByTestId("edit-webhook").click();
  const edit = page.getByRole("dialog", { name: "edit-webhook" });
  await edit.getByLabel("Webhook URL").fill("https://example.test/inbound-v2");
  await edit.getByTestId("save-webhook").click();
  await expect(page.getByTestId("mailbox-row").filter({ hasText: "support" })).toContainText("inbound-v2");

  // Not retrievable after reload.
  await page.reload();
  await expect(page.getByTestId("mailbox-secret")).toHaveCount(0);

  expect(errors(), errors().join("\n")).toEqual([]);
});
