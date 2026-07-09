import { clerk, clerkSetup } from "@clerk/testing/playwright";
import { expect, test } from "@playwright/test";

test.beforeAll(async () => {
  await clerkSetup();
});

test("log in, see live stats, create and revoke a token", async ({ page }) => {
  test.skip(
    !process.env.E2E_CLERK_USER || !process.env.E2E_CLERK_PASSWORD,
    "set E2E_CLERK_USER and E2E_CLERK_PASSWORD (+ a live stack) to run the live e2e",
  );

  await page.goto("/");
  await clerk.signIn({
    page,
    signInParams: {
      strategy: "password",
      identifier: process.env.E2E_CLERK_USER!,
      password: process.env.E2E_CLERK_PASSWORD!,
    },
  });

  // Overview shows live numbers from /api/v1 (real tile labels, not skeletons)
  await page.goto("/");
  await expect(page.getByText("Storage used")).toBeVisible();
  await expect(page.getByText("Work saved")).toBeVisible();

  // Create a token — plaintext appears exactly once
  await page.goto("/api-keys");
  await page.getByRole("button", { name: /new token/i }).click();
  await page.getByLabel(/name/i).fill("e2e-token");
  await page.getByRole("button", { name: /^create$/i }).click();
  const secret = await page.getByText(/^turbo_/).innerText();
  expect(secret).toMatch(/^turbo_/);
  await page.getByRole("button", { name: /done/i }).click();

  // It is now listed as Active; revoke THIS token's row (not just any Revoke button)
  const row = page.getByRole("row", { name: /e2e-token/ });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: /revoke/i }).click();
  await expect(row.getByText("Revoked")).toBeVisible();
});
