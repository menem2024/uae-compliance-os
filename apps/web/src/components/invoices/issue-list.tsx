"use client";

import { Check, Loader2, Pencil, Wand2 } from "lucide-react";
import { useLocale, useTranslations } from "next-intl";
import { useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { groupIssues, issueMessage } from "@/lib/p2/issues";
import { valueAtPath } from "@/lib/p2/corrections";
import type { Issue } from "@/lib/p2/types";
import { cn } from "@/lib/utils";

type Props = {
  issues: Issue[];
  payload: Record<string, unknown>;
  /** Corrections are allowed (status settled and not approved) and nothing else is in flight. */
  canCorrect: boolean;
  busy: boolean;
  /** Which path a correction is being saved for, to show its spinner. */
  savingPath: string | null;
  onCorrect: (path: string, value: string, rule: { ruleId: string; suggestion: boolean }) => void;
};

export function IssueList({ issues, payload, canCorrect, busy, savingPath, onCorrect }: Props) {
  const t = useTranslations("P2Invoices.issues");
  const { errors, warnings } = groupIssues(issues);

  if (issues.length === 0) {
    return (
      <div data-testid="issues-none" className="flex items-center gap-3 rounded-lg border border-ok/35 bg-ok-soft/70 p-4">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-pill bg-ok-soft text-ok">
          <Check className="size-5" strokeWidth={2.2} aria-hidden />
        </span>
        <span className="text-sm font-semibold text-ok">{t("none")}</span>
      </div>
    );
  }

  return (
    <ul data-testid="issues" className="flex flex-col gap-2.5">
      {[...errors, ...warnings].map((issue, i) => (
        <IssueRow
          key={`${issue.rule_id}-${issue.path}-${i}`}
          issue={issue}
          current={valueAtPath(payload, issue.path)}
          canCorrect={canCorrect}
          busy={busy}
          saving={savingPath === issue.path}
          onCorrect={onCorrect}
        />
      ))}
    </ul>
  );
}

function IssueRow({
  issue, current, canCorrect, busy, saving, onCorrect,
}: {
  issue: Issue;
  current: string;
  canCorrect: boolean;
  busy: boolean;
  saving: boolean;
  onCorrect: Props["onCorrect"];
}) {
  const t = useTranslations("P2Invoices.issues");
  const locale = useLocale();
  const inputId = useId();
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState(issue.suggested_value || current);
  const warning = issue.severity === "warning";
  const hasField = issue.path !== "";
  const hasSuggestion = issue.suggested_value !== "";

  return (
    <li
      data-testid="issue"
      data-rule-id={issue.rule_id}
      data-severity={issue.severity}
      className={cn(
        "flex flex-col gap-2.5 rounded-lg border p-3.5 animate-in fade-in slide-in-from-bottom-1 duration-300",
        warning ? "border-info/30 bg-info-soft/50" : "border-warn/35 bg-warn-soft/60",
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span
          dir="ltr"
          data-testid="issue-rule"
          className={cn("rounded-sm px-2 py-0.5 font-mono text-[11px] font-medium", warning ? "bg-info-soft text-info" : "bg-warn-soft text-warn")}
        >
          {issue.rule_id}
        </span>
        {issue.business_term && (
          <span
            dir="ltr"
            data-testid="issue-term"
            title={t("term")}
            className="rounded-pill border bg-panel px-2 py-0.5 font-mono text-[11px] text-muted-foreground"
          >
            {issue.business_term}
          </span>
        )}
        <span className={cn("text-xs font-semibold", warning ? "text-info" : "text-warn")}>
          {t(`severity.${warning ? "warning" : "error"}`)}
        </span>
      </div>

      <p data-testid="issue-message" className="text-sm leading-6 font-medium">{issueMessage(issue, locale)}</p>

      {hasField && (
        <p className="text-xs text-muted-foreground">
          {t("path")}:{" "}
          <span dir="ltr" data-testid="issue-path" className="font-mono text-foreground">{issue.path}</span>
        </p>
      )}

      {hasSuggestion && (
        <p className="text-xs text-muted-foreground">
          {t("suggested")}:{" "}
          <span dir="ltr" data-testid="issue-suggestion" className="font-mono text-foreground">{issue.suggested_value}</span>
        </p>
      )}

      {canCorrect && hasField && (
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            {hasSuggestion && (
              <Button
                type="button"
                size="sm"
                data-testid="apply-suggestion"
                disabled={busy}
                onClick={() => onCorrect(issue.path, issue.suggested_value, { ruleId: issue.rule_id, suggestion: true })}
              >
                {saving ? <Loader2 className="animate-spin" aria-hidden /> : <Wand2 aria-hidden />}
                {saving ? t("applying") : t("apply")}
              </Button>
            )}
            <Button
              type="button"
              size="sm"
              variant="outline"
              data-testid="issue-correct-toggle"
              aria-expanded={open}
              disabled={busy}
              onClick={() => setOpen((o) => !o)}
            >
              <Pencil aria-hidden />
              {t("correct")}
            </Button>
          </div>

          {open && (
            <form
              className="flex flex-col gap-1.5"
              onSubmit={(e) => {
                e.preventDefault();
                onCorrect(issue.path, value, { ruleId: issue.rule_id, suggestion: false });
              }}
            >
              <label htmlFor={inputId} className="text-xs text-muted-foreground">
                {t("correctLabel", { path: issue.path })}
                <span className="ms-2">
                  {t("current")}: <span dir="ltr" className="font-mono">{current === "" ? t("empty") : current}</span>
                </span>
              </label>
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  id={inputId}
                  data-testid="issue-correct-input"
                  dir="ltr"
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                  autoComplete="off"
                  spellCheck={false}
                  className="h-9 max-w-80 font-mono"
                />
                <Button type="submit" size="sm" data-testid="issue-correct-save" disabled={busy}>
                  {saving && <Loader2 className="animate-spin" aria-hidden />}
                  {saving ? t("saving") : t("save")}
                </Button>
                <Button type="button" size="sm" variant="ghost" onClick={() => setOpen(false)}>{t("cancel")}</Button>
              </div>
            </form>
          )}
        </div>
      )}

      {canCorrect && !hasField && <p className="text-xs text-muted-foreground">{t("noField")}</p>}
    </li>
  );
}
