/**
 * Money helpers. Amounts cross the wire as integer minor units (cents) and are
 * kept as integers in the UI; formatting/parsing is string-based so no
 * floating-point rounding ever touches a monetary value.
 */

const DECIMAL = /^(\d+)(?:\.(\d{0,2}))?$/;

/** Parse a decimal string like "12.5" into integer cents (1250). */
export function dollarsToCents(input: string): number {
  const match = DECIMAL.exec(input.trim());
  if (!match) {
    throw new Error('enter an amount like 100.00');
  }
  const whole = BigInt(match[1]);
  const fraction = BigInt((match[2] ?? '').padEnd(2, '0'));
  const cents = whole * 100n + fraction;
  if (cents > BigInt(Number.MAX_SAFE_INTEGER)) {
    throw new Error('amount is too large');
  }
  return Number(cents);
}

/** Format integer cents (1250) as a decimal string ("12.50"). */
export function centsToDollars(cents: number): string {
  const negative = cents < 0;
  const magnitude = BigInt(Math.abs(Math.trunc(cents)));
  const whole = magnitude / 100n;
  const fraction = magnitude % 100n;
  return `${negative ? '-' : ''}${whole}.${fraction.toString().padStart(2, '0')}`;
}
