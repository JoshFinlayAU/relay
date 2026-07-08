import { test, expect } from "@playwright/test";
import { collectConsoleErrors, login } from "./helpers";

test("dashboard shows live stats", async ({ page }) => {
  const errors = collectConsoleErrors(page);
  await login(page);

  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();
  await expect(page.getByText("Queue depth")).toBeVisible();
  await expect(page.getByText("Delivered", { exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Recent activity" })).toBeVisible();

  expect(errors(), errors().join("\n")).toEqual([]);
});

test("messages screen renders with filters", async ({ page }) => {
  const errors = collectConsoleErrors(page);
  await login(page);

  await page.getByRole("link", { name: "Messages" }).click();
  await expect(page).toHaveURL(/\/messages$/);
  await expect(page.getByRole("heading", { name: "Messages" })).toBeVisible();
  await expect(page.getByLabel("Filter status")).toBeVisible();
  await expect(page.getByLabel("Filter direction")).toBeVisible();

  // Detailed search: sender + subject + status, then run the query.
  await page.getByLabel("Search sender").fill("orders@example.com");
  await page.getByLabel("Search subject").fill("invoice");
  await page.getByLabel("Filter status").selectOption("delivered");
  await page.getByTestId("search-messages").click();
  await expect(page.getByText(/result/)).toBeVisible(); // result count line
  // Reset clears the active filters.
  await page.getByRole("button", { name: "Reset" }).click();

  expect(errors(), errors().join("\n")).toEqual([]);
});
