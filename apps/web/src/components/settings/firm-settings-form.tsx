"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, RotateCcw } from "lucide-react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { useId, useState, type FormEvent } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { requestJson } from "@/lib/api-client";
import { DEFAULT_BRAND, brandVars, contrastRatio, type ThemeMode } from "@/lib/brand";
import { type FirmForm, type FirmSettings, firmForm, normalizeHex, toFirmPatch } from "@/lib/firm-settings";

const PANEL: Record<ThemeMode, string> = { light: "#FFFFFF", dark: "#12151A" };

/** /settings: Firm name and white-label accent with a live, contrast-adjusted preview (adr/016). */
export function FirmSettingsForm() {
  const t = useTranslations("P1Settings");
  const firm = useQuery({ queryKey: ["firm"], queryFn: ({ signal }) => requestJson<FirmSettings>("/api/firm", { signal }) });
  if (firm.isPending) return <Loader2 className="size-5 animate-spin text-muted-foreground" />;
  if (firm.isError) return <p className="text-sm text-bad">{t("errors.load")}</p>;
  // Keyed by updated_at: a saved (or concurrently changed) Firm re-initialises the form state.
  return <SettingsEditor key={firm.data.updated_at} firm={firm.data} />;
}

function SettingsEditor({ firm }: { firm: FirmSettings }) {
  const t = useTranslations("P1Settings");
  const router = useRouter();
  const qc = useQueryClient();
  const nameId = useId();
  const colorId = useId();
  const [form, setForm] = useState<FirmForm>(() => firmForm(firm));

  const save = useMutation({
    mutationFn: (patch: object) => requestJson<FirmSettings>("/api/firm", { method: "PATCH", json: patch }),
    onSuccess: (f) => {
      qc.setQueryData(["firm"], f);
      toast.success(t("saved"));
      router.refresh(); // the root layout re-reads /v1/me and applies the new accent
    },
    onError: () => toast.error(t("errors.generic")),
  });

  const hex = form.brandColor.trim() === "" ? null : normalizeHex(form.brandColor);
  const colorInvalid = form.brandColor.trim() !== "" && hex === null;
  const patch = toFirmPatch(firm, form);

  function submit(e: FormEvent) {
    e.preventDefault();
    if (patch && Object.keys(patch).length > 0) save.mutate(patch);
  }

  return (
    <form onSubmit={submit} className="grid max-w-3xl gap-6 md:grid-cols-[1fr_16rem]" noValidate>
      <div className="flex flex-col gap-4 rounded-xl border bg-panel p-5">
        <label htmlFor={nameId} className="flex flex-col gap-1 text-sm">
          {t("fields.name")}
          <Input id={nameId} value={form.name} maxLength={200} onChange={(e) => setForm({ ...form, name: e.target.value })}
            aria-invalid={form.name.trim() === ""} />
        </label>
        <div className="flex flex-col gap-1 text-sm">
          <label htmlFor={colorId}>{t("fields.brandColor")}</label>
          <div className="flex items-center gap-2">
            <input type="color" aria-label={t("fields.brandColorPicker")} value={hex ?? DEFAULT_BRAND}
              onChange={(e) => setForm({ ...form, brandColor: e.target.value.toUpperCase() })}
              className="size-8 cursor-pointer rounded border bg-transparent" />
            <Input id={colorId} data-testid="brand-color" dir="ltr" className="w-32 font-mono" value={form.brandColor}
              placeholder={DEFAULT_BRAND} maxLength={7} aria-invalid={colorInvalid}
              onChange={(e) => setForm({ ...form, brandColor: e.target.value })} />
            <Button type="button" variant="ghost" size="sm" onClick={() => setForm({ ...form, brandColor: "" })}>
              <RotateCcw /> {t("useDefault")}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">{colorInvalid ? t("errors.invalid_brand_color") : t("brandHint")}</p>
        </div>
        <Button type="submit" data-testid="settings-save" className="self-start"
          disabled={save.isPending || patch === null || Object.keys(patch).length === 0}>
          {save.isPending && <Loader2 className="animate-spin" />} {t("save")}
        </Button>
      </div>
      <BrandPreview hex={hex} />
    </form>
  );
}

function BrandPreview({ hex }: { hex: string | null }) {
  const t = useTranslations("P1Settings");
  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm font-medium">{t("preview")}</p>
      {(["light", "dark"] as const).map((mode) => {
        const v = brandVars(hex, mode);
        return (
          <div key={mode} className="flex flex-col gap-2 rounded-xl border p-4" style={{ background: PANEL[mode] }}>
            <span className="text-xs" style={{ color: mode === "light" ? "#475467" : "#98A2B3" }}>{t(`mode.${mode}`)}</span>
            <span className="text-sm font-semibold" style={{ color: v["--brand-ink"] }}>{t("sampleLink")}</span>
            <span className="inline-flex w-fit rounded-md px-3 py-1 text-sm font-medium"
              style={{ background: v["--brand"], color: v["--brand-foreground"] }}>
              {t("sampleButton")}
            </span>
            <span className="font-mono text-[11px]" dir="ltr" style={{ color: mode === "light" ? "#475467" : "#98A2B3" }}>
              {contrastRatio(v["--brand-ink"], PANEL[mode]).toFixed(1)}:1
            </span>
          </div>
        );
      })}
    </div>
  );
}
