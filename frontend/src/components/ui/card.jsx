import { cn } from "../../lib/utils";

export function Card({ className, ...props }) {
  return <section className={cn("rounded-lg border border-stone-200 bg-white shadow-sm", className)} {...props} />;
}

export function CardHeader({ className, ...props }) {
  return <div className={cn("border-b border-stone-100 p-5", className)} {...props} />;
}

export function CardTitle({ className, children, ...props }) {
  return (
    <h2 className={cn("text-lg font-semibold text-stone-950", className)} {...props}>
      {children}
    </h2>
  );
}

export function CardDescription({ className, ...props }) {
  return <p className={cn("mt-1 text-sm text-stone-500", className)} {...props} />;
}

export function CardContent({ className, ...props }) {
  return <div className={cn("p-5", className)} {...props} />;
}
