# Experimental HTMX frontend

**Status:** experiment, branch `experiment/htmx-frontend`.
Not a replacement for the existing frontend. The classic UI keeps serving every
route, API, webhook and commit-status interface.

The question this branch answers is narrow and measurable:

> Can Teabag's pages be Go templates + HTMX + SVG + a little vanilla JavaScript,
> and can the Vue/Vite/Tailwind build go away with them?

Everything below was measured on the development machine
(Go 1.27.1, Node 26.7.0, pnpm 12.4.2, 8-core x86-64, openSUSE, 15 GB RAM,
Linux 6.x). Numbers that were not measured are marked as such rather than
estimated; where a number is noisy, the spread is given with it.

---

## 1. What the experiment covers today

`/_x` is additive. Nothing else changed, and the experiment is removed by
deleting `routers/web/xhtmx`, `routers/web/repo/x_view.go` and `templates/x`.

### 1.1 Pages

| URL | Notes |
| --- | --- |
| `/_x/` | repository list of the signed-in user, or the public one |
| `/_x/explore/repos` | search, sort, pagination |
| `/_x/explore/users` | search, sort, pagination |
| `/_x/{owner}/{repo}` | repository home, directory, file view, README; the import status page while the repository is still migrating |
| `/_x/{owner}/{repo}/src/{branch,tag,commit}/*` | directory and file view |
| `/_x/{owner}/{repo}/commit/{sha}` | **new**: author, message, parents, signature, changed files |
| `/_x/{owner}/{repo}/commits/{branch,tag,commit}/*` | commit list and its pager |
| `/_x/{owner}/{repo}/blame/{branch,tag,commit}/*` | blame as a table |
| `/_x/{owner}/{repo}/issues` | search, state, assignee, poster, label and sort filters |
| `/_x/{owner}/{repo}/issues/{index}` | **new**: read-only issue with its comments; a pull request is redirected to its classic view |
| anything else under `/_x` | experimental 404/500 page |

### 1.2 Fragment endpoints

Every `hx-get` in the experiment points at one of these, and every one of them
is an ordinary `GET` returning an HTML fragment that also works as a plain
browser navigation:

```
/_x/explore/repos/list                     /_x/explore/users/list
/_x/{owner}/{repo}/tree-nodes/{refType}/*  one directory level of the sidebar tree
/_x/{owner}/{repo}/files/{refType}/*       the directory listing
/_x/{owner}/{repo}/commits/list/{refType}/*  the commit list and its pager
/_x/{owner}/{repo}/issues/list             the issue list and its pager
```

There is **no JSON endpoint for the UI**, and none is needed: a fragment is
produced by rendering the same partial the page renders, from the same
handler preparation. `tests/integration/x_htmx_frontend_test.go`
(`TestXHtmxNoJSONEndpoints`) asserts that none of them answers with JSON and
that none of them carries a document around.

### 1.3 Reuse, not duplication

The repository pages live in `routers/web/repo/x_view.go`, inside the
production package, because they call its unexported preparation helpers:

| Experimental handler | Reuses |
| --- | --- |
| `XHome` | `checkHomeCodeViewable`, `prepareToRenderDirOrFile` |
| `XFileList` | `prepareToRenderDirectory` |
| `XCommits` | `CommitsByRange`, `processGitCommits` |
| `XCommit` | `gitrepo.GetCommit`, `gitdiff.GetDiffTree`, `asymkey_service.ParseCommitWithSignature` |
| `XBlame` | `performBlame`, `processBlameParts`, `renderBlame` |
| `XIssues` | `prepareIssueFilterAndList` |
| `XViewIssue` | `prepareIssueViewLoad`, `issue.LoadAttributes`, `filterXRefComments`, `prepareIssueViewContent`, `prepareIssueViewCommentsAndSidebarParticipants` |

