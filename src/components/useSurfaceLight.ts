import { useCallback } from "react";

/** A local edge reflection: no render loop, idle timers, or touch tracking. */
export function useSurfaceLight() {
  return useCallback((element: HTMLElement | null) => {
    if (!element) return;
    const preference = window.matchMedia(
      "(hover: hover) and (pointer: fine) and (min-width: 768px) and (prefers-reduced-motion: no-preference)",
    );
    let frame = 0;
    let x = 0;
    let y = 0;
    const clear = () => {
      cancelAnimationFrame(frame);
      frame = 0;
      delete element.dataset.illuminated;
      element.style.removeProperty("--light-x");
      element.style.removeProperty("--light-y");
    };
    const move = (event: PointerEvent) => {
      if (event.pointerType !== "mouse") return;
      x = event.clientX;
      y = event.clientY;
      if (frame) return;
      frame = requestAnimationFrame(() => {
        const bounds = element.getBoundingClientRect();
        element.style.setProperty("--light-x", `${x - bounds.left}px`);
        element.style.setProperty("--light-y", `${y - bounds.top}px`);
        element.dataset.illuminated = "true";
        frame = 0;
      });
    };
    const detach = () => {
      element.removeEventListener("pointermove", move);
      element.removeEventListener("pointerleave", clear);
      window.removeEventListener("blur", clear);
      clear();
    };
    const sync = () => {
      detach();
      if (!preference.matches) return;
      element.addEventListener("pointermove", move, { passive: true });
      element.addEventListener("pointerleave", clear);
      window.addEventListener("blur", clear);
    };
    preference.addEventListener("change", sync);
    sync();
    return () => {
      preference.removeEventListener("change", sync);
      detach();
    };
  }, []);
}
