# Experimental HTMX frontend

**Status:** experiment, branch `experiment/htmx-frontend`.
Not a replacement for the existing frontend. The classic UI keeps serving every
route, API, webhook and commit-status interface.

The question this branch answers is narrow and measurable:

> Can Teabag's pages be Go templates + HTMX + SVG + a little vanilla JavaScript,
> and can the Vue/Vite/Tailwind build go away with them?

Everything below was measured on the development machine
(Go 1.27.1, Node 26.7.0, pnpm 12.4.2, 16-core x86-64, Linux). Numbers that were
not measured are marked as such rather than estimated.

---

## 1. Audit of the existing frontend

### 1.1 Shape of the thing

| | |
| --- | --- |
| Vue components | 17 `.vue` files, 4 777 lines |
| TypeScript in `web_src/js` | 242 `.ts`/`.vue` files, 21 904 lines |
| CSS in `web_src/css` | 14 678 lines |
| Fomantic | 252 KB of vendored Less + a checked-in build |
| npm dependencies | 101 direct (57 runtime, 44 dev) |
| Resolved packages | 924 in the pnpm store, 2 030 in the lockfile |
| `node_modules` | 609 MB |

The Vue surface is concentrated in `web_src/js/components` and is not small:
the file tree, the diff tree, the actions run/job views, the workflow graph,
the pull-request merge form, the dashboard repo list, the contributors and
code-frequency charts, and the recent-commits list are all Vue.

The rest of the frontend is not Vue. It is ~22 000 lines of imperative
TypeScript that walks the DOM on load (`callInitFunctions` in
`web_src/js/index.ts` registers ~90 initialisers, most of them `querySelectorAll`
swallows). Fomantic provides the widget layer.

### 1.2 Vue-specific and build-specific dependencies

| Dependency | Why it exists |
| --- | --- |
| `vue` 3.5.42 | the components above |
| `@vitejs/plugin-vue` 6.0.9 | SFC compilation |
| `vue-tsc` 3.3.11 | type-checking `.vue` files (`make lint-js`) |
| `eslint-plugin-vue` 10.11.1 | lint |
| `eslint-plugin-vue-scoped-css` 3.1.3 | lint |

Removing Vue is therefore a five-package change *at the dependency level*, but
the real cost is rewriting 17 components and their hosts.

Icon and CSS tooling:

| Dependency | Why it exists |
| --- | --- |
| `@primer/octicons` 19.36.0 | source for `public/assets/img/svg/octicon-*.svg` (385 files) |
| `material-icon-theme` 5.38.1 | file-type icons (`options/fileicon/*.json`) |
| `tailwindcss`, `postcss`, `postcss-html` | Vite's CSS pipeline (`vite.config.ts`) |
| `svgo` | `make svg` optimises the icon sources |

### 1.3 Font Awesome

Font Awesome is, at this point, **already gone from the library sense**:
there is no `@fortawesome/*` package, no `fa-*` CSS and no `fa-` class anywhere
in `templates/` or `web_src/`. It survived only as three leftover *icon files*
with Font Awesome artwork and names:

```
web_src/svg/fontawesome-save.svg      (floppy disk)
web_src/svg/fontawesome-send.svg      (paper plane)
web_src/svg/fontawesome-openid.svg    (OpenID logo)
```

referenced from 8 template call sites.

**This branch removes them**, replacing them with icons from the internal set
(`octicon-check`, `octicon-key`). The count of Font Awesome references in the
tree is now **0**. Details in commit `f0888933dc`.

### 1.4 Build pipeline

```
pnpm install --frozen-lockfile   ->  609 MB node_modules
pnpm exec vite build             ->  public/assets/{js,css,fonts,.vite}
go generate -tags bindata        ->  embeds public/ + templates/ + options/
go build -tags bindata
```

`vite.config.ts` also runs a Vite dev server that the Go process proxies to in
development, plus a separate Rolldown pass for the IIFE bundles
(`iife.ts`, `external-render-helper.ts`).

---

## 2. Migration plan

Ordered so that each step is independently shippable and measurable. Steps 1–4
are what this branch implements; steps 5+ are what a real migration would need.

### Step 1 — Build the pieces with no build step ✅

`modules/htmxui` embeds, via `go:embed`:

* `htmx.min.js` — htmx 2.0.11, vendored verbatim, 0BSD (52 182 bytes)
* `app.css` — hand-written, 9 120 bytes, no preprocessor
* `app.js` — 1 368 bytes, copy-to-clipboard only
* `icons/x-*.svg` — 44 hand-authored icons, 16 565 bytes total

Served by `AssetHandler()` with a content-type allow-list, an ETag derived from
the asset name (the embedded tree is immutable for a given binary) and
`Cache-Control: public, max-age=31536000`.