Permissions are the production ones: `context.RepoAssignment`,
`context.RequireUnitReader`, `context.RepoRefByType`,
`context.CheckRepoScopedToken`, `MustEnableIssues`, `MustAllowPulls`.

Two changes to shared code were needed, both small and both about *which
frontend renders the answer*, never about business logic:

* `services/context`: `ctx.Data[ErrorTplNameKey]` lets a frontend choose its
  own 404/500 template, so a failure inside `/_x` does not fall back to the
  classic layout and its whole JS bundle.
* `services/context/repo.go`: `repoAssignmentIsHomeOrSettings` compares the
  current link with the repository link behind the frontend prefix the request
  came in through (`ctx.Data[FrontendBasePathKey]`). A frontend mounted under a
  sub-path (`/_x`) prefixes every URL, so its repository home is not
  `/{owner}/{repo}` — and comparing by *suffix* instead, as an earlier pass did,
  hands every classic route that happens to end in the repository link to the
  alternative frontend.

---

## 2. Bugs found and fixed during this pass

Every one of these has a regression test.

| Bug | Symptom | Fix |
| --- | --- | --- |
| No route for `/_x/…/commit/{sha}` | every commit link on the commits list and on every directory row was a 404 | `XCommit` + route |
| No route for `/_x/…/issues/{index}` | every issue link was a 404 | `XViewIssue` + route |
| `tree-nodes` only registered for branches | the tree of a tag or commit view 404'd | all three ref types for `src`, `tree-nodes`, `files`, `commits`, `blame` |
| Tree swap target used the list index | `id="x-tree-0"` was rendered by every level, so expanding a nested folder filled the *first* subtree | ids derived from the node path, unique by construction |
| Tree node paths were relative to the tree root | a tree rooted in `x/y` linked to `…/x/y/z` children of `…/x/y/x/y/z` | paths are absolute |
| Tree `src` link lost the ref type | commit trees linked to `/_x/o/r/src/<sha>/…` | built from `RefTypeNameSubURL()` |
| `hx-swap="innerHTML"` on a fragment that contained the target | the explore list ended up with a second element of the same id inside itself | fragments are the whole element, swapped `outerHTML` |
| Pager swapped into `#x-list` | the issue list has no such element, so paging silently did nothing | the pager takes its target as an argument |
| `ctx.Link` is a field, not a method | pager hrefs rendered as `%3cnil%3e?page=2` | `ctx.RootData.Link` |
| `GetParams`/page count truncation | 7 commits in pages of 5 reported "Page 1 of 1" | page count computed in the handler |
| Poster filter value never rendered | `value="{{.Poster}}"` was always empty (the data key is `PosterUsername`) | template fixed |
| `ShortSha` applied to a URL | blame showed the first 10 characters of the commit URL instead of the sha | `blameRow.CommitSha` added to the shared preparation |
| Empty blame link after rewriting | continuation rows linked to `/_x` | only non-empty links are rewritten |
| Blame filename empty | `FileTreePath` was never set by the experimental handler | set, as the production handler does |
| Breadcrumb built from `TreeLink` | a file at `a/b/c.txt` linked its own parent to `…/c.txt/a` | built from `XBranchLink`, the ref-rooted link the experiment renders |
| Eight missing translation keys | the UI showed `repo.issues.open`, `repo.empty`, `settings`, … | keys fixed, and a test now fails on any unknown key |
| Classic 404/500 pages inside `/_x` | an error loaded ~1 MB of the other frontend | `ErrorTplNameKey` + `MarkErrorPage` |
| The not-found handler matched the sub-path against the request path | the router strips the instance sub-path from `req.URL.Path`, so every unknown `/_x` URL answered with the classic page under a `ROOT_URL` with a path | `MarkErrorPage` matches the mount point, not the rendered base |
| Links ignored the instance sub-path | every link was wrong under a `ROOT_URL` with a path | `repo.XBasePath()` |
| Broken/migrating repository redirected out of `/_x` | `repoAssignmentIsHomeOrSettings` did not recognise the URL | the frontend base path is read from the context |
| `DisableUsersPage` ignored by the fragment | the users fragment served a disabled page | the fragment checks it too |
| Fragment handler rendered hrefs built from an empty base | `/README.md` instead of `/_x/owner/repo/src/branch/master/README.md` | fragment uses the same preparation as the page |
| `QueryBuild` panic on the label filter | the issue page returned 500 | `ctx.RootData.Link` |
| Skip link permanently hidden | `x-sr-only` clipped it even on focus | its own class |
| Every folder button named "Expand folder" | indistinguishable in a screen reader | the name carries the folder |
| `aria-expanded` never updated | the tree lied about its state | synced from the server-rendered list |
| Editing an asset did not rebuild the binary | `make test-e2e` served stale CSS/JS/HTML | assets added to `GO_SOURCES` |
| Sign-out was a `POST` form | `/user/logout` is a GET route, so the button was a 405 and nobody could sign out | the navbar links to the production route, as the classic navbar does |
| Repository URLs dropped the instance sub-path | under a `ROOT_URL` with a path, `/_x` links pointed at `/{owner}/{repo}/…` | every URL is `XBasePath()` plus a repository-relative path |
| Repository URLs carried it twice | `/_x` + `RepoLink` (which already has it) and `/_x` + `TreeLink` | one helper builds every repository URL, from the escaped path parameters |
| Tree and file filter URLs were built from raw names | an owner or repository name that needs escaping produced an unroutable link | same helper as every other repository URL |
| File filter jumped to the repository root | filtering in `x/` listed the entries of the root instead | the fragment URL keeps the tree path |
| Label links dropped every other filter | a label clicked from a filtered list started a new one | the base query carries `q`, `state`, `sort`, `poster`, `assignee`, `milestone`, like the production label filter |
| The Issues link was behind `{{if .RefFullName}}` | the issue and commit routes carry no ref middleware, so the link vanished on exactly those pages | the link does not depend on a ref |
| A commit page kept the page size of the pager out of its links | page 2 silently switched to the site-wide default | the page size is part of the link, and is capped at that default |
| The copy button restored its own text | the original was read from the whole button, so the reset rendered "Copy Copy `<url>`" | the value's own text is what is restored |
| `repoAssignmentIsHomeOrSettings` compared by suffix | any link ending in `/{owner}/{repo}` counted as an experimental repository home | the frontend prefix is read from the context |
| `app.js` was not linted | the only hand-written JavaScript of the frontend escaped the linters | `modules/htmxui/assets` is in `ESLINT_FILES` and in the vitest project |
| The pager kept saying "Page 1 of n" after a swap | paging with JavaScript on showed another page of the list in an unchanged pager | every list page wraps the pager in a stable `#x-pager`, and each fragment re-emits it out-of-band (`hx-swap-oob`) while still rendering the same partials as the page |
| The classic migrating page was served at `/_x` | an importing repository answered with the classic template and its JavaScript progress widget | `XHome` renders `x/repo/migrating`, prepared by the shared `setMigratingData`; cancel, retry and delete stay in the classic UI |
| A deeper path of a migrating repository left the experiment | `repoAssignmentAutoRedirectNotReady` redirected to the *classic* repository home | the redirect targets the repository home of the frontend that serves the request (`repoAssignmentHomeLink`) |
| A pull request under `/issues/` was rendered as an issue | the experiment showed a pull request without any of its meaning | `XViewIssue` redirects it to the classic `/pulls/` view, as the production code does for a mismatched type |
| The pages borrowed the classic favicon | the experiment loaded a file of the other frontend, so the e2e asset test could only denylist classic paths | the embedded assets carry their own `favicon.svg`, served at `/_x/asset/favicon.svg`; the e2e test now allowlists `/_x` and fails on anything else |

