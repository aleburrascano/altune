import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { expect, test } from '@playwright/test';

const query = 'creep radiohead';
const searchFixture = join(__dirname, 'fixtures', 'providers', 'deezer-search.json');

declare global {
  interface Window {
    __sawPending?: boolean;
  }
}

test('saving a track from Detail acquires it and lists it ready in Library', async ({ page }) => {
  test.setTimeout(180_000);
  expect(readFileSync(searchFixture, 'utf8')).toContain('Creep');

  await page.goto('/');
  await expect(page).toHaveURL(/\/(discover|library)$/, { timeout: 90_000 });
  if (page.url().endsWith('/library')) {
    await page.getByRole('button', { name: 'Discover', exact: true }).click();
  }

  const input = page.getByTestId('discover-search-input');
  await input.fill(query);
  await input.press('Enter');
  const result = page.getByTestId(/^discover-row-track-/).first();
  await expect(result).toBeVisible();
  await result.click();
  await expect(page.getByTestId('detail-banner-title')).toContainText('Creep');

  await page.evaluate(() => {
    window.__sawPending = false;
    const check = () => {
      const pill = document.querySelector('[data-testid="detail-save"]');
      if (
        pill?.textContent?.includes('Saving…') ||
        document.querySelector('[data-testid^="library-row-pending-"]')
      ) {
        window.__sawPending = true;
      }
    };
    new MutationObserver(check).observe(document.body, {
      subtree: true,
      childList: true,
      characterData: true,
      attributes: true,
    });
    check();
  });

  const pill = page.getByTestId('detail-save');
  await pill.click();
  await expect(pill).toHaveText(/^(Saved|Retry|Can't save)$/, { timeout: 90_000 });
  await expect(pill).toHaveText('Saved');
  await expect(page.getByTestId('detail-save-error')).toHaveCount(0);

  await page.getByRole('button', { name: 'Library', exact: true }).click();
  const list = page.getByTestId('library-tracks-list');
  await expect(list).toBeVisible();
  const row = list
    .getByTestId(/^library-row-(?!more-|retry-|retrying-|check-|pending-|failed-)[A-Za-z0-9_-]+$/)
    .filter({ hasText: 'Creep' });
  await expect(row).toHaveCount(1);
  await expect(list.getByTestId(/^library-row-failed-/)).toHaveCount(0);
  await expect(list.getByTestId(/^library-row-pending-/)).toHaveCount(0);

  expect(await page.evaluate(() => window.__sawPending)).toBe(true);
});
