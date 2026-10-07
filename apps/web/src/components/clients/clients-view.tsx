"use client";

import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Archive, Loader2, Pencil, Plus, RotateCcw, Search, Users } from "lucide-react";
import { useLocale, useTranslations } from "next-intl";
import { useEffect, useId, useState, type FormEvent } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ApiError, requestJson } from "@/lib/api-client";
import {
  EMIRATES, type ClientCompany, type ClientField, type ClientForm, type Emirate, type FieldError,
  displayName, errorField, formFrom, toCreateBody, toPatchBody, validateClientForm,
} from "@/lib/clients";
import { formatInt } from "@/lib/format";
import { cn } from "@/lib/utils";

type StatusFilter = "active" | "archived" | "all";
type Page = { items: ClientCompany[]; next_cursor: string | null };

const EMPTY: ClientForm = { name: "", nameAr: "", trn: "", emirate: "" };

function listUrl(status: StatusFilter, q: string, cursor: string | null): string {
  const p = new URLSearchParams({ status, limit: "50" });
  if (q) p.set("q", q);
  if (cursor) p.set("cursor", cursor);
  return `/api/client-companies?${p.toString()}`;
}

/** /clients: list, search, filter, create, edit, archive and restore ClientCompanies. */
export function ClientsView() {
  const t = useTranslations("P1Clients");
  const locale = useLocale();
  const qc = useQueryClient();
  const [status, setStatus] = useState<StatusFilter>("active");
  const [search, setSearch] = useState("");
  const [q, setQ] = useState("");
  const [editing, setEditing] = useState<ClientCompany | "new" | null>(null);

  useEffect(() => {
    const id = setTimeout(() => setQ(search.trim().slice(0, 100)), 250);
    return () => clearTimeout(id);
  }, [search]);

  const list = useInfiniteQuery({
    queryKey: ["client-companies", status, q],
    queryFn: ({ pageParam, signal }) => requestJson<Page>(listUrl(status, q, pageParam), { signal }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next_cursor,
  });
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  // The counts are uploaded documents. Invoices created through the API carry no client, so while nothing
  // was uploaded for any listed company the columns would only show misleading zeros: hide them.
  const showDocCounts = rows.some((c) => (c.documents_total ?? 0) > 0);

  const toggle = useMutation({
    mutationFn: (c: ClientCompany) =>
      requestJson<ClientCompany>(`/api/client-companies/${encodeURIComponent(c.id)}/${c.status === "active" ? "archive" : "restore"}`, { method: "POST" }),
    onSuccess: (c) => {
      toast.success(t(c.status === "archived" ? "archived" : "restored", { name: displayName(c, locale) }));
      void qc.invalidateQueries({ queryKey: ["client-companies"] });
    },
    onError: () => toast.error(t("errors.generic")),
  });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-60 flex-1">
          <Search className="pointer-events-none absolute start-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t("search")}
            aria-label={t("search")}
            className="ps-8"
          />
        </div>
        <div role="radiogroup" aria-label={t("statusFilter")} className="flex rounded-lg border p-0.5">
          {(["active", "archived", "all"] as const).map((s) => (
            <button
              key={s}
              type="button"
              role="radio"
              aria-checked={status === s}
              onClick={() => setStatus(s)}
              className={cn("rounded-md px-3 py-1 text-sm", status === s ? "bg-muted font-medium" : "text-muted-foreground")}
            >
              {t(`status.${s}`)}
            </button>
          ))}
        </div>
        <Button data-testid="client-create" onClick={() => setEditing("new")}>
          <Plus /> {t("create")}
        </Button>
      </div>

      <div className="overflow-x-auto rounded-xl border bg-panel">
        <table className="w-full text-sm">
          <thead className="text-start text-xs text-muted-foreground">
            <tr className="border-b">
              <th className="px-4 py-2 text-start font-medium">{t("columns.name")}</th>
              <th className="px-4 py-2 text-start font-medium">{t("columns.trn")}</th>
              <th className="px-4 py-2 text-start font-medium">{t("columns.emirate")}</th>
              {showDocCounts && <th className="px-4 py-2 text-end font-medium">{t("columns.documents")}</th>}
              {showDocCounts && <th className="px-4 py-2 text-end font-medium">{t("columns.needsReview")}</th>}
              <th className="px-4 py-2" />
            </tr>
          </thead>
          <tbody>
            {rows.map((c) => (
              <tr key={c.id} data-testid="client-row" className="border-b last:border-0">
                <td className="px-4 py-2.5">
                  <div className="font-medium">{displayName(c, locale)}</div>
                  {c.status === "archived" && <div className="text-xs text-muted-foreground">{t("status.archived")}</div>}
                </td>
                <td className="px-4 py-2.5 font-mono tabular-nums" dir="ltr">{c.trn ?? "—"}</td>
                <td className="px-4 py-2.5">{c.emirate ? t(`emirates.${c.emirate}`) : "—"}</td>
                {showDocCounts && <td className="px-4 py-2.5 text-end tabular-nums">{formatInt(c.documents_total ?? 0)}</td>}
                {showDocCounts && (
                  <td className="px-4 py-2.5 text-end tabular-nums">
                    <span className={cn((c.documents_needs_review ?? 0) > 0 && "font-medium text-warn")}>
                      {formatInt(c.documents_needs_review ?? 0)}
                    </span>
                  </td>
                )}
                <td className="px-4 py-2.5">
                  <div className="flex justify-end gap-1">
                    <Button variant="ghost" size="icon-sm" aria-label={t("edit")} onClick={() => setEditing(c)}>
                      <Pencil />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={t(c.status === "active" ? "archive" : "restore")}
                      disabled={toggle.isPending}
                      onClick={() => toggle.mutate(c)}
                    >
                      {c.status === "active" ? <Archive /> : <RotateCcw />}
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {list.isPending && (
          <div className="flex justify-center p-6 text-muted-foreground"><Loader2 className="size-5 animate-spin" /></div>
        )}
        {list.isError && <p className="p-6 text-center text-sm text-bad">{t("errors.load")}</p>}
        {list.isSuccess && rows.length === 0 && (
          <div className="flex flex-col items-center gap-2 p-10 text-center text-muted-foreground">
            <Users className="size-6" />
            <p>{q ? t("emptySearch") : t("empty")}</p>
          </div>
        )}
      </div>
      {list.hasNextPage && (
        <Button variant="outline" className="self-center" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
          {t("loadMore")}
        </Button>
      )}

      <ClientDialog
        editing={editing}
        onClose={() => setEditing(null)}
        onSaved={() => void qc.invalidateQueries({ queryKey: ["client-companies"] })}
      />
    </div>
  );
}

function ClientDialog({
  editing, onClose, onSaved,
}: { editing: ClientCompany | "new" | null; onClose: () => void; onSaved: () => void }) {
  return (
    <Dialog open={editing !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        {editing !== null && (
          // Keyed per target so every open starts from that company's values.
          <ClientFormBody key={editing === "new" ? "new" : editing.id} editing={editing} onClose={onClose} onSaved={onSaved} />
        )}
      </DialogContent>
    </Dialog>
  );
}

function ClientFormBody({
  editing, onClose, onSaved,
}: { editing: ClientCompany | "new"; onClose: () => void; onSaved: () => void }) {
  const t = useTranslations("P1Clients");
  const locale = useLocale();
  const ids = { name: useId(), nameAr: useId(), trn: useId(), emirate: useId() };
  const [form, setForm] = useState<ClientForm>(() => (editing === "new" ? EMPTY : formFrom(editing)));
  const [errors, setErrors] = useState<Partial<Record<ClientField, FieldError | string>>>({});

  const save = useMutation({
    mutationFn: async (f: ClientForm) => {
      if (editing === "new") {
        return requestJson<ClientCompany>("/api/client-companies", { method: "POST", json: toCreateBody(f) });
      }
      const body = toPatchBody(editing, f);
      if (Object.keys(body).length === 0) return editing;
      return requestJson<ClientCompany>(`/api/client-companies/${encodeURIComponent(editing.id)}`, {
        method: "PATCH",
        json: body,
      });
    },
    onSuccess: (c) => {
      toast.success(t("saved", { name: displayName(c, locale) }));
      onSaved();
      onClose();
    },
    onError: (err) => {
      const code = err instanceof ApiError ? err.code : "generic";
      const field = errorField(code);
      if (field) setErrors({ [field]: code });
      else toast.error(t("errors.generic"));
    },
  });

  function submit(e: FormEvent) {
    e.preventDefault();
    const found = validateClientForm(form);
    setErrors(found);
    if (Object.keys(found).length === 0) save.mutate(form);
  }

  const field = (key: ClientField) =>
    errors[key] ? { "aria-invalid": true, "aria-describedby": `${ids[key]}-err` } : {};
  const err = (key: ClientField) =>
    errors[key] ? <p id={`${ids[key]}-err`} className="text-xs text-bad">{t(`errors.${errors[key]}`)}</p> : null;

  return (
    <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
      <DialogHeader>
        <DialogTitle>{editing === "new" ? t("createTitle") : t("editTitle")}</DialogTitle>
        <DialogDescription>{t("formHint")}</DialogDescription>
      </DialogHeader>
      <label htmlFor={ids.name} className="flex flex-col gap-1 text-sm">
        {t("fields.name")}
        <Input id={ids.name} value={form.name} maxLength={200} required
          onChange={(e) => setForm({ ...form, name: e.target.value })} {...field("name")} />
        {err("name")}
      </label>
      <label htmlFor={ids.nameAr} className="flex flex-col gap-1 text-sm">
        {t("fields.nameAr")}
        <Input id={ids.nameAr} dir="rtl" lang="ar" value={form.nameAr} maxLength={200}
          onChange={(e) => setForm({ ...form, nameAr: e.target.value })} {...field("nameAr")} />
        {err("nameAr")}
      </label>
      <label htmlFor={ids.trn} className="flex flex-col gap-1 text-sm">
        {t("fields.trn")}
        <Input id={ids.trn} dir="ltr" inputMode="numeric" className="font-mono" value={form.trn} maxLength={40}
          placeholder="100XXXXXXXXXXXX" onChange={(e) => setForm({ ...form, trn: e.target.value })} {...field("trn")} />
        {err("trn")}
      </label>
      <label htmlFor={ids.emirate} className="flex flex-col gap-1 text-sm">
        {t("fields.emirate")}
        <select id={ids.emirate} value={form.emirate}
          onChange={(e) => setForm({ ...form, emirate: e.target.value as "" | Emirate })}
          className="h-8 rounded-lg border bg-background px-2 text-sm" {...field("emirate")}>
          <option value="">{t("fields.emirateNone")}</option>
          {EMIRATES.map((e) => <option key={e} value={e}>{t(`emirates.${e}`)}</option>)}
        </select>
        {err("emirate")}
      </label>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>{t("cancel")}</Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending && <Loader2 className="animate-spin" />} {t("save")}
        </Button>
      </DialogFooter>
    </form>
  );
}