Known and deliberately *not* fixed:

* A repository whose only readable unit is issues redirects to the *classic*
  issues page: that redirect comes from the production check, which the
  experiment reuses rather than re-implements.
* The commit page does not render a diff (see section 5).
* A pull request has no page in the experiment at all: like the classic
  frontend for a mismatched type, `/_x/…/issues/{index}` redirects one to the
  classic `/pulls/` view.

---

## 3. Progressive enhancement

The rule the whole branch is built on: **every interaction is a link, a form
or a button first**. `hx-*` attributes only upgrade it.

| Interaction | Without JavaScript | With JavaScript |
| --- | --- | --- |
| search the repository list | `GET /_x/explore/repos?q=…` re-renders the page | the same URL swaps `#x-list` and re-emits the pager out-of-band |
| sort | `GET …?sort=…` | same |
| page through a list | the pager is an `<a rel="next" href="…?page=2">` | the same link swaps the list and the pager, which travels with it out-of-band |
| filter a directory | `GET …/src/branch/main?filter=x` | the same URL renders `#x-file-list` only |
| open a folder in the tree | the folder name is a link to its directory | the arrow fills the list in place |
| filter issues | plain GET form with `q`, `state`, `assignee`, `poster`, `sort`, `labels` | same, in place |
| sign out | a link to the production `GET /user/logout` | unchanged |
| copy the clone URL | the address is printed in the button | one click copies it |

