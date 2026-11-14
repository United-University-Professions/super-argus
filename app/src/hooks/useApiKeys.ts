import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, unwrap, type CreateApiKeyRequest } from '@/api/client';
import { useSession } from '@/auth/session';

/** API keys are admin-only; viewers/editors would just get a 403. */
export function useApiKeys(enabled = true) {
  const on = useSession((s) => s.status === 'signedIn');
  return useQuery({ queryKey: ['api-keys'], enabled: on && enabled, queryFn: async () => unwrap(await api.GET('/api-keys')) });
}

export function useApiKeyMutations() {
  const qc = useQueryClient();
  const refresh = () => void qc.invalidateQueries({ queryKey: ['api-keys'] });
  return {
    createApiKey: useMutation({
      mutationFn: async (body: CreateApiKeyRequest) => unwrap(await api.POST('/api-keys', { body })),
      onSuccess: refresh,
    }),
    revokeApiKey: useMutation({
      mutationFn: async (id: string) => {
        const r = await api.DELETE('/api-keys/{id}', { params: { path: { id } } });
        if (r.error !== undefined) unwrap(r as never);
      },
      onSuccess: refresh,
    }),
  };
}
