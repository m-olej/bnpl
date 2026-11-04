import type { ApiErrorBody } from './types';

/**
 * Placeholder identity headers (see the API's OpenAPI spec). They guard every
 * `/api/` route until real token verification replaces them.
 */
const ACTOR_HEADERS: Record<string, string> = {
  'X-Actor-Type': 'ADMIN',
  'X-Actor-Id': 'frontend',
};

/** An HTTP error carrying the server's machine-readable code. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

async function toApiError(res: Response): Promise<ApiError> {
  let code = 'unknown_error';
  let message = `request failed with status ${res.status}`;
  try {
    const body = (await res.json()) as ApiErrorBody;
    if (body.error) {
      code = body.error.code;
      message = body.error.message;
    }
  } catch {
    // Non-JSON error body; keep the defaults.
  }
  return new ApiError(res.status, code, message);
}

async function request<T>(
  base: string,
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const method = (init.method ?? 'GET').toUpperCase();
  const headers = new Headers(init.headers);
  headers.set('Content-Type', 'application/json');
  for (const [name, value] of Object.entries(ACTOR_HEADERS)) {
    headers.set(name, value);
  }
  // AGENTS.md: every mutating call needs an Idempotency-Key.
  if (method !== 'GET' && method !== 'HEAD') {
    headers.set('Idempotency-Key', crypto.randomUUID());
  }

  const res = await fetch(`${base}${path}`, { ...init, method, headers });
  if (!res.ok) {
    throw await toApiError(res);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

/** Call the checkout service (proxied under `/checkout`). */
export function checkoutFetch<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  return request<T>('/checkout', path, init);
}

/** Call the underwriting service (proxied under `/underwriting`). */
export function underwritingFetch<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  return request<T>('/underwriting', path, init);
}
