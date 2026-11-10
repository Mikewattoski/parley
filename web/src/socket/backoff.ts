const BASE_MS = 1000;
const MAX_MS = 30_000;

/**
 * Delay before reconnect attempt number `attempt` (0-based): exponential
 * growth capped at 30 s, with "equal jitter" (between half and the full
 * delay) so a crowd of clients does not reconnect in lockstep after an outage.
 */
export function backoffDelay(attempt: number, random: () => number = Math.random): number {
  const exp = Math.min(MAX_MS, BASE_MS * 2 ** attempt);
  return exp / 2 + random() * (exp / 2);
}