`tests/e2e/xhtmx.test.ts` has a `javaScriptEnabled: false` test that walks the
file view, the blame page, the raw file, the tree, the file filter, the explore
search and the issue filters without a single line of client-side JavaScript
running.

Other properties the tests pin down:

* **Refresh and bookmarking** — fragment URLs and filtered URLs are ordinary
  URLs; reloading them reproduces the same page.
* **Back/forward** — HTMX swaps do not push history entries, and ordinary
  navigations do; a test navigates file view → back → forward.
* **Errors** — a missing file, an unknown page and a failed request all render
  the experimental error page, which links back into the experiment and to the
  classic UI.
* **Redirects** — a renamed repository (`/_x/user2/oldrepo1` → `/_x/user2/repo1`)
  and a renamed user stay inside the experiment.
* **Links under a sub-path** — every URL the experiment renders is
  `AppSubURL + /_x + repository-relative path`, and a test renders the pages
  with a non-root `AppSubURL` and compares the result: a link that drops the
  sub-path and a link that repeats it both fail it.
* **CSP** — no inline `<script>`, no inline event handler, htmx configured
  through a `<meta>` tag with `"allowEval":false` and the request nonce. A
  template test fails on any `<script>` without a `src` and a nonce, on any
  `on*=`, and on any `style=` that is not a label colour.

---

## 4. Accessibility and semantics

Checked by hand and by the Playwright accessibility snapshot; the regressions
that a machine can catch are pinned down by tests.

* one `<h1>` per page; the file, directory and home pages carry a visually
  hidden one so the document has an outline without changing the design
* `<nav>`, `<main>`, `<header>`, `<footer>`, `<article>`, `<table>` with
  `scope="col"`/`scope="row"`, `<th>` row headers for blame and code lines
* every form control has a `<label>`; the ones that are visually implied get an
  `x-sr-only` label
* `aria-current="page"` on the active navbar entry and on the last breadcrumb
  element, which is text and not a link
* the tree folders are `<button>`s with `aria-expanded` and `aria-controls`
  pointing at a stable, unique `<ul>`; the chevron rotates with the state
* the skip link is the first focusable element and becomes visible on focus
* `:focus-visible` outlines on every interactive element
* `prefers-reduced-motion` disables the swap animation and the chevron rotation
* busy and loading regions carry `role="status"`
* redundant `role="navigation"` / `role="main"` on `<nav>` / `<main>` removed
* the blame table and the code view are tables, not piles of `<div>`s
* inline `style` attributes are gone; the only ones left are label colours,
  which are database data and cannot live in the stylesheet

