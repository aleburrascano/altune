import { focusRing } from "./focusRing";

interface SearchBoxProps {
  label: string;
  value: string;
  onChange: (value: string) => void;
}

export function SearchBox({ label, value, onChange }: SearchBoxProps) {
  return (
    <input
      type="search"
      aria-label={label}
      placeholder={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={`w-56 border border-border bg-elev px-2 py-1 text-sm text-fg ${focusRing}`}
    />
  );
}

interface ToggleProps {
  label: string;
  options: string[];
  active: string[];
  onToggle: (option: string) => void;
}

export function ToggleChips({ label, options, active, onToggle }: ToggleProps) {
  return (
    <div role="group" aria-label={label} className="flex gap-1">
      {options.map((option) => (
        <button
          key={option}
          type="button"
          aria-pressed={active.includes(option)}
          onClick={() => onToggle(option)}
          className={`border border-border px-2 py-1 text-xs text-fg/50 aria-pressed:bg-elev aria-pressed:text-fg ${focusRing}`}
        >
          {option}
        </button>
      ))}
    </div>
  );
}
