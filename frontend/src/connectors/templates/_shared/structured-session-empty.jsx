import { Button } from "../../../components/ui/button";

export function StructuredSessionEmpty({
  icon: Icon,
  title,
  description,
  buttonLabel,
  onStart,
  panelClass,
  mutedClass,
  footer,
  compact = false,
}) {
  return (
    <div className={`${footer ? "grid grid-rows-[minmax(0,1fr)_auto]" : "grid place-items-center"} h-full min-h-0 ${panelClass}`}>
      <div className="grid place-items-center p-6 text-center sm:p-8">
        <div className="grid max-w-lg gap-4">
          <Icon className={`mx-auto ${compact ? "h-8 w-8" : "h-10 w-10"} ${mutedClass}`} aria-hidden="true" />
          <div>
            <h3 className={compact ? "font-semibold" : "text-lg font-semibold"}>{title}</h3>
            <p className={`${compact ? "mt-1" : "mt-2"} text-sm ${mutedClass}`}>{description}</p>
          </div>
          <Button type="button" className="mx-auto" onClick={onStart}>
            {buttonLabel}
          </Button>
        </div>
      </div>
      {footer}
    </div>
  );
}
