import { expect, test } from '@playwright/test';

test('a cold open with test auth lands on Discover', async ({ page }) => {
  await page.goto('/');

  await expect(page).toHaveURL(/\/discover$/);
  await expect(page.getByTestId('discover-search-input')).toBeVisible();
});
