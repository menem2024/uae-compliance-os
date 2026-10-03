"use client";

import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useTranslations } from "next-intl";
import { ApiError, requestJson } from "@/lib/api-client";
import { sha256Hex } from "@/lib/sha256";
import {
  runUploads, type CompleteResponseItem, type UploadDeps, type UploadResponseItem,
} from "@/lib/upload-queue";
import {
  documentsNeedPolling, isTerminal, settleFromRows, uploadReducer, type DocumentRow, type UploadAction, type UploadItem,
} from "@/lib/uploads";
import { ClientPicker } from "./client-picker";
import { Dropzone } from "./dropzone";
import { DocumentsTable } from "./documents-table";
import { UploadQueueView } from "./upload-queue-view";

type Page = { items: DocumentRow[]; next_cursor: string | null };

const POLL_MS = 3000;

/** PUT with progress (fetch has no upload progress). The signed URL carries the auth; no cookies. */
function putFile(
  target: { url: string; method: string; headers: Record<string, string> },
  file: File,
  onProgress: (fraction: number) => void,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(target.method, target.url);
    for (const [k, v] of Object.entries(target.headers)) xhr.setRequestHeader(k, v);
    xhr.upload.onprogress = (e) => e.lengthComputable && onProgress(e.loaded / e.total);
    xhr.onload = () => (xhr.status >= 200 && xhr.status < 300 ? resolve() : reject(new ApiError(xhr.status, "upload_failed", null)));
    xhr.onerror = () => reject(new ApiError(0, "upload_failed", null));
    xhr.send(file);
  });
}

const deps: UploadDeps = {
  hash: sha256Hex,
  requestUploads: async (clientCompanyId, files) =>
    (await requestJson<{ items: UploadResponseItem[] }>("/api/documents/uploads", {
      method: "POST",
      json: { client_company_id: clientCompanyId, files },
    })).items,
  put: putFile,
  complete: async (ids) =>
    (await requestJson<{ items: CompleteResponseItem[] }>("/api/documents/complete", {
      method: "POST",
      json: { document_ids: ids },
    })).items,
};

/** /documents: pick a ClientCompany, drop files, watch the queue, browse the Documents. */
export function DocumentsView() {
  const t = useTranslations("P1Documents");
  const qc = useQueryClient();
  const [clientId, setClientId] = useState("");
  const [raw, setItems] = useState<UploadItem[]>([]);
  const rawRef = useRef<UploadItem[]>(raw);

  const dispatch = useCallback((a: UploadAction) => setItems((prev) => uploadReducer(prev, a)), []);

  const list = useInfiniteQuery({
    queryKey: ["documents", clientId],
    enabled: clientId !== "",
    queryFn: ({ pageParam, signal }) => {
      const p = new URLSearchParams({ client_company_id: clientId, limit: "100" });
      if (pageParam) p.set("cursor", pageParam);
      return requestJson<Page>(`/api/documents?${p.toString()}`, { signal });
    },
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next_cursor,
    // Poll only while something is unsettled; react-query pauses it in a background tab.
    refetchInterval: (query) => {
      const latest = query.state.data?.pages.flatMap((p) => p.items) ?? [];
      return documentsNeedPolling(latest, settleFromRows(rawRef.current, latest)) ? POLL_MS : false;
    },
  });
  const rows = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const items = useMemo(() => settleFromRows(raw, rows), [raw, rows]);
  useEffect(() => {
    rawRef.current = raw;
  }, [raw]);

  const onFiles = (files: File[]) => {
    if (!clientId) return toast.error(t("dropzone.needClient"));
    const added: UploadItem[] = files.map((file) => ({
      id: crypto.randomUUID(),
      file,
      state: "hashing",
      progress: 0,
    }));
    setItems((prev) => [...prev, ...added]);
    void runUploads({ items: added, clientCompanyId: clientId, dispatch, deps }).then(() => {
      void qc.invalidateQueries({ queryKey: ["documents", clientId] });
    });
  };

  return (
    <div className="flex flex-col gap-6">
      <ClientPicker value={clientId} onChange={setClientId} />
      <Dropzone disabled={!clientId} onFiles={onFiles} />
      <UploadQueueView items={items} onClear={() => {
          const done = new Set(items.filter((i) => isTerminal(i.state)).map((i) => i.id));
          setItems((prev) => prev.filter((i) => !done.has(i.id)));
        }} />
      <DocumentsTable
        rows={rows}
        loading={clientId !== "" && list.isPending}
        error={list.isError}
        hasMore={list.hasNextPage}
        loadingMore={list.isFetchingNextPage}
        onLoadMore={() => void list.fetchNextPage()}
      />
    </div>
  );
}
