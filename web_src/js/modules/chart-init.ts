/**
 * Chart.js initialization utilities for HTMX + Alpine.js
 *
 * This module provides a clean API for initializing and managing Chart.js charts
 * without Vue. It's designed to work with server-rendered HTML that includes
 * chart configuration in data attributes.
 */

import {
  _adapters,
  BarController,
  Chart,
  LinearScale,
  LineController,
  TimeScale,
  type ChartData,
  type ChartOptions,
  type TimeUnit,
} from 'chart.js';
import { chartJsColors } from '../utils/color.ts';
import dayjs from 'dayjs';
import advancedFormat from 'dayjs/plugin/advancedFormat.js';
import quarterOfYear from 'dayjs/plugin/quarterOfYear.js';
import type { ConfigType, ManipulateType } from 'dayjs';

// Extend dayjs with required plugins
dayjs.extend(advancedFormat);
dayjs.extend(quarterOfYear);

// Configure Chart.js defaults
Chart.defaults.color = chartJsColors.text;
Chart.defaults.borderColor = chartJsColors.border;
Chart.register(BarController, LineController, LinearScale, TimeScale);

// minimal port of chartjs-adapter-dayjs-4, MIT license, Copyright (c) 2022 bolstycjw
_adapters._date.override({
  formats: () => ({
    datetime: 'MMM D, YYYY, h:mm:ss a',
    millisecond: 'h:mm:ss.SSS a',
    second: 'h:mm:ss a',
    minute: 'h:mm a',
    hour: 'hA',
    day: 'MMM D',
    week: 'MMM D, YYYY',
    month: 'MMM YYYY',
    quarter: '[Q]Q - YYYY',
    year: 'YYYY',
  }),
  parse: (value: ConfigType) => {
    const date = dayjs(value);
    return date.isValid() ? date.valueOf() : null;
  },
  format: (time: number, format: string) => dayjs(time).format(format),
  add: (time: number, amount: number, unit: TimeUnit) =>
    dayjs(time).add(amount, unit as ManipulateType).valueOf(),
  diff: (max: number, min: number, unit: TimeUnit) => dayjs(max).diff(min, unit),
  startOf: (time: number, unit: TimeUnit) => dayjs(time).startOf(unit).valueOf(),
  endOf: (time: number, unit: TimeUnit) => dayjs(time).endOf(unit).valueOf(),
});

// Store chart instances by canvas ID to allow cleanup
export const chartInstances = new Map<string, Chart>();

/**
 * Initialize a Chart.js chart on a canvas element
 * @param canvas - The canvas element or its ID
 * @param type - Chart type ('bar' or 'line')
 * @param data - Chart data
 * @param options - Chart options
 * @returns The Chart.js instance
 */
export function initChart(
  canvas: HTMLCanvasElement | string,
  type: 'bar' | 'line',
  data: ChartData,
  options: ChartOptions = {},
) {
  const canvasEl = typeof canvas === 'string' ? document.querySelector(canvas) : canvas;

  if (!canvasEl) {
    throw new Error(`Canvas element not found: ${String(canvas)}`);
  }

  // Destroy existing chart if present
  const existingChart = chartInstances.get(canvasEl.id);
  if (existingChart) {
    existingChart.destroy();
    chartInstances.delete(canvasEl.id);
  }

  // Create new chart - use toRaw equivalent by cloning if needed
  // Chart.js mutates the data, so we need to ensure we're not passing reactive proxies
  const chartData = structuredClone(data);
  const chartOptions = structuredClone(options);

  const chart = new Chart(canvasEl, {
    type,
    data: chartData,
    options: chartOptions,
  });

  chartInstances.set(canvasEl.id, chart);
  return chart;
}

/**
 * Update an existing chart with new data and options
 * @param canvas - The canvas element or its ID
 * @param data - New chart data
 * @param options - New chart options (optional)
 */
