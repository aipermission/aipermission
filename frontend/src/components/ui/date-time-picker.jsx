import { CalendarClock, X } from "lucide-react";
import { Button } from "./button";
import { Input } from "./form";

export function DateTimePicker({ value, onChange, disabled = false }) {
  const [date, time] = splitLocalDateTime(value);

  function updateDate(nextDate) {
    onChange(nextDate ? `${nextDate}T${time || "23:59"}` : "");
  }

  function updateTime(nextTime) {
    if (!date) return;
    onChange(`${date}T${nextTime || "00:00"}`);
  }

  return (
    <div className="grid grid-cols-[minmax(0,1fr)_120px_40px] gap-2">
      <div className="relative">
        <CalendarClock className="pointer-events-none absolute left-3 top-3 h-4 w-4 text-stone-400" />
        <Input
          type="date"
          className="pl-9"
          value={date}
          disabled={disabled}
          onChange={(event) => updateDate(event.target.value)}
          aria-label="Expiry date"
        />
      </div>
      <Input
        type="time"
        value={time}
        disabled={disabled || !date}
        onChange={(event) => updateTime(event.target.value)}
        aria-label="Expiry time"
      />
      <Button
        type="button"
        variant="outline"
        className="h-10 w-10 px-0"
        title="Clear expiry"
        disabled={disabled || !value}
        onClick={() => onChange("")}
      >
        <X className="h-4 w-4" />
      </Button>
    </div>
  );
}

function splitLocalDateTime(value) {
  const normalized = String(value || "").trim();
  if (!normalized) return ["", ""];
  const [date = "", rawTime = ""] = normalized.split("T");
  return [date, rawTime.slice(0, 5)];
}
