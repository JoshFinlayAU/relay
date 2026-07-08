import { Page, expect } from "@playwright/test";

export const USER = process.env.TEST_USER ?? "e2e";
export const PASS = process.env.TEST_PASSWORD ?? "e2e-password-123";

// login signs in through the real username/password form.
export async function login(page: Page, user = USER, pass = PASS) {
  await page.goto("/login");
  await page.getByLabel("Username").fill(user);
  await page.getByLabel("Password").fill(pass);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/$/);
}

// collectConsoleErrors attaches a listener and returns a getter.
export function collectConsoleErrors(page: Page) {
  const errors: string[] = [];
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });
  page.on("pageerror", (e) => errors.push(e.message));
  return () => errors;
}
