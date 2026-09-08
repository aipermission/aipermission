import { useEffect, useId, useRef, useState } from "react";
import { Button } from "./button";
import { cn } from "../../lib/utils";

export function ActionMenu({ trigger, items, itemKey = (item) => item, renderItem, onSelect, label, empty, panelClassName }) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef(null);
  const triggerRef = useRef(null);
  const menuRef = useRef(null);
  const initialFocusRef = useRef("first");
  const menuID = useId();

  useEffect(() => {
    if (!open) return undefined;
    const menuItems = menuRef.current?.querySelectorAll('[role="menuitem"]') || [];
    const initialItem = initialFocusRef.current === "last" ? menuItems[menuItems.length - 1] : menuItems[0];
    (initialItem || menuRef.current)?.focus();
    initialFocusRef.current = "first";
    function dismissOutside(event) {
      if (!rootRef.current?.contains(event.target)) setOpen(false);
    }
    document.addEventListener("pointerdown", dismissOutside);
    return () => document.removeEventListener("pointerdown", dismissOutside);
  }, [open]);

  function close({ restoreFocus = false } = {}) {
    setOpen(false);
    if (restoreFocus) triggerRef.current?.focus();
  }

  function handleMenuKeyDown(event) {
    if (event.key === "Escape") {
      event.preventDefault();
      close({ restoreFocus: true });
      return;
    }
    if (event.key === "Tab") {
      close();
      return;
    }
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    const menuItems = [...(menuRef.current?.querySelectorAll('[role="menuitem"]') || [])];
    if (menuItems.length === 0) return;
    const current = menuItems.indexOf(document.activeElement);
    const next =
      event.key === "Home"
        ? 0
        : event.key === "End"
          ? menuItems.length - 1
          : (current + (event.key === "ArrowDown" ? 1 : -1) + menuItems.length) % menuItems.length;
    menuItems[next].focus();
  }

  return (
    <div ref={rootRef} className="relative">
      <Button
        ref={triggerRef}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuID : undefined}
        onClick={() => {
          initialFocusRef.current = "first";
          setOpen((current) => !current);
        }}
        onKeyDown={(event) => {
          if (!open && ["ArrowDown", "ArrowUp"].includes(event.key)) {
            event.preventDefault();
            initialFocusRef.current = event.key === "ArrowUp" ? "last" : "first";
            setOpen(true);
          }
        }}
      >
        {trigger}
      </Button>
      {open ? (
        <div
          ref={menuRef}
          id={menuID}
          role="menu"
          aria-label={label}
          tabIndex={-1}
          className={cn(
            "absolute right-0 top-12 z-40 max-h-[70vh] w-80 overflow-y-auto rounded-lg border border-stone-200 bg-white p-2 shadow-xl dark-panel",
            panelClassName,
          )}
          onKeyDown={handleMenuKeyDown}
        >
          {items.map((item) => (
            <button
              type="button"
              role="menuitem"
              tabIndex={-1}
              key={itemKey(item)}
              className="grid w-full gap-2 rounded-md px-3 py-3 text-left transition hover:bg-stone-50 focus:bg-stone-50 focus:outline-none dark-panel-subtle"
              onClick={() => {
                close({ restoreFocus: true });
                onSelect(item);
              }}
            >
              {renderItem(item)}
            </button>
          ))}
          {items.length === 0 ? empty : null}
        </div>
      ) : null}
    </div>
  );
}
