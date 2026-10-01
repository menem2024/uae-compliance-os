export const dashboardPath = (locale: string): string => `/${locale}/dashboard`;

export const signInPath = (locale: string, returnTo: string): string =>
  `/${locale}/sign-in?callbackUrl=${encodeURIComponent(returnTo)}`;
