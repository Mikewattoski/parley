import { describe, expect, it } from 'vitest';
import { backoffDelay } from './backoff';

describe('backoffDelay', () => {
  it('starts at half a second to one second', () => {
    expect(backoffDelay(0, () => 0)).toBe(500);
    expect(backoffDelay(0, () => 1)).toBe(1000);
  });

  it('doubles with every attempt', () => {
    expect(backoffDelay(1, () => 1)).toBe(2000);
    expect(backoffDelay(3, () => 0)).toBe(4000);
    expect(backoffDelay(3, () => 1)).toBe(8000);
  });

  it('never exceeds 30 seconds, however many attempts', () => {
    expect(backoffDelay(20, () => 1)).toBe(30_000);
    expect(backoffDelay(20, () => 0)).toBe(15_000);
    expect(backoffDelay(1000, () => 1)).toBe(30_000);
  });

  it('is jittered: different random values give different delays', () => {
    expect(backoffDelay(4, () => 0.2)).not.toBe(backoffDelay(4, () => 0.8));
  });
});
