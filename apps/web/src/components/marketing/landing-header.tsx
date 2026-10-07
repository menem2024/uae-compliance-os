import { ArrowRight } from "lucide-react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import { signInAction } from "@/app/actions/auth";
import { LocaleSwitch } from "@/components/shell/locale-switch";
import { Logo } from "@/components/shell/logo";
import { ThemeToggle } from "@/components/shell/theme-toggle";
import { landingCta } from "@/lib/landing-cta";

const ctaClassName =
  "group inline-flex h-10 items-center gap-2 rounded-lg bg-primary px-4 text-sm font-bold text-primary-foreground " +
  "transition-[filter,transform] hover:brightness-110 active:translate-y-px";

/** Landing header: brand, theme/locale controls, and the one sign-in-or-open-workspace CTA. */
export function LandingHeader({ locale, signedIn }: { locale: string; signedIn: boolean }) {
  const tHome = useTranslations("Home");
  const tShell = useTranslations("Shell");
  const tNav = useTranslations("Shell.nav");
  const cta = landingCta(signedIn, locale);

  return (
    <header className="relative z-10 flex items-center justify-between gap-3 p-4 md:p-6">
      <Logo title={tHome("title")} latin={tShell("brandLatin")} />
      <div className="flex items-center gap-2">
        <ThemeToggle />
        <LocaleSwitch />
        {cta.testId === "open-workspace" ? (
          <Link href={cta.href} data-testid="open-workspace" className={ctaClassName}>
            {tNav("dashboard")}
            <ArrowRight
              className="size-4 transition-transform group-hover:translate-x-0.5 rtl:-scale-x-100 rtl:group-hover:-translate-x-0.5"
              strokeWidth={2}
              aria-hidden
            />
          </Link>
        ) : (
          <form action={signInAction}>
            <input type="hidden" name="locale" value={locale} />
            <input type="hidden" name="callbackUrl" value={cta.callbackUrl} />
            <button type="submit" data-testid="sign-in" className={ctaClassName}>
              {tHome("signIn")}
              <ArrowRight
                className="size-4 transition-transform group-hover:translate-x-0.5 rtl:-scale-x-100 rtl:group-hover:-translate-x-0.5"
                strokeWidth={2}
                aria-hidden
              />
            </button>
          </form>
        )}
      </div>
    </header>
  );
}
