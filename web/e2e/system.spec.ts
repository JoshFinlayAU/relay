import { test, expect } from "@playwright/test";
import { collectConsoleErrors, login } from "./helpers";

test("settings and events screens render", async ({ page }) => {
  const errors = collectConsoleErrors(page);
  await login(page);

  await page.getByRole("link", { name: "Settings" }).click();
  await expect(page).toHaveURL(/\/settings$/);
  await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
  await expect(page.getByText("mail.as135559.net.au")).toBeVisible(); // hostname
  await expect(page.getByRole("heading", { name: "Listeners" })).toBeVisible();

  // Message retention: switch to count mode, set a limit, save.
  await expect(page.getByRole("heading", { name: "Message retention" })).toBeVisible();
  await page.getByLabel("Keep by count").check();
  await page.getByLabel("Retention max messages").fill("5000");
  await page.getByTestId("save-retention").click();
  await expect(page.getByText("Saved.")).toBeVisible();

  await page.getByRole("link", { name: "Events" }).click();
  await expect(page).toHaveURL(/\/events$/);
  await expect(page.getByRole("heading", { name: "Events" })).toBeVisible();

  expect(errors(), errors().join("\n")).toEqual([]);
});

test("domain stats panel and test-send", async ({ page }) => {
  const errors = collectConsoleErrors(page);
  await login(page);

  const domain = `stats-${Date.now()}.example`;
  await page.goto("/domains");
  await page.getByRole("button", { name: "Add domain" }).click();
  const add = page.getByRole("dialog", { name: "add-domain" });
  await add.getByLabel("Domain name").fill(domain);
  await add.getByRole("button", { name: "Create" }).click();
  await page.getByRole("link", { name: domain }).click();

  // Stats panel with tiles.
  await expect(page.getByRole("heading", { name: "Statistics" })).toBeVisible();
  await expect(page.getByText("Submitted")).toBeVisible();
  await expect(page.getByText("Delivered")).toBeVisible();

  // Test send enqueues (delivery is off in e2e, so it stays queued).
  await page.getByLabel("Test recipient").fill("dest@example.net");
  await page.getByTestId("test-send").click();
  await expect(page.getByText(/Queued - trace at/)).toBeVisible();

  // The queued message has a stored (DKIM-signed) body → raw headers view works.
  await page.goto("/messages");
  await page.getByRole("link", { name: "Relay test message" }).first().click();
  await page.getByTestId("raw-headers-toggle").click();
  await expect(page.getByText(/^DKIM-Signature:/m)).toBeVisible();

  expect(errors(), errors().join("\n")).toEqual([]);
});
