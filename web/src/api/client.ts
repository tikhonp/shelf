// ApiError is an answer that isn't 2xx, with the message shelf gave.
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message)
  }
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init)
  if (res.ok) return (await res.json()) as T
  let message = res.statusText
  try {
    const body = (await res.json()) as { error?: string }
    if (body.error) message = body.error
  } catch {
    // The gateway's own error pages aren't JSON, and the status text says enough.
  }
  throw new ApiError(res.status, message)
}

export function get<T>(path: string, signal?: AbortSignal): Promise<T> {
  return call<T>(path, signal ? { signal } : undefined)
}

export function post<T>(path: string, body?: unknown): Promise<T> {
  return call<T>(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

// errorText is the message to show for anything a call threw.
export function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}
