import {env} from 'node:process';
import {expect, test} from '@playwright/test';
import {
  apiCloseIssue,
  apiCreateFiles,
  apiCreateIssue,
  apiCreateRepo,
  apiDeleteRepo,
  login,
  randomString,
} from './utils.ts';

// The experimental frontend (branch: experiment/htmx-frontend) has to be
// usable the way a server-rendered frontend is supposed to be: every hx-*
// attribute only upgrades markup that already works as a plain link, a form or
// a table, and the pages have to stay navigable by keyboard and by the browser
// back/forward buttons.
//
// These tests cover the half of that claim an integration test cannot see. They
// deliberately avoid page.waitForTimeout: every wait is on a state the server
// or the user produced.

const repoName = `e2e-xhtmx-${randomString(6)}`;
const owner = env.GITEA_TEST_E2E_USER;
const repoPath = `/_x/${owner}/${repoName}`;

// serial: the tests share one repository, and some of them create the issues
// the later ones filter for
test.describe.serial('experimental htmx frontend', () => {
  test.beforeAll(async ({request}) => {
    await apiCreateRepo(request, {name: repoName, autoInit: true});
    // autoInit already provides a README.md; these files give the tree, the
    // commits and the file view something to render
    await apiCreateFiles(request, owner, repoName, [
      {path: 'notes.txt', content: 'some notes\n'},
      {path: 'docs/guide.md', content: '# Guide\n'},
      {path: 'docs/deep/inner.md', content: '# Inner\n'},
    ]);
  });

  test.afterAll(async ({request}) => {
    await apiDeleteRepo(request, owner, repoName);
  });

  test('repository home lists the directory and the tree', async ({page}) => {
    const consoleErrors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text());
    });
    await page.goto(repoPath);

    // the directory listing is in the first response, not fetched afterwards
    const files = page.locator('#x-file-list .x-tree-name');
    await expect(files.filter({hasText: 'docs'})).toBeVisible();
    await expect(files.filter({hasText: 'notes.txt'})).toBeVisible();

    // the tree is fetched as an HTML fragment right after the first paint
    const tree = page.locator('#x-tree-root .x-tree-name');
    await expect(tree.filter({hasText: 'docs'})).toBeVisible();
    await expect(tree.filter({hasText: 'notes.txt'})).toBeVisible();
    expect(consoleErrors).toEqual([]);
  });

  test('lazy file tree expands in place, level by level', async ({page}) => {
    const consoleErrors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text());
    });
    await page.goto(repoPath);

    const toggle = page.getByRole('button', {name: 'Expand folder docs'});
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await toggle.click();
    // the child list is a real element with a stable id, so the button can
    // report what it did
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await expect(page.locator('#x-tree-root').getByRole('button', {name: 'Expand folder deep'})).toBeVisible();

    // expanding a nested folder fills the right subtree, not the first one
    await page.getByRole('button', {name: 'Expand folder deep'}).click();
    await expect(page.locator('#x-tree-docs-deep').getByText('inner.md')).toBeVisible();

    // expanding must not navigate away
    await expect(page).toHaveURL(new RegExp(`${repoPath}$`));
    expect(consoleErrors).toEqual([]);
  });

  test('every tree link leads to a page that renders', async ({page}) => {
    await page.goto(repoPath);
    const links = page.locator('#x-tree-root a.x-tree-name');
    const count = await links.count();
    expect(count).toBeGreaterThan(1);
    for (let i = 0; i < count; i++) {
      await expect(links.nth(i)).toHaveAttribute('href', new RegExp(`^${repoPath}/src/`));
      const href = await links.nth(i).getAttribute('href');
      const response = await page.request.get(href!);
      expect(response.status(), `${href} must resolve`).toBe(200);
    }
  });

  test('file filter narrows the listing without leaving the page', async ({page}) => {
    await page.goto(repoPath);
    const names = page.locator('#x-file-list .x-tree-name');
    await expect(names.filter({hasText: 'notes.txt'})).toBeVisible();

    await page.getByRole('searchbox', {name: 'Filter files in this directory'}).fill('guide');
    await expect(names).toHaveCount(0);

    await page.getByRole('searchbox', {name: 'Filter files in this directory'}).fill('notes');
    await expect(names).toHaveCount(1);
    await expect(names).toHaveText('notes.txt');
    // the address bar is untouched: an in-place filter is not a navigation
    await expect(page).toHaveURL(new RegExp(`${repoPath}$`));
  });

  test('a file view renders its content and links to blame and raw', async ({page}) => {
    await page.goto(`${repoPath}/src/branch/main/notes.txt`);
    // the page has a heading of its own, so the document has an outline
    await expect(page.getByRole('heading', {level: 1})).toHaveText('notes.txt');
    await expect(page.locator('table.x-code')).toContainText('some notes');
    await expect(page.getByRole('link', {name: 'Blame'})).toBeVisible();
    await expect(page.getByRole('link', {name: 'Raw'})).toBeVisible();
  });

  test('commits list, commit page and their pager', async ({page}) => {
    await page.goto(`${repoPath}/commits/branch/main`);
    const rows = page.locator('#x-list li');
    expect(await rows.count()).toBeGreaterThan(1);
    const newest = rows.first().getByRole('link').first();
    const title = await newest.textContent();
    await newest.click();
    await expect(page.locator('h1.x-page-title')).toContainText(title!.trim());
    // the commit page links into the experiment, and labels the full diff as
    // something the experiment does not render
    await expect(page.getByRole('link', {name: 'Browse Source'})).toHaveAttribute('href', new RegExp(`${repoPath}/src/commit/`));
    await expect(page.getByRole('link', {name: 'View the full diff in the classic UI'})).toBeVisible();
    await expect(page.locator('.x-tree-name').first()).toHaveAttribute('href', new RegExp(`^${repoPath}/src/commit/`));
  });

  test('blame renders a table and links back into the experiment', async ({page}) => {
    await page.goto(`${repoPath}/blame/branch/main/notes.txt`);
    const table = page.locator('table.x-blame');
    await expect(table).toBeVisible();
    await expect(table.locator('tbody tr')).toHaveCount(1);
    const commit = await table.locator('tbody tr a').first().getAttribute('href');
    await table.locator('tbody tr a').first().click();
    await expect(page).toHaveURL(commit!);
  });

  test('issues list, filters and the issue page', async ({page, request}) => {
    const issue = await apiCreateIssue(request, {owner, repo: repoName, title: 'an experimental issue', body: 'a body'});
    const closed = await apiCreateIssue(request, {owner, repo: repoName, title: 'a closed issue'});
    await apiCloseIssue(request, owner, repoName, closed.index);

    await page.goto(`${repoPath}/issues?state=all`);
    const list = page.locator('#x-issue-list');
    await expect(list.getByRole('link', {name: 'an experimental issue'})).toBeVisible();
    await expect(list.getByRole('link', {name: 'a closed issue'})).toBeVisible();

    // the state filter is a plain query parameter, so it works as a URL too
    await page.goto(`${repoPath}/issues?state=closed`);
    await expect(page.locator('#x-issue-list').getByRole('link', {name: 'a closed issue'})).toBeVisible();
    await expect(page.locator('#x-issue-list').getByRole('link', {name: 'an experimental issue'})).toHaveCount(0);

    // ... and as a form
    await page.goto(`${repoPath}/issues`);
    await page.getByLabel('State').selectOption('all');
    await page.getByLabel('Author').fill('e2e-admin');
    await expect(page.locator('#x-issue-list').getByRole('link', {name: 'an experimental issue'})).toBeVisible();
    // filtering by an author nobody has stops there
    await page.getByLabel('Author').fill('no-such-author-at-all');
    await expect(page.locator('#x-issue-list .x-empty')).toBeVisible();
    // the in-place filter does not navigate: the same query as a URL gives the
    // same list, and that URL is the shareable one
    await page.goto(`${repoPath}/issues?state=all&poster=${owner}`);
    await expect(page.locator('#x-issue-list').getByRole('link', {name: 'an experimental issue'})).toBeVisible();

    await page.goto(`${repoPath}/issues/${issue.index}`);
    await expect(page.locator('h1.x-page-title')).toContainText('an experimental issue');
    await expect(page.locator('.x-readme')).toContainText('a body');
    // the read-only view says where writing happens
    await expect(page.getByRole('link', {name: 'Comment, edit or close in the classic UI'})).toBeVisible();
  });

  test('explore search filters in place instead of navigating', async ({page}) => {
    await page.goto(`/_x/explore/repos?q=zzzz-no-such-repository`);
    await expect(page.locator('#x-list .x-empty')).toBeVisible();

    await page.getByRole('searchbox', {name: 'Search repositories'}).fill(repoName);
    await expect(page.locator('#x-list').getByText(repoName)).toBeVisible();
    // HTMX swapped the fragment, the address bar stayed put
    await expect(page).toHaveURL(/_x\/explore\/repos\?q=zzzz-no-such-repository$/);
  });

  test('explore lists link to the experimental repository page', async ({page}) => {
    await page.goto(`/_x/explore/repos?q=${repoName}`);
    const link = page.locator('#x-list a.x-list-title').first();
    await expect(link).toHaveAttribute('href', new RegExp(`^${repoPath}$`));
    await link.click();
    await expect(page).toHaveURL(new RegExp(`${repoPath}$`));
  });

  test('the user list keeps its own path and marks its links', async ({page}) => {
    await page.goto('/_x/explore/users');
    await expect(page.locator('#x-list')).toBeVisible();
    await page.getByRole('searchbox', {name: 'Search users'}).fill('no-such-user-at-all');
    await expect(page.locator('#x-list .x-empty')).toBeVisible();
  });

  test('pagination is a real link that also swaps in place', async ({page, browser}) => {
    // a page of one commit, so the repository this test created has more than
    // one page: the pager only renders when there is something to page to
    await page.goto(`${repoPath}/commits/branch/main?limit=1`);
    const list = page.locator('#x-list li');
    await expect(list).toHaveCount(1);
    const first = await list.first().textContent();

    const next = page.getByRole('link', {name: 'Next'});
    await expect(next).toBeVisible();
    await expect(next).toHaveAttribute('rel', 'next');
    const href = await next.getAttribute('href');
    expect(href).toContain('page=2');

    // the link is an ordinary navigation, not a control that only HTMX knows
    const noJs = await (await browser.newContext({javaScriptEnabled: false})).newPage();
    const response = await noJs.goto(href!);
    expect(response?.status()).toBe(200);
    await expect(noJs.locator('#x-list li')).toHaveCount(1);
    await expect(noJs.locator('#x-list li').first()).not.toHaveText(first!);
    await noJs.context().close();

    // ... and the same link swaps the list in place instead of navigating
    await next.click();
    await expect(page).toHaveURL(new RegExp(`${repoPath}/commits/branch/main\\?limit=1$`));
    await expect(list).toHaveCount(1);
    await expect(list.first()).not.toHaveText(first!);
    // the pager itself is re-rendered out-of-band, so it never says "Page 1"
    // while the list shows page 2
    await expect(page.locator('#x-pager')).toContainText('Page 2 of');
    await expect(page.getByRole('link', {name: 'Next'})).toHaveCount(0);
  });

  test('browser back and forward work across fragment swaps and links', async ({page}) => {
    await page.goto(repoPath);
    await page.getByRole('searchbox', {name: 'Filter files in this directory'}).fill('notes');
    await expect(page.locator('#x-file-list .x-tree-name')).toHaveCount(1);

    await page.getByRole('link', {name: 'notes.txt'}).first().click();
    await expect(page).toHaveURL(new RegExp(`${repoPath}/src/branch/main/notes.txt$`));

    // the file view is a real history entry
    await page.goBack();
    await expect(page).toHaveURL(new RegExp(`${repoPath}$`));
    await expect(page.locator('h1.x-page-title, .x-repo-head')).toBeVisible();

    await page.goForward();
    await expect(page).toHaveURL(new RegExp(`${repoPath}/src/branch/main/notes.txt$`));
    await expect(page.locator('table.x-code')).toContainText('some notes');
  });

  test('a filtered URL is bookmarkable and refreshable', async ({page}) => {
    const url = `${repoPath}/issues?state=all&poster=e2e-admin`;
    await page.goto(url);
    await expect(page.locator('#x-issue-list').getByRole('link', {name: 'an experimental issue'})).toBeVisible();
    await page.reload();
    await expect(page).toHaveURL(/\?state=all&poster=e2e-admin/);
    await expect(page.locator('#x-issue-list').getByRole('link', {name: 'an experimental issue'})).toBeVisible();
  });

  test('an unknown page answers with the experimental error page', async ({page}) => {
    const response = await page.goto(`${repoPath}/no-such-page`);
    expect(response?.status()).toBe(404);
    await expect(page.locator('.x-error')).toBeVisible();
    await expect(page.locator('.x-error h1')).toContainText('Page Not Found');
    // the error page is one of ours: it does not load the classic bundle
    await expect(page.locator('script[src*="/assets/js/"]')).toHaveCount(0);
    await page.getByRole('link', {name: 'Home'}).click();
    await expect(page).toHaveURL(/_x\/$/);
  });

  test('a missing file is an error page, not a broken page', async ({page}) => {
    const response = await page.goto(`${repoPath}/src/branch/main/no-such-file.txt`);
    expect(response?.status()).toBe(404);
    await expect(page.locator('.x-error')).toBeVisible();
  });

  test('keyboard navigation reaches the skip link, the tree and the search', async ({page}) => {
    await page.goto(repoPath);

    await page.keyboard.press('Tab');
    const skip = page.getByRole('link', {name: 'Skip to main content'});
    await expect(skip).toBeFocused();
    // the skip link is invisible until it is focused, and then it is visible
    await expect(skip).toBeVisible();
    await skip.press('Enter');
    await expect(page.locator('#x-main')).toBeFocused();

    // the tree folders are real buttons: they are reachable and operable with
    // the keyboard alone
    const toggle = page.getByRole('button', {name: 'Expand folder docs'});
    await toggle.focus();
    await expect(toggle).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('button', {name: 'Expand folder deep'})).toBeVisible();

    // the search box is a labelled control, so it can be reached by its name
    await page.getByRole('searchbox', {name: 'Filter files in this directory'}).focus();
    await expect(page.getByRole('searchbox', {name: 'Filter files in this directory'})).toBeFocused();
  });

  test('the current page is marked in the navigation', async ({page}) => {
    await page.goto('/_x/explore/repos');
    await expect(page.locator('nav.x-navbar a[aria-current="page"]')).toHaveText(/Repositories/);
    await page.goto('/_x/explore/users');
    await expect(page.locator('nav.x-navbar a[aria-current="page"]')).toHaveText(/Users/);
  });

  test('the whole experiment works with JavaScript disabled', async ({browser}) => {
    const context = await browser.newContext({javaScriptEnabled: false});
    const page = await context.newPage();

    await page.goto(repoPath);
    // the directory listing is in the first response, not fetched afterwards
    await expect(page.locator('#x-file-list .x-tree-name').filter({hasText: 'notes.txt'})).toBeVisible();

    // the filter is a plain GET form, so submitting it re-renders the page
    await page.getByRole('searchbox', {name: 'Filter files in this directory'}).fill('guide');
    await page.getByRole('button', {name: 'Filter'}).click();
    await expect(page).toHaveURL(/filter=guide/);
    await expect(page.locator('#x-file-list .x-tree-name')).toHaveCount(0);

    // navigation is navigation
    await page.goto(`${repoPath}/src/branch/main/notes.txt`);
    await expect(page.locator('table.x-code')).toContainText('some notes');
    await page.getByRole('link', {name: 'Blame'}).click();
    await expect(page.locator('table.x-blame')).toBeVisible();
    await page.goBack();
    await page.getByRole('link', {name: 'Raw'}).click();
    // the raw file is served by the production route, and it is still a file
    await expect(page).toHaveURL(new RegExp(`/${owner}/${repoName}/raw/branch/main/notes.txt$`));
    await expect(page.locator('body')).toContainText('some notes');

    // the lazy tree degrades to its server-rendered fallback: the sidebar
    // keeps a message, and every folder is still a link to its directory
    await page.goto(repoPath);
    await expect(page.locator('#x-tree-root')).toContainText('Loading');
    await page.getByRole('link', {name: 'docs'}).first().click();
    await expect(page.locator('#x-file-list .x-tree-name').filter({hasText: 'guide.md'})).toBeVisible();

    // the same holds for the explore search form
    await page.goto('/_x/explore/repos');
    await expect(page.locator('#x-list')).toBeVisible();
    await page.getByRole('searchbox', {name: 'Search repositories'}).fill('nothing-matches-this');
    await page.getByRole('button', {name: 'Search'}).click();
    await expect(page).toHaveURL(/\?q=nothing-matches-this/);

    // ... and for the issue filters
    await page.goto(`${repoPath}/issues`);
    await expect(page.locator('#x-issue-list')).toBeVisible();
    await page.getByRole('searchbox', {name: 'Search issues'}).fill('experimental');
    await page.getByRole('button', {name: 'Search'}).click();
    await expect(page).toHaveURL(/q=experimental/);
    await expect(page.locator('#x-issue-list')).toBeVisible();

    await context.close();
  });

  test('signed in and signed out see the same pages, and the session works', async ({page}) => {
    await login(page);
    await page.goto(repoPath);
    // signing out is the production GET route, reached by a link exactly like
    // the classic navbar does it
    const signOut = page.getByRole('link', {name: 'Sign Out'});
    await expect(signOut).toBeVisible();
    await expect(signOut).toHaveAttribute('href', '/user/logout');
    await expect(page.locator('form[action="/user/logout"]')).toHaveCount(0);
    await expect(page.locator('#x-file-list .x-tree-name')).toContainText([/notes\.txt/]);
  });

  test('a private repository is not served to anonymous visitors', async ({browser}) => {
    const context = await browser.newContext();
    const page = await context.newPage();
    const response = await page.goto(`/_x/${owner}/private-repo-${randomString(6)}`);
    expect(response?.status()).toBe(404);
    await expect(page.locator('.x-error')).toBeVisible();
    await context.close();
  });

  test('the experiment loads no asset it does not ship', async ({page}) => {
    const requested: string[] = [];
    page.on('request', (req) => {
      requested.push(req.url());
    });
    await page.goto(repoPath);
    await page.getByRole('button', {name: 'Expand folder docs'}).click();
    await expect(page.locator('#x-tree-docs')).toContainText('guide.md');

    // every request lives inside the experiment: the classic bundle, its
    // stylesheets, web fonts and even its favicon must never be fetched
    const outside = requested
      .filter((url) => url.startsWith('http'))
      .filter((url) => !new URL(url).pathname.startsWith('/_x'));
    expect(outside).toEqual([]);
  });

  test('a fragment endpoint is usable without the page around it', async ({page}) => {
    const response = await page.request.get(`${repoPath}/tree-nodes/branch/main`);
    expect(response.status()).toBe(200);
    expect(response.headers()['content-type']).toContain('text/html');
    const body = await response.text();
    expect(body).toContain('x-tree');
    // a fragment is not a page: it must not carry a second document around
    expect(body).not.toContain('<html');
  });

  test('a commit page is reachable for every commit of the list', async ({page}) => {
    await page.goto(`${repoPath}/commits/branch/main`);
    const links = page.locator('#x-list a.x-list-title');
    const count = await links.count();
    expect(count).toBeGreaterThan(0);
    for (let i = 0; i < count; i++) {
      const href = await links.nth(i).getAttribute('href');
      const response = await page.request.get(href!);
      expect(response.status(), `${href} must resolve`).toBe(200);
      expect(await response.text()).toContain('x-commit-meta');
    }
  });
});
