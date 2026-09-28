import type { ReactNode } from "react";
import { PageTransition } from "@/components/motion";

/** Templates re-mount on navigation, so every route gets the fade-and-lift entrance. */
export default function Template({ children }: { children: ReactNode }) {
  return <PageTransition>{children}</PageTransition>;
}
