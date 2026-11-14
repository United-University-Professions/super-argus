import { useState, type CSSProperties } from 'react';

import { errorMessage, type CreatedApiKey } from '@/api/client';
import { useApiKeyMutations, useApiKeys } from '@/hooks/useApiKeys';
import { useTheme } from '@/theme';

import { Button, Card, Chip, ErrorText } from './ui';

/** Admin-only management of API keys (ingest/agent auth). */
export function ApiKeysCard() {
  const t = useTheme();
  const keys = useApiKeys();
  const { createApiKey, revokeApiKey } = useApiKeyMutations();
  const [name, setName] = useState('');
  const [label, setLabel] = useState('');
  const [rateLimit, setRateLimit] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [created, setCreated] = useState<CreatedApiKey | null>(null);
  const [copied, setCopied] = useState(false);

  const add = () => {
    setError(null);
    if (!name.trim()) return setError('Name is required');
    let limit: number | undefined;
    if (rateLimit.trim()) {
      limit = Number(rateLimit);
      if (!Number.isInteger(limit) || limit < 0) return setError('Rate limit must be a non-negative integer');
    }
    createApiKey.mutate(
      { name: name.trim(), ...(label.trim() ? { label: label.trim() } : {}), ...(limit !== undefined ? { rate_limit: limit } : {}) },
      {
        onSuccess: (k) => {
          setCreated(k);
          setCopied(false);
          setName('');
          setLabel('');
          setRateLimit('');
        },
        onError: (e) => setError(errorMessage(e, 'Could not create the key')),
      },
    );
  };

  const copyKey = async () => {
    if (!created) return;
    try {
      await navigator.clipboard.writeText(created.key);
      setCopied(true);
    } catch {
      // clipboard access can be denied (permissions, non-secure context) -- the
      // key stays visible on screen either way, so this is a soft failure.
      setCopied(false);
    }
  };

  const field: CSSProperties = {
    color: t.text, backgroundColor: t.surface, borderColor: t.border, borderWidth: 1, borderStyle: 'solid',
    borderRadius: 8, padding: '8px 10px', fontSize: 14, width: '100%',
  };

  return (
    <Card style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <span style={{ color: t.text, fontSize: 16, fontWeight: 700 }}>API keys</span>

      {created && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6, padding: 10, borderRadius: 8, backgroundColor: t.surfaceAlt, borderColor: t.accent, borderWidth: 1, borderStyle: 'solid' }}>
          <span style={{ color: t.textDim, fontSize: 12 }}>
            Copy this now -- it won&apos;t be shown again.
          </span>
          <code style={{ color: t.text, fontSize: 13, wordBreak: 'break-all' }}>{created.key}</code>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <Chip label={copied ? 'Copied' : 'Copy'} accessibilityLabel="Copy API key" onPress={() => void copyKey()} />
            <Chip label="Dismiss" accessibilityLabel="Dismiss new API key" onPress={() => setCreated(null)} />
          </div>
        </div>
      )}

      {keys.data?.length === 0 && <span style={{ color: t.textDim }}>No API keys yet.</span>}
      {keys.data?.map((k) => (
        <div key={k.id} style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <span style={{ color: t.text, fontWeight: 600 }}>
            {k.name}
            {k.revoked_at ? ' · revoked' : ''}
          </span>
          <span style={{ color: t.textDim, fontSize: 12 }}>
            {k.key_prefix}… {k.label ? `· ${k.label}` : ''} {k.rate_limit ? `· limit ${k.rate_limit}/s` : ''}
          </span>
          <span style={{ color: t.textDim, fontSize: 12 }}>
            Created {new Date(k.created_at).toLocaleString()}
            {k.last_used_at ? ` · last used ${new Date(k.last_used_at).toLocaleString()}` : ' · never used'}
          </span>
          {!k.revoked_at && (
            <div style={{ display: 'flex', gap: 8 }}>
              <Chip
                label="Revoke"
                accessibilityLabel={`Revoke ${k.name}`}
                onPress={() => revokeApiKey.mutate(k.id, { onError: (e) => setError(errorMessage(e, 'Could not revoke the key')) })}
              />
            </div>
          )}
        </div>
      ))}

      <input aria-label="API key name" placeholder="Name" value={name} onChange={(e) => setName(e.target.value)} style={field} />
      <input aria-label="API key label" placeholder="Label (optional)" value={label} onChange={(e) => setLabel(e.target.value)} style={field} />
      <input
        aria-label="API key rate limit"
        placeholder="Rate limit, lines/sec (optional)"
        value={rateLimit}
        onChange={(e) => setRateLimit(e.target.value)}
        inputMode="numeric"
        style={field}
      />
      {error && <ErrorText>{error}</ErrorText>}
      <Button title="Create key" onPress={add} loading={createApiKey.isPending} />
    </Card>
  );
}
