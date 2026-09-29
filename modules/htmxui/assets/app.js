// Teabag experimental frontend — htmxui
//
// Everything here is behaviour that neither plain HTML nor HTMX can express.
// Anything that can be a form, a link, <details> or an hx-* attribute lives in
// the templates instead, and is deliberately absent from this file.
'use strict';

// Clipboard: the async Clipboard API is not reachable from a plain link or
// htmx attribute, so the few places that need it opt in with data-x-copy.
// Delegated on document so it keeps working across htmx swaps.
document.addEventListener('click', async (ev) => {
  const trigger = ev.target.closest('[data-x-copy]');
  if (!trigger) return;
  const text = trigger.getAttribute('data-x-copy');
  if (!text) return;

  ev.preventDefault();
  // the result is reported by the button itself
  await copyAndReport(trigger, text);
});

async function copyAndReport(trigger, text) {
  let ok = false;
  // the clipboard API needs a secure context
  if (navigator.clipboard) {
    try {
      await navigator.clipboard.writeText(text);
      ok = true;
    } catch {
      // the browser can refuse a write, and the button has to say so
    }
  } else {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.append(ta);
    ta.select();
    try {
      // execCommand is deprecated, but it is the only copy a browser without
      // the clipboard API offers
      ok = document.execCommand('copy'); // eslint-disable-line @typescript-eslint/no-deprecated -- the fallback of the fallback
    } catch {
      // not implemented everywhere either
    }
    ta.remove();
  }
  report(trigger, ok);
}

// report shows the result in the button itself: the label is the accessible
// name of the button, so changing it is what makes the result observable to a
// screen reader, and the button goes back to its own text after a moment.
function report(trigger, ok) {
  trigger.classList.toggle('is-done', ok);
  // the value is what gets replaced, so the original has to be taken from it
  // and never from the whole button, which also carries the "Copy" label: the
  // reset would otherwise render "Copy Copy <value>"
  const value = trigger.querySelector('.x-clipboard-value') || trigger;
  const label = trigger.getAttribute('data-x-copy-done');
  // the original is remembered for the reset, so that a second click before
  // the timeout cannot remember the "copied" text instead; an explicit
  // data-x-copy-label always wins over the text of the value itself
  if (!trigger.getAttribute('data-x-copy-label')) {
    trigger.setAttribute('data-x-copy-label', value.textContent);
  }
  const original = trigger.getAttribute('data-x-copy-label');
  if (label) {
    value.textContent = ok ? label : original;
  }
  if (ok) {
    setTimeout(() => {
      trigger.classList.remove('is-done');
      value.textContent = original;
    }, 1500);
  }
}

// The file tree marks its folders with aria-expanded, but the expansion itself
// is a server render: the only thing the client knows is whether the child list
// it targets has content. Keeping that one bit in sync is the whole job.
function syncTreeExpanded(root) {
  for (const button of root.querySelectorAll('.x-tree-toggle[aria-expanded]')) {
    // the id comes from an attribute, not from a selector, and it may legally
    // contain characters that are not valid in one
    // eslint-disable-next-line unicorn/prefer-query-selector -- see above
    const list = document.getElementById(button.getAttribute('aria-controls'));
    button.setAttribute('aria-expanded', list?.children.length ? 'true' : 'false');
  }
}

document.addEventListener('htmx:afterSwap', () => syncTreeExpanded(document));
document.addEventListener('DOMContentLoaded', () => syncTreeExpanded(document));