`xIcon` renders an icon inline with the same `(name, size, class)` convention as
the existing `{{svg}}` helper, and caches the size/class rewrite the same way.

### Step 2 — Reusable UI primitives ✅

`templates/x/base/` — layout (`head_start`/`head_end`), navbar, flash, pager,
empty state. The layout is split in two rather than using a `{{block}}`,
because Go's `{{define}}` registers a *global* name: every page defining
`"main"` would silently collapse into one.

### Step 3 — Simple pages ✅

`/_x/` (home), `/_x/explore/repos`, `/_x/explore/users`. All three reuse
`explore.RenderRepoSearch` / `explore.RenderUserSearch` with a different
`TplName`; no new query layer.

### Step 4 — Repository pages ✅

`/_x/{owner}/{repo}` and the `src/`, `commits/`, `blame/`, `issues` sub-paths.
These live in `routers/web/repo/x_view.go` **inside the production package**,
because they reuse its unexported preparation helpers
(`prepareToRenderDirOrFile`, `prepareToRenderDirectory`, `processGitCommits`,
`prepareIssueFilterAndList`, `performBlame`, `renderBlame`). Only the
presentation changes; git access, permissions, escaping and markdown rendering
are literally the same code the classic UI uses.

### Step 5 — Interactions that need more than HTML (not implemented)

These are the ones that decided the fate of several Vue components, and they
are the honest hard part of the migration:

| Vue component | What it does | What replaces it |
| --- | --- | --- |
| `ViewFileTree.vue` + `ViewFileTreeStore.ts` | lazy sidebar tree over a JSON API | **done** — one directory level per request, as HTML |
| `DiffFileTree.vue` | virtualised diff tree | needs a server-rendered tree plus O(n) diff slicing |
| `WorkflowGraph.vue` + `utils.ts` | DAG layout | a real algorithm, not a template; a canvas or a server-side layout pass |
| `ActionRunView/JobView/SummaryView` | live-updating run/job state | needs polling or SSE; HTMX can drive it, but the log tail is the hard part |
| `ChartCanvas.vue` | chart.js canvas | not a template problem; a charting library or server-side SVG |
| `PullRequestMergeForm.vue` | merge form with live preview | a server-rendered form with an HTMX preview fragment |
| `DashboardRepoList.vue`, `RepoContributors.vue`, `RepoCodeFrequency.vue`, `RepoRecentCommits.vue` | small lists | **done** — plain Go templates, as in `/_x/explore/repos` |

### Step 6 — Removing Vue, if the experiment is adopted

Once nothing imports `vue`:

```sh
pnpm remove vue @vitejs/plugin-vue vue-tsc eslint-plugin-vue eslint-plugin-vue-scoped-css
```

then delete the `iife`/`external-render-helper` split in `vite.config.ts` and
the proxy in `modules/public/vitedev.go`, and fold the remaining imperative
TypeScript into templates one page at a time. `@primer/octicons` can only be
dropped after the 385 generated `octicon-*.svg` files are replaced, which is
what the 44-icon internal set in step 1 is a first cut at.

---

## 3. Results

### 3.1 Dependency and toolchain

| | Classic frontend | Experimental frontend |
| --- | --- | --- |
| npm dependencies | 101 | **0** |
| vendored third-party runtime JS | 57 packages | 1 file (htmx 2.0.11, 0BSD) |
| `node_modules` | 609 MB / 924 packages | **not required** |
| frontend build step | `pnpm install` + `vite build` | **none** |
| preprocessor | Tailwind + PostCSS | none |
| CSS | 14 678 lines + Fomantic | 228 lines, hand-written |
| TypeScript in the UI | 21 904 lines + 17 Vue SFCs | 38 lines of vanilla JS |
| SVG icon pipeline | `svgo` via `make svg` | hand-authored, checked in |
| Vue components | 17 | 0 (added by this branch) |
| Font Awesome references | 8 | **0** |

The experimental `node_modules`-free claim is not theoretical. With
`node_modules` moved aside *and* `public/assets/{js,css,fonts,.vite}` deleted:

```
$ go test -count=1 -run 'TestXHtmx' ./tests/integration/
ok      gitea.dev/tests/integration   7.156s
```

All nine experimental page and asset tests pass with no frontend build output
on disk at all.

### 3.2 Build

Measured on this machine, `CGO_ENABLED=0`, warm Go build cache where stated.

