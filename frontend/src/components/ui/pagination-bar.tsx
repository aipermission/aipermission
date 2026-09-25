import { Button } from "./button";

type Props = {
  start: number;
  end: number;
  total: number;
  disabled?: boolean;
  onPrevious: () => void;
  onNext: () => void;
  hasPrevious: boolean;
  hasNext: boolean;
};

export function PaginationBar({ start, end, total, disabled, onPrevious, onNext, hasPrevious, hasNext }: Props) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-stone-200 bg-white px-4 py-3 text-sm text-stone-600">
      <span>
        Showing {start}-{end} of {total}
      </span>
      <div className="flex items-center gap-2">
        <Button type="button" variant="outline" onClick={onPrevious} disabled={disabled || !hasPrevious}>
          Previous
        </Button>
        <Button type="button" variant="outline" onClick={onNext} disabled={disabled || !hasNext}>
          Next
        </Button>
      </div>
    </div>
  );
}
