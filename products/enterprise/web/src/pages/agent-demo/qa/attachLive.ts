/** 最小上传：POST /v1/files → 拿 id，供 StartRun attachments 使用（禁止把 base64 塞进 Run）。 */

export type UploadedAttachment = {
  id: string;
  name: string;
  mime: string;
  role: string;
  sha256?: string;
  size?: number;
  workspace_alias?: string;
};

const API_BASE =
  (typeof process !== 'undefined' && process.env.UMI_APP_GENESIS_API) ||
  'http://127.0.0.1:8080';

export async function uploadFile(file: File): Promise<UploadedAttachment> {
  const fd = new FormData();
  fd.append('file', file);
  const res = await fetch(`${API_BASE}/v1/files`, { method: 'POST', body: fd });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`upload failed: ${res.status} ${text}`);
  }
  return (await res.json()) as UploadedAttachment;
}

export async function runWithAttachments(
  input: string,
  attachments: UploadedAttachment[],
): Promise<{ answer: string; status: string }> {
  const res = await fetch(`${API_BASE}/v1/runs`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      input,
      user_id: 'web-user',
      attachments: attachments.map((a) => ({
        id: a.id,
        name: a.name,
        mime: a.mime,
        role: a.role,
        sha256: a.sha256,
        size: a.size,
        workspace_alias: a.workspace_alias,
      })),
    }),
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`run failed: ${res.status} ${text}`);
  }
  const data = (await res.json()) as { answer?: string; status?: string };
  return { answer: data.answer ?? '', status: data.status ?? '' };
}

export { API_BASE };
