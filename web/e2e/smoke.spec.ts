import { test, expect } from "@playwright/test";

test("login page renders", async ({ page }) => {
  const errors: string[] = [];
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });

  await page.goto("/login");
  await expect(page.getByRole("form", { name: "login" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Relay" })).toBeVisible();
  await expect(page.getByLabel("Username")).toBeVisible();
  await expect(page.getByLabel("Password")).toBeVisible();
  await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible();

  expect(errors, `console errors: ${errors.join("\n")}`).toEqual([]);
});

test("unauthed deep link redirects to login", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);
});
