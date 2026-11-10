import { useState, type FormEvent } from 'react';
import { authenticate, clearAuthError } from '../store/authSlice';
import { useAppDispatch, useAppSelector } from '../store/hooks';

type Mode = 'login' | 'register';

export function AuthScreen() {
  const dispatch = useAppDispatch();
  const { status, error } = useAppSelector((s) => s.auth);
  const [mode, setMode] = useState<Mode>('login');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const loading = status === 'loading';

  const switchMode = (next: Mode) => {
    setMode(next);
    dispatch(clearAuthError());
  };

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (loading) return;
    void dispatch(authenticate({ mode, username: username.trim(), password }));
  };

  return (
    <div className="auth-screen">
      <form className="auth-card" onSubmit={submit}>
        <h1 className="auth-title">Parley</h1>
        <p className="auth-sub">Real-time chat rooms</p>

        <div className="mode-switch">
          <button
            type="button"
            className={mode === 'login' ? 'active' : ''}
            aria-pressed={mode === 'login'}
            onClick={() => switchMode('login')}
          >
            Log in
          </button>
          <button
            type="button"
            className={mode === 'register' ? 'active' : ''}
            aria-pressed={mode === 'register'}
            onClick={() => switchMode('register')}
          >
            Register
          </button>
        </div>

        <label className="field">
          <span>Username</span>
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            maxLength={32}
            required
          />
        </label>

        <label className="field">
          <span>Password</span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
            minLength={mode === 'register' ? 8 : undefined}
            maxLength={72}
            required
          />
        </label>

        {mode === 'register' && (
          <p className="hint">Username: 3-32 characters (letters, digits, _ or -). Password: at least 8 characters.</p>
        )}
        {error && (
          <p className="error-text" role="alert">
            {error}
          </p>
        )}

        <button type="submit" className="btn btn-primary" disabled={loading}>
          {loading ? 'Please wait…' : mode === 'login' ? 'Log in' : 'Create account'}
        </button>
      </form>
    </div>
  );
}
