import {env} from 'node:process';
import {expect, test} from '@playwright/test';
import {apiCreateFiles, apiCreateRepo, apiDeleteRepo, randomString} from './utils.ts';

// The experimental frontend (branch: experiment/htmx-frontend) must keep
// working with JavaScript disabled: every hx-* attribute only upgrades markup
// that already functions as plain HTML. These tests cover both halves of that
// claim on the two interactions an integration test cannot see.

test.describe('experimental htmx frontend', () => {
  const repoName = `e2e-xhtmx-${randomString(6)}`;
  const owner = env.GITEA_TEST_E2E_USER;

  test.beforeAll(async ({request}) => {
    await apiCreateRepo(request, {name: repoName, autoInit: true});
    // autoInit already provides a README.md
    await apiCreateFiles(request, owner, repoName, [
      {path: 'notes.txt', content: 'some notes\n'},
      {path: 'docs/guide.md', content: '# Guide\n'},
    ]);
  });

  test.afterAll(async ({request}) => {
    await apiDeleteRepo(request, owner, repoName);
  });

  test('lazy file tree expands in place', async ({page}) => {
    const consoleErrors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text());
    });
    await page.goto(`/_x/${owner}/${repoName}`);
    // the tree is fetched as an HTML fragment after the first paint
    const tree = page.locator('#x-tree-root .x-tree-name');
    await expect(tree.filter({hasText: 'docs'})).toBeVisible();
    await expect(tree.filter({hasText: 'notes.txt'})).toBeVisible();

    await page.getByRole('button', {name: 'Expand folder'}).first().click();
    await expect(page.locator('#x-tree-root ul ul').getByText('guide.md')).toBeVisible();

    // expanding must not navigate away
    await expect(page).toHaveURL(new RegExp(`/_x/${owner}/${repoName}$`));
    expect(consoleErrors).toEqual([]);
  });

  test('file filter narrows the listing without leaving the page', async ({page}) => {
    await page.goto(`/_x/${owner}/${repoName}`);
    const names = page.locator('#x-file-list .x-tree-name');
    await expect(names.filter({hasText: 'notes.txt'})).toBeVisible();

    await page.getByRole('searchbox', {name: 'Filter files in this directory'}).fill('guide');
    await expect(names).toHaveCount(0);

    await page.getByRole('searchbox', {name: 'Filter files in this directory'}).fill('notes');
    await expect(names).toHaveCount(1);
    await expect(names).toHaveText('notes.txt');
  });

  test('the same pages work with JavaScript disabled', async ({browser}) => {
    const context = await browser.newContext({javaScriptEnabled: false});
    const page = await context.newPage();

    await page.goto(`/_x/${owner}/${repoName}`);
    // the directory listing is in the first response, not fetched afterwards
    await expect(page.locator('#x-file-list .x-tree-name').filter({hasText: 'notes.txt'})).toBeVisible();

    // the filter is a plain GET form, so submitting it re-renders the page
    await page.getByRole('searchbox', {name: 'Filter files in this directory'}).fill('guide');
    await page.getByRole('button', {name: 'Filter'}).click();
    await expect(page).toHaveURL(/filter=guide/);
    await expect(page.locator('#x-file-list .x-tree-name')).toHaveCount(0);

    // the same holds for the explore search form
    await page.goto('/_x/explore/repos');
    await expect(page.locator('#x-list')).toBeVisible();
    await page.getByRole('searchbox', {name: 'Search repositories'}).fill('nothing-matches-this');
    await page.getByRole('button', {name: 'Search'}).click();
    await expect(page).toHaveURL(/\?q=nothing-matches-this/);

    await context.close();
  });

  test('explore search filters in place instead of navigating', async ({page}) => {
    await page.goto('/_x/explore/repos?q=zzzz-no-such-repository');
    await expect(page.locator('#x-list .x-empty')).toBeVisible();

    await page.getByRole('searchbox', {name: 'Search repositories'}).fill(repoName);
    await expect(page.locator('#x-list').getByText(repoName)).toBeVisible();
    // HTMX swapped the fragment, the address bar stayed put
    await expect(page).toHaveURL(/_x\/explore\/repos\?q=zzzz-no-such-repository$/);
  });
});
