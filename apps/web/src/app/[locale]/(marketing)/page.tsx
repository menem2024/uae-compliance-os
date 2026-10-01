import { ArrowRight } from "lucide-react";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { auth } from "@/auth";
import { signInAction } from "@/app/actions/auth";
import { GeometricPattern } from "@/components/geometric-pattern";
import { LocaleSwitch } from "@/components/shell/locale-switch";
import { Logo } from "@/components/shell/logo";
import { ThemeToggle } from "@/components/shell/theme-toggle";
import { Link } from "@/i18n/navigation";
import { dashboardPath } from "@/lib/routes";

/**
 * Public landing (minimal until Task 20 builds the full marketing page). Never renders
 * AppShell: signed-out visitors get a sign-in CTA, signed-in visitors a link back in.
 */
export default async function MarketingPage({ params }: PageProps<"/[locale]">) {
  const { locale } = await params;
  setRequestLocale(locale);

  const session = await auth();
  const tHome = await getTranslations("Home");
  const tShell = await getTranslations("Shell");
  const tNav = await getTranslations("Shell.nav");

  return (
    <div className="relative flex min-h-dvh flex-col items-center justify-center gap-8 overflow-hidden bg-background p-6 text-foreground">
      <GeometricPattern />
      <div className="absolute inset-x-0 top-0 flex items-center justify-between gap-3 p-4 md:p-6">
        <Logo title={tHome("title")} latin={tShell("brandLatin")} />
        <div className="flex items-center gap-2">
          <ThemeToggle />
          <LocaleSwitch />
        </div>
      </div>

      <h1 className="relative text-3xl font-semibold tracking-[-0.5px] md:text-4xl">{tHome("title")}</h1>

      {session ? (
        <Link
          href="/dashboard"
          data-testid="open-workspace"
          className="group relative inline-flex h-11 items-center gap-2.5 rounded-lg bg-primary px-5 text-sm font-bold text-primary-foreground transition-[filter,transform] hover:brightness-110 active:translate-y-px"
        >
          {tNav("dashboard")}
          <ArrowRight
            className="size-4 transition-transform group-hover:translate-x-0.5 rtl:-scale-x-100 rtl:group-hover:-translate-x-0.5"
            strokeWidth={2}
            aria-hidden
          />
        </Link>
      ) : (
        <form action={signInAction} className="relative">
          <input type="hidden" name="locale" value={locale} />
          <input type="hidden" name="callbackUrl" value={dashboardPath(locale)} />
          <button
            type="submit"
            data-testid="sign-in"
            className="group inline-flex h-11 items-center gap-2.5 rounded-lg bg-primary px-5 text-sm font-bold text-primary-foreground transition-[filter,transform] hover:brightness-110 active:translate-y-px"
          >
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
  );
}
