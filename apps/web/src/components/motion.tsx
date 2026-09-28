"use client";

import { motion } from "motion/react";
import type { ReactNode } from "react";

/**
 * Route content fades in and lifts 6px on mount (0.35s). Under
 * prefers-reduced-motion, MotionConfig reducedMotion="user" drops the lift.
 */
export function PageTransition({ children }: { children: ReactNode }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.35, ease: "easeOut" }}
      className="flex flex-col gap-6"
    >
      {children}
    </motion.div>
  );
}