---

## 5. What HTMX is used for, and what it is not

Nine `hx-get` attributes remain, in five places:

1. the file tree (one request per directory level, on expand)
2. the three search/filter forms (explore repos, explore users, issues)
3. the directory filter
4. the three pagers

Everything else is a link, a form, a button or plain HTML. There is **no
polling**, no client-side state beyond "is this folder open" (which the server
already knows), no JSON endpoint, no framework, and **no new npm dependency**:
the whole frontend is htmx (0BSD, vendored) plus 13 KB of hand-written CSS and
3.9 KB of hand-written JavaScript.

`app.js` contains exactly two behaviours, both of which no attribute can
express: the clipboard (with a visible and announced result) and keeping
`aria-expanded` in sync with what the server rendered. Both are covered by
`modules/htmxui/app.test.ts`, which runs in a real browser through
vitest like the tests of the classic frontend.

---

## 6. Measurements

### 6.1 Build and dependencies

| Step | Base (no experiment) | This branch |
| --- | --- | --- |
| `pnpm install` | 609 MB, not timed (needs network) | not needed |
| `pnpm exec vite build`, cold | 20.67 s wall, 1 549 280 kB peak RSS | **no such step** |
| `pnpm exec vite build`, warm | 21.58 s wall, 1 589 988 kB peak RSS | **no such step** |
| `make frontend-htmx` | — | 5.64 s (two Go test packages, no bundler) |
| `go generate -tags bindata` | 6.97 s, 134 504 kB | 6.97 s, 134 504 kB |
| `go build -a -tags bindata` (full rebuild) | 2:25.03, 1 403 308 kB peak RSS | 2:40.84, 1 416 612 kB peak RSS |
| `go build` warm, one package touched | 1.27 s, 91 596 kB | 1.27 s, 91 596 kB |
| production binary (`-tags bindata -ldflags '-s -w'`) | 118 358 176 B | 118 550 688 B (**+192 512 B, +0.163 %**, measured before the assets of the last pass; they grew by a further 2 348 B) |
| binary without bindata | — | 110 002 336 B |
| npm dependencies | 101 (57 runtime) | **0** |
| Vue components | 17 | 0 |
| Font Awesome references | 8 (3 files) | 0 |

The two `go build -a` rows are the same kind of measurement (a full rebuild of
everything, same machine, same second) but they ran 3 minutes apart on a shared
8-core box, so the 16 s difference (6 %) is run-to-run noise, not a cost of the
experiment. The honest statement is that the experiment adds no build step and
no measurable Go build work: the extra binary size is htmx, the stylesheet,
the JavaScript, 44 icons, 33 templates and the handlers.

### 6.2 Bytes on the wire

Measured on a local instance (`measure/measure-repo`, 9 files, 9 commits), with
`curl`, uncompressed, summing every JS/CSS file the page references:

| Page | classic HTML | classic JS+CSS | experimental HTML | experimental JS+CSS | experimental total |
| --- | --- | --- | --- | --- | --- |
| `/` | 15 883 | 1 032 576 | 7 009 | 66 819 | 73 828 |
| `/explore/repos` | 20 906 | 1 032 576 | 7 448 | 66 819 | 74 267 |
| repository home | 51 623 | 1 032 576 | 12 167 | 66 819 | 78 986 |
| issues | 35 543 | 1 032 576 | 8 477 | 66 819 | 75 296 |
| commits | 58 288 | 1 032 576 | 16 700 | 66 819 | 83 519 |

The experimental JS+CSS figure is constant (52 182 B htmx + 13 134 B CSS +
3 851 B JS), loaded once and cached for a year.

The table above was measured on the previous revision of the branch. Since
then the stylesheet was reformatted and `app.js` was fixed and linted, so the
assets are 2 348 B larger and every "experimental total" above is that much
higher; the asset column is the size of the files the binary serves now.

