// The clipboard button is the one place in the experimental frontend that
// rewrites text it does not own: it replaces the clone URL with a "copied"
// label and puts the original back a moment later. The reset used to remember
// the text of the whole button, which carries the "Copy" label as well, and
// wrote that back into the value — "Copy Copy <value>".

import './assets/app.js';

const cloneURL = 'https://teabag.example/user2/repo1.git';

function renderButton(doneLabel = 'Copied'): HTMLElement {
  const button = document.createElement('button');
  button.className = 'x-btn x-clipboard';
  button.setAttribute('data-x-copy', cloneURL);
  button.setAttribute('data-x-copy-done', doneLabel);
  const value = document.createElement('span');
  value.className = 'mono x-clipboard-value';
  value.textContent = cloneURL;
  button.append('Copy ', value);
  document.body.append(button);
  return button;
}

const valueOf = (button: HTMLElement) => button.querySelector('.x-clipboard-value')!.textContent;

// the handler is asynchronous, so a click only reports after the clipboard
// write settles on a later tick
const click = async (button: HTMLElement) => {
  button.click();
  await vi.advanceTimersByTimeAsync(0);
};

beforeEach(() => {
  vi.useFakeTimers();
  Object.defineProperty(navigator, 'clipboard', {
    value: {writeText: vi.fn().mockResolvedValue(undefined)},
    configurable: true,
  });
});

afterEach(() => {
  vi.useRealTimers();
  document.body.replaceChildren();
});

test('a successful copy shows the label and restores the value, not the whole button', async () => {
  const button = renderButton();
  await click(button);
  expect(navigator.clipboard.writeText).toHaveBeenCalledWith(cloneURL);
  expect(valueOf(button)).toBe('Copied');
  expect(button.classList.contains('is-done')).toBe(true);

  await vi.advanceTimersByTimeAsync(1500);
  expect(valueOf(button)).toBe(cloneURL);
  expect(button.textContent).toBe(`Copy ${cloneURL}`);
  expect(button.classList.contains('is-done')).toBe(false);
});

test('a refused copy leaves the button as it was', async () => {
  const button = renderButton();
  vi.mocked(navigator.clipboard.writeText).mockRejectedValue(new Error('denied'));
  await click(button);
  expect(valueOf(button)).toBe(cloneURL);
  expect(button.classList.contains('is-done')).toBe(false);
});

test('an explicit copy label wins over the text of the value', async () => {
  const button = renderButton('Kopiert');
  button.setAttribute('data-x-copy-label', cloneURL);
  await click(button);
  expect(valueOf(button)).toBe('Kopiert');
  await vi.advanceTimersByTimeAsync(1500);
  expect(valueOf(button)).toBe(cloneURL);
});
