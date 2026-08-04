import { forwardRef } from "react";
import { Slot } from "@radix-ui/react-slot";
import { cn } from "../../lib/utils";

const variants = {
  default: "bg-emerald-950 text-white hover:bg-emerald-900",
  outline: "border border-stone-300 bg-white text-stone-900 hover:bg-stone-100",
  ghost: "text-stone-700 hover:bg-stone-100",
  danger: "bg-red-700 text-white hover:bg-red-800",
};

export const Button = forwardRef(function Button({ className, variant = "default", asChild = false, ...props }, ref) {
  const Comp = asChild ? Slot : "button";
  return (
    <Comp
      ref={ref}
      className={cn(
        "inline-flex h-10 items-center justify-center gap-2 rounded-md px-4 text-sm font-semibold transition disabled:pointer-events-none disabled:opacity-50",
        variants[variant],
        className,
      )}
      {...props}
    />
  );
});
