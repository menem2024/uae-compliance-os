"use client";

import { UploadCloud } from "lucide-react";
import { useTranslations } from "next-intl";
import { useId, useRef, useState, type DragEvent } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const ACCEPT = "application/pdf,image/png,image/jpeg,image/webp,text/csv,.csv,.xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";

/** Drag-and-drop plus a file input. Validation and uploading belong to the pipeline, not here. */
export function Dropzone({ disabled, onFiles }: { disabled?: boolean; onFiles: (files: File[]) => void }) {
  const t = useTranslations("P1Documents.dropzone");
  const inputId = useId();
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);

  const take = (list: FileList | null) => {
    const files = list ? Array.from(list) : [];
    if (files.length > 0) onFiles(files);
  };
  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setOver(false);
    if (!disabled) take(e.dataTransfer.files);
  };

  return (
    <div
      data-testid="dropzone"
      onDragOver={(e) => {
        e.preventDefault();
        if (!disabled) setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={onDrop}
      className={cn(
        "flex flex-col items-center gap-2 rounded-xl border-2 border-dashed bg-panel p-8 text-center transition-colors",
        over && "border-primary bg-muted",
        disabled && "opacity-60",
      )}
    >
      <UploadCloud className="size-7 text-muted-foreground" aria-hidden />
      <p className="font-medium">{t("title")}</p>
      <p className="max-w-md text-xs text-muted-foreground">{t("hint")}</p>
      <input
        ref={input}
        id={inputId}
        type="file"
        multiple
        accept={ACCEPT}
        disabled={disabled}
        className="sr-only"
        aria-label={t("browse")}
        onChange={(e) => {
          take(e.target.files);
          e.target.value = "";
        }}
      />
      <Button type="button" variant="outline" disabled={disabled} onClick={() => input.current?.click()}>
        {t("browse")}
      </Button>
      {disabled && <p className="text-xs text-warn">{t("needClient")}</p>}
    </div>
  );
}