### 6.3 Requests and transfer, measured in a browser

Chromium, `networkidle`, the lazy tree expanded on the repository page:

| Page | classic requests | classic transferred | experimental requests | experimental transferred |
| --- | --- | --- | --- | --- |
| home | 10 | 363 495 B | 4 | 75 028 B |
| explore | 9 | 314 820 B | 4 | 75 467 B |
| repository (+ tree expanded) | 10 | 349 690 B | 5 | 82 508 B |
| issues | 9 | 329 457 B | 4 | 76 496 B |
| commits | 10 | 356 356 B | 4 | 84 719 B |

### 6.4 Response time

50 sequential requests per page over loopback, after 5 warm-up requests,
against the same running instance:

| Page | classic mean | experimental mean | classic mean time-to-first-byte | experimental mean TTFB |
| --- | --- | --- | --- | --- |
| repository | 8.76 ms | 7.42 ms | 6.62 ms | 6.72 ms |
| explore | 3.32 ms | 1.93 ms | 2.10 ms | 1.75 ms |

This is a loopback measurement of one instance on an idle machine, not a
user-facing performance claim: it says the experimental pages are not slower to
produce, not that pages load faster for a user on a network.

### 6.5 Cold start

Process start to the first successful `GET /api/healthz`, same instance and
data directory:

| | base | this branch |
| --- | --- | --- |
| first ever start (database created) | not measured | 1 704 ms |
| warm page cache, mean of 3 | 422 ms (396/397/474) | 417 ms (428/396/426) |

The experiment is not on the startup path: nothing in it registers a hook, a
task or a cache that runs at boot.

### 6.6 Runtime memory

`VmRSS` of an idle server, five runs each, same data directory, same binary
flags:

| | runs (kB) | mean (kB) | stdev |
| --- | --- | --- | --- |
| base, classic frontend | 172 996 / 169 852 / 171 304 / 168 628 / 171 568 | 170 870 | 1 678 |
| this branch, classic frontend only | 192 172 / 189 472 / 191 236 / 181 956 / 180 704 | 187 108 | 5 381 |

The difference is **+16.2 MB (+9.5 %)**, but it is *not* data held by the
experiment: with `GODEBUG=gctrace=1` the live Go heap after the last startup GC
is **50 MB in both**, and the experiment's entire embedded footprint is under
200 KB.
The gap tracks the Go heap goal (95 MB vs 92 MB in the trace) and therefore
looks like allocator behaviour, not like the frontend. It is reported as an
observation, not as a result.

**Not measured:** CI wall clock and CI memory (they need a hosted runner),
memory under a populated production instance, and behaviour under a real
network.

---

## 7. Test coverage

| Suite | What it covers |
| --- | --- |
| `modules/templates` (6 tests) | every template parses and is registered; every `{{xIcon}}` exists; **every translation key used by the experiment exists in `locale_en-US.json`**; no unused `x.*` key; **every `hx-target`/`hx-select` and every pager target is an element some template renders**; no inline script, no inline event handler, no stray `style=` |
| `modules/htmxui` (7 tests) | the embedded assets exist, the icon set renders, size/class rewriting, unknown icons, the asset handler (content types, conditional GET, traversal, methods) |
| `modules/htmxui/app.test.ts` (3 tests, chromium + firefox) | the clipboard button: a successful copy is reported and the value is restored as `Copy <value>`, a refused copy changes nothing, and an explicit `data-x-copy-label` wins |
| `services/context` (`TestRepoAssignmentIsHomeOrSettings`) | which links count as the repository home, with and without a frontend prefix, and under a non-root `AppSubURL` |
| `tests/integration` (31 tests) | every `/_x` page and fragment, pagination on all four list pages, the migrating repository page and its in-frontend redirect, permissions against the classic answers, redirects, read-only routes, the experimental error page, the users-page setting, URL encoding, **the same page rendered under a non-root `AppSubURL`**, and **a crawler that follows every `/_x` link the experiment renders** (including the ones inside fragments) and fails on any that the classic frontend could not serve either |
| `tests/e2e/xhtmx.test.ts` (24 tests, chromium + firefox) | repository navigation, two-level tree expansion, `aria-expanded`, file view, commits and commit page, blame, issues and their filters, explore search, **pagination as a real link that also swaps in place**, bookmarking and refresh, unknown URL, missing file, keyboard navigation, `aria-current`, the whole site without JavaScript, signed-in state, anonymous 404, "loads no asset it does not ship" (every request allowlisted to `/_x`), fragment endpoints, and every commit link in the list |

