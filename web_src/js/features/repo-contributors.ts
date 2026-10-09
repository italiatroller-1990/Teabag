/**
 * HTMX + Alpine.js implementation for RepoContributors
 *
 * This is the most complex chart component, featuring:
 * - Multiple chart instances (main + per-contributor)
 * - Contribution type dropdown filter
 * - Date range zoom/pan (simplified in this version)
 * - Real-time polling
 *
 * Replaces the Vue component with:
 * - Server-rendered HTML template
 * - Alpine.js for reactivity and state management
 * - HTMX for data fetching
 * - Chart.js for rendering
 */

import { initChart } from '../modules/chart-init.ts';
import { chartJsColors } from '../utils/color.ts';

/**
 * Initialize the contributors chart component
 *
 * The server template should render:
 * <div id="repo-contributors-chart"
 *   data-repo-link="/user/repo"
 *   data-locale-filter-label="..."
 *   data-locale-contribution-type-commits="..."
 *   data-locale-contribution-type-additions="..."
 *   data-locale-contribution-type-deletions="..."
 *   data-locale-loading-title="..."
 *   data-locale-loading-title-failed="..."
 *   data-locale-loading-info="..."
 *   data-locale-chart-zoom-hint="..."
 * ></div>
 */
export async function initRepoContributors() {
  const container = document.querySelector('#repo-contributors-chart');
  if (!container) {
    return;
  }

  const repoLink = container.getAttribute('data-repo-link');
  if (!repoLink) {
    console.error('repo-contributors-chart: missing data-repo-link');
    return;
  }

  const filterLabel =
    container.getAttribute('data-locale-filter-label') || 'Filter by';
  const contributionTypeCommits =
    container.getAttribute('data-locale-contribution-type-commits') || 'Commits';
  const contributionTypeAdditions =
    container.getAttribute('data-locale-contribution-type-additions') ||
    'Additions';
  const contributionTypeDeletions =
    container.getAttribute('data-locale-contribution-type-deletions') ||
    'Deletions';
  const loadingTitle =
    container.getAttribute('data-locale-loading-title') || 'Loading...';
  const loadingTitleFailed =
    container.getAttribute('data-locale-loading-title-failed') || 'Failed to load';
  const chartZoomHint =
    container.getAttribute('data-locale-chart-zoom-hint') ||
    'Scroll to zoom, drag to pan';

  // Build HTML with Alpine.js
  container.innerHTML = `
    <div x-data="{
      isLoading: true,
      errorText: '',
      type: 'commits',
      totalStats: [],
      sortedContributors: [],
      mainChart: null,
      contributorCharts: [],

      async loadData() {
        this.isLoading = true;
        this.errorText = '';

        try {
          let response;
          do {
            response = await fetch('${repoLink}/activity/contributors/data');
            if (response.status === 202) {
              await new Promise((r) => setTimeout(r, 1000));
            }
          } while (response.status === 202);

          if (!response.ok) {
            throw new Error(response.statusText || 'Request failed');
          }

          const data = await response.json();
          this.processData(data);
          this.renderCharts();

        } catch (err) {
          this.errorText = err.message || '${loadingTitleFailed}';
        } finally {
          this.isLoading = false;
        }
      },

      processData(data) {
        // Simplified data processing
        const { total, ...contributors } = data;

        // Process total stats
        const totalWeeks = Object.fromEntries(
          Object.entries(total.weeks).sort()
        );
        this.totalStats = Object.values(totalWeeks);

        // Process contributors
        this.sortedContributors = Object.entries(contributors)
          .map(([email, user]) => ({
            email,
            ...user,
            total_commits: user.weeks.reduce(
              (sum, w) => sum + w.commits,
              0
            ),
            total_additions: user.weeks.reduce(
              (sum, w) => sum + w.additions,
              0
            ),
            total_deletions: user.weeks.reduce(
              (sum, w) => sum + w.deletions,
              0
            ),
          }))
          .filter((c) => c.total_commits > 0 || c.total_additions > 0 || c.total_deletions > 0)
          .sort((a, b) => {
            const aVal = a[\`total_\${this.type}\`] || 0;
            const bVal = b[\`total_\${this.type}\`] || 0;
            return bVal - aVal;
          })
          .slice(0, 100);
      },

      renderCharts() {
        if (!this.$el) {
          return;
        }

        // Cleanup existing charts
        if (this.mainChart) {
          this.mainChart.destroy();
          this.mainChart = null;
        }
        this.contributorCharts.forEach((chart) => {
          if (chart) {
            chart.destroy();
          }
        });
        this.contributorCharts = [];

        // Render main chart
        const mainCanvas = this.$el.querySelector('#contributors-main-canvas');
        if (mainCanvas && this.totalStats.length > 0) {
          const mainChartData = this.buildMainChartData();
          const mainChartOptions = this.buildMainChartOptions();
          this.mainChart = initChart(mainCanvas, 'line', mainChartData, mainChartOptions);
        }

        // Render contributor charts
        const contributorCanvases = this.$el.querySelectorAll('.contributor-chart-canvas');
        contributorCanvases.forEach((canvas, index) => {
          const contributor = this.sortedContributors[index];
          if (contributor) {
            const data = this.buildContributorChartData(contributor);
            const options = this.buildContributorChartOptions();
            const chart = initChart(canvas, 'line', data, options);
            this.contributorCharts.push(chart);
          }
        });
      },

      buildMainChartData() {
        const contributionType = this.type;
        return {
          datasets: [{
            data: this.totalStats.map((i) => ({ x: i.week, y: i[contributionType] })),
            pointRadius: 0,
            pointHitRadius: 0,
            fill: 'start',
            backgroundColor: chartJsColors[this.type],
            borderWidth: 0,
            tension: 0.3,
          }],
        };
      },

      buildMainChartOptions() {
        return {
          responsive: true,
          maintainAspectRatio: false,
          animation: false,
          events: [
            'mousemove',
            'mouseout',
            'click',
            'touchstart',
            'touchmove',
            'dblclick',
          ],
          plugins: {
            title: {
              display: true,
              text: '${chartZoomHint}',
              position: 'top',
              align: 'center',
            },
          },
          scales: {
            x: {
              type: 'time',
              grid: { display: false },
              time: { minUnit: 'month' },
              ticks: { maxRotation: 0, maxTicksLimit: 12 },
            },
            y: {
              min: 0,
              ticks: { maxTicksLimit: 6 },
            },
          },
        };
      },

      buildContributorChartData(contributor) {
        const contributionType = this.type;
        return {
          datasets: [{
            data: contributor.weeks.map((w) => ({
              x: w.week,
              y: w[contributionType],
            })),
            pointRadius: 0,
            pointHitRadius: 0,
            fill: 'start',
            backgroundColor: chartJsColors[this.type],
            borderWidth: 0,
            tension: 0.3,
          }],
        };
      },

      buildContributorChartOptions() {
        return {
          responsive: true,
          maintainAspectRatio: false,
          animation: false,
          scales: {
            x: {
              type: 'time',
              grid: { display: false },
              time: { minUnit: 'month' },
              ticks: { maxRotation: 0, maxTicksLimit: 6 },
            },
            y: {
              min: 0,
              ticks: { maxTicksLimit: 4 },
            },
          },
        };
      },

      setType(newType) {
        this.type = newType;
        this.renderCharts();
      },
    }
    x-init="
      // Initial load
      loadData();

      // Set up polling every 10 seconds
      const interval = setInterval(() => loadData(), 10000);

      // Cleanup on page hide/unload
      const cleanup = () => {
        clearInterval(interval);
        if ($data.mainChart) {
          $data.mainChart.destroy();
        }
        $data.contributorCharts.forEach((chart) => {
          if (chart) {
            chart.destroy();
          }
        });
      };

      window.addEventListener('beforeunload', cleanup);
      document.addEventListener('visibilitychange', () => {
        if (document.hidden) {
          cleanup();
        }
      });

      // Also cleanup when this component is removed from DOM
      const observer = new MutationObserver((mutations) => {
        if (!document.body.contains($el)) {
          cleanup();
          observer.disconnect();
        }
      });
      observer.observe(document.body, { childList: true, subtree: true });
    "
    >
      <div>
        <div class="ui header flex-left-right">
          <div>
            <span x-text="isLoading ? '${loadingTitle}' : errorText ? '${loadingTitleFailed}' : '-' "></span>
          </div>
          <div>
            <!-- Contribution type dropdown -->
            <div class="ui floating dropdown jump" id="repo-contributors">
              <div class="ui basic compact button" @click="$el.querySelector('.menu').classList.toggle('show')">
                <span class="not-mobile">${filterLabel}</span> 
                <strong x-text="type === 'commits' ? '${contributionTypeCommits}' : type === 'additions' ? '${contributionTypeAdditions}' : '${contributionTypeDeletions}'"></strong>
                <svg style="width: 14px; height: 14px; margin-left: 0.5rem;" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M12 15.5A3.5 3.5 0 0 1 8.5 12 3.5 3.5 0 0 1 12 8.5 3.5 3.5 0 0 1 15.5 12 3.5 3.5 0 0 1 12 15.5zM7.5 12a4.5 4.5 0 1 0 9 0 4.5 4.5 0 0 0-9 0z"/>
                </svg>
              </div>
              <div class="menu" style="display: none;">
                <div class="item" @click="setType('commits'); $el.parentElement.querySelector('.menu').classList.remove('show');" :class="{selected: type === 'commits'}" data-value="commits">
                  ${contributionTypeCommits}
                </div>
                <div class="item" @click="setType('additions'); $el.parentElement.querySelector('.menu').classList.remove('show');" :class="{selected: type === 'additions'}" data-value="additions">
                  ${contributionTypeAdditions}
                </div>
                <div class="item" @click="setType('deletions'); $el.parentElement.querySelector('.menu').classList.remove('show');" :class="{selected: type === 'deletions'}" data-value="deletions">
                  ${contributionTypeDeletions}
                </div>
              </div>
            </div>
          </div>
        </div>

        <div class="tw-flex ui segment main-graph">
          <template x-if="isLoading || errorText !== ''">
            <div class="tw-m-auto">
              <template x-if="isLoading">
                <svg class="tw-mr-2 rotate-clockwise" style="width: 16px; height: 16px;" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z"/>
                </svg>
                <span>Loading...</span>
              </template>
              <template x-if="!isLoading">
                <svg style="width: 16px; height: 16px; color: var(--color-red); margin-right: 0.5rem;" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z"/>
                </svg>
                <span x-text="errorText"></span>
              </template>
            </div>
          </template>
          <template x-if="totalStats.length > 0">
            <canvas id="contributors-main-canvas" role="img"></canvas>
          </template>
        </div>

        <div class="contributor-grid">
          <template x-for="(contributor, index) in sortedContributors" :key="index">
            <div>
              <div class="ui top attached header tw-flex tw-flex-1">
                <b class="ui right">#<span x-text="index + 1"></span></b>
                <a :href="contributor.home_link">
                  <img loading="lazy" class="ui avatar tw-align-middle" height="40" width="40" :src="contributor.avatar_link" alt="">
                </a>
                <div class="tw-ml-2">
                  <a x-show="contributor.home_link && contributor.home_link !== ''" :href="contributor.home_link">
                    <h4 x-text="contributor.name"></h4>
                  </a>
                  <h4 x-show="!contributor.home_link || contributor.home_link === ''" class="contributor-name" x-text="contributor.name"></h4>
                  <p class="tw-text-12 tw-flex tw-gap-1">
                    <strong x-show="contributor.total_commits > 0">
                      <a class="silenced" :href="'${repoLink}/commits/branch/main/search?q=author:' + contributor.email">
                        <span x-text="contributor.total_commits.toLocaleString()"></span> ${contributionTypeCommits}
                      </a>
                    </strong>
                    <strong x-show="contributor.total_additions > 0" class="tw-text-green">
                      <span x-text="contributor.total_additions.toLocaleString()"></span>++ 
                    </strong>
                    <strong x-show="contributor.total_deletions > 0" class="tw-text-red">
                      <span x-text="contributor.total_deletions.toLocaleString()"></span>--
                    </strong>
                  </p>
                </div>
              </div>
              <div class="ui attached segment">
                <div>
                  <canvas class="contributor-chart-canvas" role="img"></canvas>
                </div>
              </div>
            </div>
          </template>
        </div>
      </div>

      <style>
        .main-graph {
          height: 260px;
          padding-top: 2px;
        }

        .contributor-grid {
          display: grid;
          grid-template-columns: repeat(2, 1fr);
          gap: 1rem;
        }

        .contributor-grid > * {
          min-width: 0;
        }

        @media (max-width: 991.98px) {
          .contributor-grid {
            grid-template-columns: repeat(1, 1fr);
          }
        }

        .contributor-name {
          margin-bottom: 0;
        }

        .rotate-clockwise {
          animation: chart-rotate 1s linear infinite;
        }

        @keyframes chart-rotate {
          from { transform: rotate(0deg); }
          to { transform: rotate(360deg); }
        }

        /* Dropdown menu styles */
        .ui.dropdown .menu.show {
          display: block;
        }

        .ui.dropdown .menu {
          position: absolute;
          z-index: 100;
          background: var(--color-box-body);
          border: 1px solid var(--color-secondary);
          border-radius: var(--border-radius);
          box-shadow: var(--box-shadow);
          min-width: 160px;
        }

        .ui.dropdown .menu .item {
          padding: 0.75rem 1rem;
          cursor: pointer;
        }

        .ui.dropdown .menu .item:hover {
          background: var(--color-hover);
        }

        .ui.dropdown .menu .item.selected {
          color: var(--color-primary);
        }
      </style>
    </div>
  `;
}
