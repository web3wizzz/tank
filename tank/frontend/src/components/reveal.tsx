"use client";

import { motion, useAnimationControls, useReducedMotion } from "framer-motion";
import { useEffect, type ReactNode } from "react";

export default function Reveal({ children, className = "", delay = 0 }: {
  children: ReactNode;
  className?: string;
  delay?: number;
}) {
  const controls = useAnimationControls();
  const reduceMotion = useReducedMotion();

  useEffect(() => {
    if (reduceMotion) return;
    controls.set({ opacity: 0.72, y: 10 });
    void controls.start({ opacity: 1, y: 0, transition: { duration: 0.45, delay, ease: "easeOut" } });
  }, [controls, delay, reduceMotion]);

  return (
    <motion.div className={className} initial={false} animate={controls}>
      {children}
    </motion.div>
  );
}
