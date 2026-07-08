import { test, expect } from "@playwright/test";
import { collectConsoleErrors, login } from "./helpers";

test("admin user management + logout", async ({ page }) => {
  const errors = collectConsoleErrors(page);
  await login(page);

  await page.getByRole("link", { name: "Admin users" }).click();
  await expect(page).toHaveURL(/\/users$/);
  await expect(page.getByRole("heading", { name: "Admin users" })).toBeVisible();
  // The seeded e2e admin is listed.
  await expect(page.getByTestId("user-row").filter({ hasText: "e2e" })).toBeVisible();

  // Add a new admin.
  const uname = `u${Date.now()}`;
  await page.getByRole("button", { name: "Add user" }).click();
  const dialog = page.getByRole("dialog", { name: "add-user" });
  await dialog.getByLabel("Username").fill(uname);
  await dialog.getByLabel("Password").fill("password12345");
  await dialog.getByRole("button", { name: "Create" }).click();
  await expect(page.getByTestId("user-row").filter({ hasText: uname })).toBeVisible();

  // Sign out returns to login.
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);

  expect(errors(), errors().join("\n")).toEqual([]);
});
