import { expect, test } from '@playwright/test';

const query = 'creep radiohead';

test('searching Discover and tapping a track opens its Detail', async ({ page }) => {
  test.setTimeout(120_000);
  await page.goto('/');
  await expect(page).toHaveURL(/\/(discover|library)$/, { timeout: 90_000 });
  if (page.url().endsWith('/library')) {
    await page.getByRole('button', { name: 'Discover', exact: true }).click();
  }

  const input = page.getByTestId('discover-search-input');
  await input.fill(query);
  await input.press('Enter');

  const row = page.getByTestId(/^discover-row-track-/).first();
  await expect(row).toBeVisible();
  await row.click();

  await expect(page.getByTestId('detail-header')).toBeVisible();
  await expect(page.getByTestId('detail-banner-title')).toContainText('Creep');
});
