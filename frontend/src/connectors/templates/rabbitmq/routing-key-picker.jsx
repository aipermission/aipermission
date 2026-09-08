import { useEffect, useMemo, useState } from "react";
import { Input } from "../../../components/ui/form";
import { uniqueQueueNames } from "./helpers";

export function RoutingKeyPicker({ queues, value, custom, onQueue, onCustom, styles, disabled = false }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const queueNames = useMemo(() => uniqueQueueNames(queues), [queues]);
  const selectedLabel = custom ? "Custom routing key" : value || "";
  const visibleQueues = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return (needle ? queueNames.filter((name) => name.toLowerCase().includes(needle)) : queueNames).slice(0, 80);
  }, [queueNames, query]);
  const options = useMemo(
    () => [
      { kind: "custom", label: "Custom routing key", help: "Type an exchange-specific routing key manually." },
      ...visibleQueues.map((name) => ({ kind: "queue", label: name, help: "Queue routing key via amq.default" })),
    ],
    [visibleQueues],
  );

  useEffect(() => {
    if (open) setActiveIndex((index) => Math.min(Math.max(index, 0), Math.max(options.length - 1, 0)));
  }, [open, options.length]);

  function choose(event, option) {
    event.preventDefault();
    if (!option) return;
    if (option.kind === "custom") onCustom();
    else onQueue(option.label);
    setOpen(false);
    setQuery("");
  }

  function handleKeyDown(event) {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      setOpen(true);
      const direction = event.key === "ArrowDown" ? 1 : -1;
      setActiveIndex((index) => (index + direction + Math.max(options.length, 1)) % Math.max(options.length, 1));
    } else if ((event.key === "Enter" || event.key === "Tab") && open) {
      choose(event, options[activeIndex]);
    } else if (event.key === "Escape") {
      event.preventDefault();
      setOpen(false);
      setQuery("");
    }
  }

  return (
    <div className="relative">
      <Input
        aria-activedescendant={open ? `rabbit-routing-option-${activeIndex}` : undefined}
        aria-controls="rabbit-routing-options"
        aria-expanded={open}
        aria-haspopup="listbox"
        role="combobox"
        className={styles.input}
        value={open ? query : selectedLabel}
        onFocus={() => {
          setOpen(true);
          setQuery("");
          setActiveIndex(0);
        }}
        onBlur={() => window.setTimeout(() => setOpen(false), 120)}
        onChange={(event) => {
          setQuery(event.target.value);
          setOpen(true);
          setActiveIndex(0);
        }}
        onKeyDown={handleKeyDown}
        placeholder="Search queue routing keys"
        aria-label="Search queue routing keys"
        disabled={disabled}
      />
      {open ? (
        <div
          id="rabbit-routing-options"
          className={`absolute left-0 right-0 top-[calc(100%+4px)] z-20 max-h-64 overflow-auto rounded-md border p-1 shadow-xl ${styles.border} ${styles.subtlePanel}`}
          role="listbox"
        >
          {options.map((option, index) => (
            <button
              key={`${option.kind}:${option.label}`}
              id={`rabbit-routing-option-${index}`}
              type="button"
              role="option"
              aria-selected={index === activeIndex}
              className={`grid w-full gap-0.5 rounded px-2 py-2 text-left text-sm ${index === activeIndex ? styles.activeRow : styles.rowHover}`}
              onMouseEnter={() => setActiveIndex(index)}
              onMouseDown={(event) => choose(event, option)}
            >
              <span className={`${option.kind === "queue" ? "truncate font-mono text-xs" : ""} font-semibold`}>{option.label}</span>
              <span className={`text-xs ${styles.muted}`}>{option.help}</span>
            </button>
          ))}
          {visibleQueues.length === 0 ? <div className={`px-2 py-3 text-xs ${styles.muted}`}>No queue matches this search.</div> : null}
        </div>
      ) : null}
    </div>
  );
}
