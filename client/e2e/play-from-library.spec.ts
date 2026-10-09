import { expect, test } from '@playwright/test';

type AudioUrls = { urls: { track_id: string; url: string }[] };

const readyRow = /^library-row-(?!more-|retry-|retrying-|check-|pending-|failed-)[A-Za-z0-9_-]+$/;

test('tapping the track saved by the save journey plays it in the player bar', async ({ page, request }) => {
  test.setTimeout(120_000);

  await page.goto('/');
  await expect(page).toHaveURL(/\/(discover|library)$/, { timeout: 90_000 });
  await page.getByRole('button', { name: 'Library', exact: true }).click();
  await page.getByRole('button', { name: 'Tracks', exact: true }).click();
  const row = page.getByTestId('library-tracks-list').getByTestId(readyRow).filter({ hasText: 'Creep' });
  await expect(row).toHaveCount(1);

  const audioUrls = page.waitForResponse(
    (response) => response.url().endsWith('/v1/audio-urls') && response.request().method() === 'POST',
  );
  await row.click();

  await expect(page.getByTestId('player-bar')).toContainText('Creep');
  const resolved = await audioUrls;
  expect(resolved.status()).toBe(200);
  const body = (await resolved.json()) as AudioUrls;
  expect(body.urls.length).toBeGreaterThan(0);
  const audio = await request.get(body.urls[0].url);
  expect(audio.status()).toBe(200);
  await expect(page.getByTestId('player-bar')).not.toContainText('contract violation');
});
