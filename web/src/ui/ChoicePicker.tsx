import { useEffect, useRef, useId, useState } from "react";
import "./choice-picker.css";

export function ChoicePicker({
  label,
  value,
  onChange,
  items,
  disabled = false,
  className = "",
  placement = "bottom",
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  items: { value: string; label: string }[];
  disabled?: boolean;
  className?: string;
  placement?: "top" | "bottom";
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const options = useRef<(HTMLButtonElement | null)[]>([]);
  const id = useId();
  const values = items.map((item) => item.value);
  useEffect(() => {
    if (!open) return;
    if (disabled) {
      setOpen(false);
      return;
    }
    options.current[Math.max(0, values.indexOf(value))]?.focus();
    const close = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", close);
    return () => document.removeEventListener("pointerdown", close);
  }, [open, value, disabled]);
  return (
    <div
      className={`choice-picker ${className}`}
      data-placement={placement}
      ref={root}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <button
        ref={trigger}
        className="choice-picker-trigger"
        type="button"
        disabled={disabled}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => setOpen(!open)}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault();
            setOpen(true);
          }
        }}
      >
        {items.find((item) => item.value === value)?.label || value}
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          aria-hidden="true"
        >
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>
      {open && (
        <div
          id={id}
          className="choice-picker-menu"
          role="menu"
          aria-label={label}
        >
          {values.map((size, index) => (
            <button
              key={size}
              ref={(node) => {
                options.current[index] = node;
              }}
              type="button"
              role="menuitemradio"
              aria-checked={value === size}
              tabIndex={-1}
              onClick={() => {
                setOpen(false);
                trigger.current?.focus();
                onChange(size);
              }}
              onKeyDown={(event) => {
                let next = index;
                if (event.key === "ArrowDown")
                  next = (index + 1) % values.length;
                else if (event.key === "ArrowUp")
                  next = (index + values.length - 1) % values.length;
                else if (event.key === "Home") next = 0;
                else if (event.key === "End") next = values.length - 1;
                else if (event.key === "Escape") {
                  event.preventDefault();
                  setOpen(false);
                  trigger.current?.focus();
                  return;
                } else return;
                event.preventDefault();
                options.current[next]?.focus();
              }}
            >
              {items[index].label}
              <span aria-hidden="true">{value === size ? "✓" : ""}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
