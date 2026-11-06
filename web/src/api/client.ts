let unauthorized: (() => void) | undefined
let paused: (() => boolean) | undefined
export function setAuthHandling(onUnauthorized: () => void, isPaused: () => boolean) {
  unauthorized = onUnauthorized
  paused = isPaused
}

// ApiError is an answer that isn't 2xx, with the message shelf gave.
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
    // body is the whole answer, for the calls whose refusals carry more than a message.
    readonly body?: unknown,
  ) {
    super(message)
  }
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  if (paused?.()) throw new ApiError(401, "Sign in to Shelf")
  const res = await fetch(path, init)
  if (res.status === 401) unauthorized?.()
  if (res.ok) return (await res.json()) as T
  let message = res.statusText
  let body: { error?: string } | undefined
  try {
    body = (await res.json()) as { error?: string }
    if (body.error) message = body.error
  } catch {
    // The gateway's own error pages aren't JSON, and the status text says enough.
  }
  throw new ApiError(res.status, message, body)
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

export function del<T>(path: string): Promise<T> {
  return call<T>(path, { method: 'DELETE' })
}

// upload sends a file as the raw body.
export function upload<T>(path: string, file: Blob): Promise<T> {
  return call<T>(path, { method: 'POST', headers: { 'Content-Type': file.type }, body: file })
}

// errorText is the message to show for anything a call threw.
export function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}
