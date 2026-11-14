import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { vi, type Mock } from 'vitest';

import { api, type ApiKey, type CreatedApiKey } from '@/api/client';
import { ApiKeysCard } from '@/components/ApiKeysCard';
import { useSession } from '@/auth/session';

vi.mock('@/auth/session', () => ({ useSession: vi.fn() }));

const withClient = (ui: ReactElement) => (
  <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{ui}</QueryClientProvider>
);

const keys: ApiKey[] = [
  { id: 'k1', name: 'uup-k8s-agent', key_prefix: 'argus_ab', label: 'cluster=uup-k8s', rate_limit: 0, created_at: '2026-01-01T00:00:00Z', last_used_at: null, revoked_at: null },
  { id: 'k2', name: 'old-agent', key_prefix: 'argus_cd', label: '', rate_limit: 100, created_at: '2025-01-01T00:00:00Z', last_used_at: null, revoked_at: '2026-02-01T00:00:00Z' },
];

beforeEach(() => {
  (useSession as unknown as Mock).mockImplementation((sel: (s: unknown) => unknown) =>
    sel({ status: 'signedIn', user: { role: 'admin' } }),
  );
  vi.spyOn(api, 'GET').mockResolvedValue({ data: keys, error: undefined, response: new Response() } as never);
});

afterEach(() => vi.restoreAllMocks());

describe('ApiKeysCard', () => {
  it('lists keys, marking a revoked one and hiding its Revoke action', async () => {
    render(withClient(<ApiKeysCard />));
    expect(await screen.findByText('uup-k8s-agent')).toBeTruthy();
    expect(screen.getByText(/old-agent · revoked/)).toBeTruthy();
    expect(screen.getByLabelText('Revoke uup-k8s-agent')).toBeTruthy();
    expect(screen.queryByLabelText('Revoke old-agent')).toBeNull();
  });

  it('requires a name before creating a key', async () => {
    const post = vi.spyOn(api, 'POST');
    render(withClient(<ApiKeysCard />));
    await screen.findByText('uup-k8s-agent');

    fireEvent.click(screen.getByLabelText('Create key'));
    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(post).not.toHaveBeenCalled();
  });

  it('creates a key and shows the one-time plaintext value', async () => {
    const createdKey: CreatedApiKey = { ...keys[0], id: 'k3', name: 'new-agent', key: 'argus_plaintext_abc123' };
    const post = vi.spyOn(api, 'POST').mockResolvedValue({ data: createdKey, error: undefined, response: new Response() } as never);
    render(withClient(<ApiKeysCard />));
    await screen.findByText('uup-k8s-agent');

    fireEvent.change(screen.getByLabelText('API key name'), { target: { value: 'new-agent' } });
    fireEvent.click(screen.getByLabelText('Create key'));

    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/api-keys', expect.objectContaining({ body: { name: 'new-agent' } })),
    );
    expect(await screen.findByText('argus_plaintext_abc123')).toBeTruthy();
  });

  it('revokes a key', async () => {
    const del = vi.spyOn(api, 'DELETE').mockResolvedValue({ data: undefined, error: undefined, response: new Response(null, { status: 204 }) } as never);
    render(withClient(<ApiKeysCard />));
    await screen.findByText('uup-k8s-agent');

    fireEvent.click(screen.getByLabelText('Revoke uup-k8s-agent'));

    await waitFor(() =>
      expect(del).toHaveBeenCalledWith('/api-keys/{id}', expect.objectContaining({ params: { path: { id: 'k1' } } })),
    );
  });
});