export function updateChart(
  canvas: HTMLCanvasElement | string,
  data: ChartData,
  options?: ChartOptions,
) {
  const canvasEl = typeof canvas === 'string' ? document.querySelector(canvas) : canvas;

  if (!canvasEl) {
    return;
  }

  const chart = chartInstances.get(canvasEl.id);
  if (!chart) {
    return;
  }

  // Clone data to prevent mutation of original
  chart.data = structuredClone(data);
  if (options) {
    chart.options = structuredClone(options);
  }
  chart.update();
}

/**
 * Destroy a chart instance
 * @param canvas - The canvas element or its ID
 */
export function destroyChart(canvas: HTMLCanvasElement | string) {
  const canvasEl = typeof canvas === 'string' ? document.querySelector(canvas) : canvas;

  if (!canvasEl) {
    return;
  }

  const chart = chartInstances.get(canvasEl.id);
  if (chart) {
    chart.destroy();
    chartInstances.delete(canvasEl.id);
  }
}

/**
 * Initialize chart from data attributes on a canvas element
 * Expected data attributes:
 * - data-chart-type: 'bar' or 'line'
 * - data-chart-data: JSON string of ChartData
 * - data-chart-options: JSON string of ChartOptions (optional)
 * @param canvas - The canvas element or its ID
 * @returns The Chart.js instance or null if initialization failed
 */
export function initChartFromDataAttributes(
  canvas: HTMLCanvasElement | string,
) {
  const canvasEl = typeof canvas === 'string' ? document.querySelector(canvas) : canvas;

  if (!canvasEl) {
    return null;
  }

  const type = canvasEl.getAttribute('data-chart-type') as 'bar' | 'line' | null;
  const dataStr = canvasEl.getAttribute('data-chart-data');
  const optionsStr = canvasEl.getAttribute('data-chart-options');

  if (!type || !dataStr) {
    console.warn(`Canvas ${canvasEl.id} missing required data attributes`);
    return null;
  }

  try {
    const data: ChartData = JSON.parse(dataStr);
    const options: ChartOptions = optionsStr ? JSON.parse(optionsStr) : {};
    return initChart(canvasEl, type, data, options);
  } catch (err) {
    console.error(`Failed to parse chart data for canvas ${canvasEl.id}:`, err);
    return null;
  }
}

/**
 * Initialize all charts in a container that have data attributes
 * @param container - The container element (defaults to document)
 */
export function initChartsInContainer(
  container: HTMLElement | Document = document,
) {
  const canvases = container.querySelectorAll<HTMLCanvasElement>(
    'canvas[data-chart-type][data-chart-data]:not([data-chart-initialized])',
  );

  canvases.forEach((canvas) => {
    const chart = initChartFromDataAttributes(canvas);
    if (chart) {
      canvas.setAttribute('data-chart-initialized', 'true');
    }
  });
}

/**
 * Cleanup all charts in a container
 * @param container - The container element (defaults to document)
 */
export function cleanupChartsInContainer(
  container: HTMLElement | Document = document,
) {
  const canvases = container.querySelectorAll<HTMLCanvasElement>(
    'canvas[data-chart-initialized]',
  );
  canvases.forEach((canvas) => {
    destroyChart(canvas);
    canvas.removeAttribute('data-chart-initialized');
  });
}

// Auto-initialize charts on HTMX events
if (typeof document !== 'undefined') {
  document.addEventListener('DOMContentLoaded', () => {
    initChartsInContainer(document);
  });

  // Initialize charts after HTMX swaps content
  document.body.addEventListener('htmx:afterSwap', (evt: CustomEvent) => {
    if (evt.detail?.successful && evt.detail?.elt) {
      initChartsInContainer(evt.detail.elt);
    }
  });

  // Cleanup charts before HTMX removes content
  document.body.addEventListener('htmx:beforeSwap', (evt: CustomEvent) => {
    if (evt.detail?.elt) {
      cleanupChartsInContainer(evt.detail.elt);
    }
  });
}
