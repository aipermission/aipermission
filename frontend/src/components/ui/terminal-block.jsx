import { cn } from "../../lib/utils";
import { forwardRef } from "react";

export const TerminalBlock = forwardRef(function TerminalBlock({ children, className, surface = "dark", ...props }, ref) {
  return (
    <pre
      ref={ref}
      className={cn(
        "terminal-text min-h-0 overflow-auto whitespace-pre-wrap break-words rounded-md p-4",
        surface === "log" ? "terminal-log-surface" : "terminal-output-surface",
        className
      )}
      {...props}
    >
      {children}
    </pre>
  );
});
