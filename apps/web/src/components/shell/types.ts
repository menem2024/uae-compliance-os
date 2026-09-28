/**
 * The Firm shown in the shell. Phase 0 (story 7a) passes a static demo Firm;
 * story 7b maps the `/v1/me` response onto this shape.
 */
export type ShellFirm = {
  name: string;
  /** `#RRGGBB` accent from Firm settings, or null for the platform default gold. */
  brandColor: string | null;
};

/** The signed-in user (7b). Absent until Auth.js is wired. */
export type ShellUser = {
  name: string;
};
