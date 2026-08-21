export type ApiErrorKind = 'config' | 'network' | 'http' | 'invalid-response';

export type ApiErrorMetadata = {
  httpStatus?: number;
  backendStatus?: string | number;
  requestId?: string;
};

export class ApiError extends Error {
  readonly kind: ApiErrorKind;
  readonly httpStatus?: number;
  readonly backendStatus?: string | number;
  readonly requestId?: string;

  constructor(
    kind: ApiErrorKind,
    message: string,
    metadata: ApiErrorMetadata = {},
    options?: { cause?: unknown },
  ) {
    super(message, options);
    this.name = 'ApiError';
    this.kind = kind;
    this.httpStatus = metadata.httpStatus;
    this.backendStatus = metadata.backendStatus;
    this.requestId = metadata.requestId;
  }
}

type GetJsonOptions = {
  query: Record<string, string>;
  signal?: AbortSignal;
};

type PostJsonOptions = {
  body: unknown;
  signal?: AbortSignal;
};

export type ApiJsonResult = {
  data: unknown;
  httpStatus: number;
  requestId?: string;
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function getBackendStatus(value: unknown): string | number | undefined {
  if (!isRecord(value)) {
    return undefined;
  }

  if (typeof value.status === 'string' || typeof value.status === 'number') {
    return value.status;
  }

  if (
    isRecord(value.error) &&
    (typeof value.error.status === 'string' || typeof value.error.status === 'number')
  ) {
    return value.error.status;
  }

  return undefined;
}

function buildApiUrl(path: string, query: Record<string, string>): string {
  const configuredBaseUrl = process.env.EXPO_PUBLIC_API_BASE_URL?.trim();

  if (!configuredBaseUrl) {
    throw new ApiError('config', 'The API base URL is not configured.');
  }

  let url: URL;

  try {
    url = new URL(configuredBaseUrl);
  } catch (error) {
    throw new ApiError('config', 'The API base URL is invalid.', {}, { cause: error });
  }

  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new ApiError('config', 'The API base URL must use HTTP or HTTPS.');
  }

  const normalizedPath = path.startsWith('/') ? path : `/${path}`;
  url.pathname = `${url.pathname.replace(/\/+$/, '')}${normalizedPath}`;
  url.search = '';
  url.hash = '';

  Object.entries(query).forEach(([key, value]) => {
    url.searchParams.set(key, value);
  });

  return url.toString();
}

export function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === 'AbortError';
}

async function parseJsonResponse(response: Response): Promise<ApiJsonResult> {
  const requestId = response.headers.get('X-Request-ID') ?? undefined;
  let data: unknown;

  try {
    const responseText = await response.text();
    data = responseText.length > 0 ? JSON.parse(responseText) : null;
  } catch (error) {
    if (!response.ok) {
      throw new ApiError(
        'http',
        'The API returned an error response.',
        { httpStatus: response.status, requestId },
        { cause: error },
      );
    }

    throw new ApiError(
      'invalid-response',
      'The API returned an invalid JSON response.',
      { httpStatus: response.status, requestId },
      { cause: error },
    );
  }

  if (!response.ok) {
    throw new ApiError('http', 'The API returned an error response.', {
      httpStatus: response.status,
      backendStatus: getBackendStatus(data),
      requestId,
    });
  }

  return { data, httpStatus: response.status, requestId };
}

export async function getJson(path: string, options: GetJsonOptions): Promise<ApiJsonResult> {
  const url = buildApiUrl(path, options.query);
  let response: Response;

  try {
    response = await fetch(url, {
      method: 'GET',
      headers: { Accept: 'application/json' },
      signal: options.signal,
    });
  } catch (error) {
    if (isAbortError(error)) {
      throw error;
    }

    throw new ApiError('network', 'The API request could not be completed.', {}, { cause: error });
  }

  return parseJsonResponse(response);
}

export async function postJson(path: string, options: PostJsonOptions): Promise<ApiJsonResult> {
  const url = buildApiUrl(path, {});
  let response: Response;

  try {
    response = await fetch(url, {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(options.body),
      signal: options.signal,
    });
  } catch (error) {
    if (isAbortError(error)) {
      throw error;
    }

    throw new ApiError('network', 'The API request could not be completed.', {}, { cause: error });
  }

  return parseJsonResponse(response);
}
