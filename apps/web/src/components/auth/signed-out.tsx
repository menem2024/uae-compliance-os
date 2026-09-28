import { ArrowRight, KeyRound, LockKeyhole, ScanLine, ShieldCheck, UserCheck } from "lucide-react";
import { getTranslations } from "next-intl/server";
import { signInAction } from "@/app/actions/auth";
import { GeometricPattern } from "@/components/geometric-pattern";
import { LocaleSwitch } from "@/components/shell/locale-switch";
import { Logo } from "@/components/shell/logo";
import { ThemeToggle } from "@/components/shell/theme-toggle";
import { dirOf } from "@/i18n/routing";

const PILLARS = [
  { key: "read", icon: ScanLine },
  { key: "validate", icon: ShieldCheck },
  { key: "human", icon: UserCheck },
] as const;

/**
 * Signed-out landing (prototype Login board). Credentials are entered on Zitadel's hosted
 * page, so the form is a single SSO action; the hero panel carries the product story.
 */
export async function SignedOut({ locale, callbackUrl }: { locale: string; callbackUrl?: string }) {
  const t = await getTranslations("Auth");
  const tHome = await getTranslations("Home");
  const tShell = await getTranslations("Shell");
  const rtl = dirOf(locale) === "rtl";

  return (
    <div className="flex min-h-dvh bg-background text-foreground">
      <section className="flex w-full shrink-0 flex-col gap-10 px-6 py-8 sm:px-12 md:py-14 lg:w-[560px] lg:px-[72px]">
        <div className="flex items-center justify-between gap-3">
          <Logo title={tHome("title")} latin={tShell("brandLatin")} />
          <ThemeToggle />
        </div>

        <div className="flex flex-col gap-2.5 lg:mt-[70px]">
          <h1 className="text-[28px] font-semibold tracking-[-0.5px] md:text-[34px]">{t("welcome")}</h1>
          <p className="text-[15px] leading-7 text-muted-foreground">{t("subtitle")}</p>
        </div>

        <form action={signInAction} className="flex flex-col gap-[18px]">
          <input type="hidden" name="locale" value={locale} />
          {callbackUrl && <input type="hidden" name="callbackUrl" value={callbackUrl} />}

          {callbackUrl && (
            <p
              role="status"
              className="flex items-center gap-2.5 rounded-lg border border-info/30 bg-info-soft px-3.5 py-3 text-[13px] text-foreground"
            >
              <LockKeyhole className="size-4 shrink-0 text-info" strokeWidth={1.8} aria-hidden />
              {t("continueNotice")}
            </p>
          )}

          <button
            type="submit"
            data-testid="sign-in"
            className="group mt-1.5 flex h-[50px] items-center justify-center gap-2.5 rounded-lg bg-primary text-[15px] font-bold text-primary-foreground transition-[filter,transform] hover:brightness-110 active:translate-y-px"
          >
            {tHome("signIn")}
            <ArrowRight
              className="size-4 transition-transform group-hover:translate-x-0.5 rtl:-scale-x-100 rtl:group-hover:-translate-x-0.5"
              strokeWidth={2.2}
              aria-hidden
            />
          </button>

          <div className="flex items-center gap-3 text-xs text-muted-foreground">
            <span className="h-px grow bg-border" aria-hidden />
            {t("or")}
            <span className="h-px grow bg-border" aria-hidden />
          </div>

          <div className="flex items-start gap-3 rounded-lg border px-4 py-3.5">
            <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-brand-soft text-brand-ink">
              <KeyRound className="size-4" strokeWidth={1.8} aria-hidden />
            </span>
            <span className="flex flex-col gap-1">
              <span className="text-[13px] leading-6">{t("sso")}</span>
              <span className="num text-[11px] tracking-[0.3px] text-muted-foreground" dir="ltr">
                {t("secured")}
              </span>
            </span>
          </div>
        </form>

        <div className="mt-auto flex items-center justify-between gap-3 text-xs text-muted-foreground">
          <span className="num">{t("copyright")}</span>
          <LocaleSwitch />
        </div>
      </section>

      <section className="auth-hero relative m-4 hidden grow flex-col justify-end overflow-hidden rounded-3xl border p-14 lg:flex">
        <GeometricPattern className="glow" focus={rtl ? "30% 30%" : "70% 30%"} />
        <div className="relative flex max-w-[620px] flex-col gap-7">
          <span className="inline-flex items-center gap-2 self-start rounded-pill border border-brand/35 bg-brand-soft px-3 py-1.5 text-[13px] text-brand-ink">
            <span className="size-1.5 rounded-pill bg-brand" aria-hidden />
            {t("badge")}
          </span>
          <h2 className="text-[40px] leading-[1.35] font-semibold tracking-[-0.8px] xl:text-[46px]">
            {t("heroLine1")}
            <br />
            {t("heroLine2")}
          </h2>
          <p className="text-[17px] leading-[1.8] text-muted-foreground">
            {t.rich("heroBody", {
              ltr: (chunks) => (
                <bdi dir="ltr" className="num whitespace-nowrap">
                  {chunks}
                </bdi>
              ),
            })}
          </p>
          <ul className="grid grid-cols-3 gap-3">
            {PILLARS.map(({ key, icon: Icon }) => (
              <li
                key={key}
                className="lift flex flex-col gap-2.5 rounded-xl border border-border/70 bg-panel/40 p-4 backdrop-blur-md"
              >
                <Icon className="size-5 text-brand" strokeWidth={1.8} aria-hidden />
                <span className="text-sm font-semibold">{t(`pillars.${key}.title`)}</span>
                <span className="text-xs leading-[1.6] text-muted-foreground">{t(`pillars.${key}.text`)}</span>
              </li>
            ))}
          </ul>
        </div>
      </section>
    </div>
  );
}
