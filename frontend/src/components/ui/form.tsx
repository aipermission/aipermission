import { cn } from "../../lib/utils";
import type {
  ComponentPropsWithRef,
  InputHTMLAttributes,
  LabelHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
  TextareaHTMLAttributes,
} from "react";

const fieldClassName = "grid gap-2 text-sm font-medium text-stone-800";

export function Field({ className, children, ...props }: LabelHTMLAttributes<HTMLLabelElement>) {
  return (
    <label className={cn(fieldClassName, className)} {...props}>
      {children}
    </label>
  );
}

export function FieldWithAction({
  label,
  action,
  htmlFor,
  children,
  className,
}: {
  label: ReactNode;
  action: ReactNode;
  htmlFor: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn(fieldClassName, className)}>
      <div className="flex items-center justify-between gap-2">
        <label htmlFor={htmlFor}>{label}</label>
        {action}
      </div>
      {children}
    </div>
  );
}

export function Input({ className, ...props }: ComponentPropsWithRef<"input">) {
  return (
    <input
      className={cn(
        "h-10 w-full rounded-md border border-stone-300 bg-white px-3 text-sm outline-none transition placeholder:text-stone-400 focus:border-emerald-800 focus:ring-2 focus:ring-emerald-900/10",
        className,
      )}
      {...props}
    />
  );
}

export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={cn(
        "h-10 w-full rounded-md border border-stone-300 bg-white px-3 text-sm outline-none transition focus:border-emerald-800 focus:ring-2 focus:ring-emerald-900/10",
        className,
      )}
      {...props}
    />
  );
}

export function Textarea({ className, ...props }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={cn(
        "min-h-24 w-full rounded-md border border-stone-300 bg-white px-3 py-2 text-sm outline-none transition placeholder:text-stone-400 focus:border-emerald-800 focus:ring-2 focus:ring-emerald-900/10",
        className,
      )}
      {...props}
    />
  );
}

export function Checkbox({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      type="checkbox"
      className={cn("mt-0.5 h-4 w-4 rounded border-stone-300 text-emerald-900 accent-emerald-900", className)}
      {...props}
    />
  );
}
