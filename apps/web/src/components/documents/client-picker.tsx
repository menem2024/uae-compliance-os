"use client";

import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useLocale, useTranslations } from "next-intl";
import { useEffect, useId } from "react";
import { Link } from "@/i18n/navigation";
import { requestJson } from "@/lib/api-client";
import { displayName, type ClientCompany } from "@/lib/clients";

type Page = { items: ClientCompany[]; next_cursor: string | null };

/** Chooses the ClientCompany that new uploads belong to and the table is filtered by. */
export function ClientPicker({ value, onChange }: { value: string; onChange: (id: string) => void }) {
  const t = useTranslations("P1Documents.client");
  const locale = useLocale();
  const id = useId();
  const list = useQuery({
    queryKey: ["client-companies", "active", "picker"],
    queryFn: ({ signal }) => requestJson<Page>("/api/client-companies?status=active&limit=100", { signal }),
  });
  const items = list.data?.items;

  // Select the first company once; drop a selection that is no longer an active company.
  useEffect(() => {
    if (!items) return;
    if (items.length > 0 && !items.some((c) => c.id === value)) onChange(items[0]!.id);
    if (items.length === 0 && value) onChange("");
  }, [items, value, onChange]);

  return (
    <div className="flex flex-col gap-1 text-sm">
      <label htmlFor={id} className="font-medium">{t("label")}</label>
      <div className="flex items-center gap-2">
        <select
          id={id}
          data-testid="client-picker"
          value={value}
          disabled={!items?.length}
          onChange={(e) => onChange(e.target.value)}
          className="h-9 min-w-64 rounded-lg border bg-background px-2 text-sm"
        >
          {!items?.length && <option value="">{t("placeholder")}</option>}
          {items?.map((c) => <option key={c.id} value={c.id}>{displayName(c, locale)}</option>)}
        </select>
        {list.isPending && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
      </div>
      {list.isError && <p className="text-xs text-bad">{t("loadError")}</p>}
      {list.isSuccess && items?.length === 0 && (
        <p className="text-xs text-muted-foreground">
          {t("none")} <Link href="/clients" className="underline">{t("add")}</Link>
        </p>
      )}
    </div>
  );
}