| Step | Classic | Experimental |
| --- | --- | --- |
| `pnpm install --frozen-lockfile` | 609 MB written (not timed; needs network) | skipped |
| `vite build` (production, cold) | 10.90 s wall, 1 760 624 KB peak RSS | **0 s** (no step) |
| `vite build` (production, warm) | 12.29 s wall, 1 626 980 KB peak RSS | 0 s |
| `go generate -tags bindata` | 4.05 s | 4.05 s (unchanged) |
| `go build -tags bindata` (cold cache) | 37.39 s wall, 1 413 340 KB peak RSS | 22.59 s wall, 1 427 564 KB peak RSS¹ |
| `go build` (no bindata) | — | 4.14 s wall, 1 416 756 KB peak RSS¹ |

¹ the two `go build` rows are not a comparison: the first is a cold-cache build,
the second a warm one. The only honest statement is that the experiment does not
add a build step, and adds no measurable time to the Go build.

### 3.3 Bytes on the wire

HTML plus every JS/CSS/image the page references, uncompressed, measured
through the integration test server (`user2/repo1`, default branch):

| Page | Classic HTML | Classic JS+CSS | Experimental HTML | Experimental JS+CSS |
| --- | --- | --- | --- | --- |
| home | 16 282 | 1 032 576 | 50 574 | 62 670 |
| explore/repos | 84 094 | 1 032 576 | 51 034 | 62 670 |
| repository | 50 485 | 1 032 576 | 10 158 | 62 670 |
| issues | 46 750 | 1 032 576 | 12 065 | 62 670 |

The experimental figure is **constant**: 52 182 B htmx + 9 120 B CSS +
1 368 B JS, loaded once and cached for a year. It does not grow with the number
of components, because there are no components.

The experimental HTML is larger on the list pages than the classic HTML, and
that is expected: the SVG icons are inlined rather than referenced through a
sprite or a JS component, so a 60-row repo list carries ~60 inline `<svg>`
elements. It is still 5–8× smaller than the classic total.

### 3.4 Binary size

Both built with `-tags bindata -ldflags '-s -w'`, `CGO_ENABLED=0`:

```
main     118 386 848 bytes
branch   118 526 112 bytes
delta       +139 264 bytes  (+0.12%)
```

That 139 KB is the entire experimental frontend: htmx, the stylesheet, the
vanilla JS, 44 icons, 28 templates and the handler.

### 3.5 CI

| | `pull-e2e-tests.yml` | `experimental-htmx.yml` |
| --- | --- | --- |
| Node setup | yes (pnpm + node + store cache) | **no** |
| `make deps-frontend` | yes | **no** |
| `make frontend` | yes | **no** |
| `make backend` (bindata) | yes | no |
| Playwright | yes | **no** |
| Go module + build cache | yes | yes (same actions) |
| `CGO_ENABLED=0` | via Makefile | explicit |

CI wall-clock and memory were **not** measured — that needs a run on a hosted
runner, and inventing a number here would be worse than leaving it out.

### 3.6 Runtime memory

**Not measured.** A meaningful number needs a populated instance and a
before/after A/B of the same server, which this branch does not set up. The
binary is 139 KB larger, which is an upper bound on the added resident data
from the embedded assets; it is not a claim about total RSS.

### 3.7 Functionality preserved

Nine integration tests (`tests/integration/x_htmx_frontend_test.go`) cover:
home, explore repos + its HTMX fragment, explore users, repository home, the
tree fragment, file view, blame, commits, issues, asset serving and asset
path-traversal rejection. `modules/htmxui` has unit tests for the icon
renderer and the asset handler.

Nothing was removed. The API, webhooks, commit status, authentication and
repository routes are untouched; `/_x` is purely additive.

---

## 4. What the experiment shows

**Yes**, a useful subset of Teabag is comfortably expressible as Go templates
plus HTMX. Repository browsing, blame, commits, issues, search, sorting,
filtering, pagination and lazy tree expansion are all server-rendered, need no
JSON layer, and work with JavaScript disabled — the `hx-*` attributes only ever
*upgrade* markup that already functions.

**The real cost is not in the templates.** It is concentrated in the places
where Vue is doing something a template cannot: canvas charts, the diff
virtualiser, the workflow DAG layout, the actions log tail. Four of the
seventeen Vue components are genuinely hard; the other thirteen are lists and
forms that this branch already renders server-side.

**The build overhead is the clearest win.** A page load drops from ~1.03 MB of
JS+CSS to a constant 62 KB, the frontend build step disappears entirely, and
the whole experiment costs 139 KB in the binary. The Node toolchain is still
required by the classic frontend on this branch, so the 609 MB `node_modules`
is not saved *yet* — it becomes saveable when the last Vue component and the
Vite dev-server proxy are gone.

**What this branch does not prove:** that the whole frontend can move, that
runtime memory improves, or that CI gets faster. Those need the steps in
section 2 that are still open, and measurements that were not taken here.
