import { useTranslations } from "next-intl";
import { ApiError } from "@/lib/api-client";
import { errorMessageKey } from "@/lib/p2/display";

/** Understandable text for any error of the review flow (an `ApiError` code, or anything else). */
export function useErrorText(): (error: unknown) => string {
  const t = useTranslations("P2Invoices.errors");
  return (error) => t(errorMessageKey(error instanceof ApiError ? error.code : ""));
}
