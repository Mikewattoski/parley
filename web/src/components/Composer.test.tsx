import { configureStore } from '@reduxjs/toolkit';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Provider } from 'react-redux';
import { describe, expect, it } from 'vitest';
import type { ConnectionStatus } from '../store/connectionSlice';
import { rootReducer } from '../store/rootReducer';
import { Composer } from './Composer';

function renderComposer(status: ConnectionStatus) {
  const store = configureStore({
    reducer: rootReducer,
    preloadedState: {
      auth: { token: 't', user: { id: 1, username: 'alice' }, status: 'idle' as const, error: null },
      connection: { status, attempt: 0, epoch: 1, notice: null },
    },
  });
  render(
    <Provider store={store}>
      <Composer room="general" />
    </Provider>,
  );
  return { store, box: screen.getByRole('textbox', { name: /message/i }) };
}

describe('Composer', () => {
  it('sends on Enter and clears the field', async () => {
    const user = userEvent.setup();
    const { store, box } = renderComposer('connected');

    await user.type(box, 'hello{Enter}');

    const messages = store.getState().chat.rooms.general.messages;
    expect(messages).toHaveLength(1);
    expect(messages[0]).toMatchObject({ text: 'hello', from: 'alice', status: 'sending' });
    expect(box).toHaveValue('');
  });

  it('adds a new line on Shift+Enter instead of sending', async () => {
    const user = userEvent.setup();
    const { store, box } = renderComposer('connected');

    await user.type(box, 'a{Shift>}{Enter}{/Shift}b');

    expect(box).toHaveValue('a\nb');
    expect(store.getState().chat.rooms.general).toBeUndefined();
  });

  it('does not send blank messages', async () => {
    const user = userEvent.setup();
    const { store, box } = renderComposer('connected');

    await user.type(box, '   {Enter}');

    expect(store.getState().chat.rooms.general).toBeUndefined();
    expect(screen.getByRole('button', { name: /send/i })).toBeDisabled();
  });

  it('cannot send while the connection is down', async () => {
    const user = userEvent.setup();
    const { store, box } = renderComposer('reconnecting');

    await user.type(box, 'hello{Enter}');

    expect(screen.getByRole('button', { name: /send/i })).toBeDisabled();
    expect(store.getState().chat.rooms.general).toBeUndefined();
    expect(box).toHaveValue('hello'); // the text is kept, so nothing is lost
  });
});