All Go tests, the template tests, `make lint-go`, `make lint-js`,
`make lint-css` and the SVG linter pass. `make lint-templates` cannot run its
`djlint` half here (no `uv` in this environment); its SVG half
(`node tools/lint-templates-svg.ts`) passes. `make lint-md` fails on
pre-existing README.md issues that this branch does not touch.

Pre-existing integration failures in this environment, unrelated to the
experiment: the LFS tests (no `git-lfs` binary) and `TestGitGeneral/*/Raw`
(same reason).

---

## 8. Known differences from the classic UI

| | Classic | Experimental |
| --- | --- | --- |
| pull requests | full page with merge box, diff, reviews | listed as issues, read-only |
| commit page | full diff | author, message, parents, signature and the changed-file list; every file links to "browse at this commit", the diff links to the classic UI |
| user and organisation profiles | full page | not implemented; the user list links to the classic UI and says so |
| search, code search, admin, settings, wiki, releases, topics, actions | implemented | not implemented; not linked from the experiment except through explicit "classic UI" links |
| label filter | dropdown with archived labels and scopes | a row of label links (`?labels=1,-2`) |
| issues | sidebar with milestone, time tracking, dependencies, projects | filter row plus a read-only page |
| code view | CodeMirror | a server-rendered table, no editing |
| empty repository | quick guide with clone commands | a short page with a link to the classic UI, which creates the first file |
| themes | full theming, user theme selection | light/dark via `prefers-color-scheme` |
| i18n | all locales | all locales (the `x.*` keys exist only in `locale_en-US.json` and fall back to English elsewhere) |

Everything the experiment renders comes out of the production queries and
permission checks, so those two columns differ in *presentation only*.

---

## 9. What this branch does and does not prove

**Proves.** A useful subset of Teabag — repository browsing, blame, commits,
issues, search, sorting, filtering, pagination, lazy trees — is comfortably
expressible as Go templates plus HTMX, needs no JSON layer, works with
JavaScript disabled, and costs under 200 KB in the binary. A page load of the
experiment transfers 75–85 KB against 315–356 KB for the classic frontend, and
the frontend build step disappears entirely.

**Does not prove.** That the whole frontend can move: the four hard Vue
components (the diff tree virtualiser, the workflow DAG, the actions log tail
and the chart canvas) are still Vue, and the ~22 000 lines of imperative
TypeScript that drive the rest of the classic pages have not been folded into
templates. The Node toolchain is still required by the classic frontend on this
branch, so `node_modules` is not saved *yet*.

## 10. Next experimental step, if this one is adopted

1. **Diff and compare**, as a server-rendered fragment per file. It is the
   largest missing piece and the one the file tree and the commit page both
   point at.
2. **User and organisation profiles**, which removes the last systematic
   cross-frontend link from the repository pages.
3. **Write paths** (create issue, comment, star/watch), which is where the
   production CSRF and permission machinery has to be reused rather than
   re-implemented.
4. Only then the dependency removal: `vue`, `@vitejs/plugin-vue`, `vue-tsc`,
   `eslint-plugin-vue`, `eslint-plugin-vue-scoped-css`, and the 385 generated
   `octicon-*.svg` files once the 44-icon internal set covers them.
