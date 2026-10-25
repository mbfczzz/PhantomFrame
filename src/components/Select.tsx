import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
} from "react";
import { createPortal } from "react-dom";
import { Check, ChevronDown, Search, X } from "lucide-react";

export type SelectOption = {
  value: string;
  label: string;
  description?: string;
  meta?: string;
  group?: string;
  disabled?: boolean;
};

export function Select({
  label,
  value,
  options,
  onChange,
  placeholder = "请选择",
  searchable = false,
  disabled = false,
  className = "",
}: {
  label: string;
  value: string;
  options: SelectOption[];
  onChange: (value: string) => void;
  placeholder?: string;
  searchable?: boolean;
  disabled?: boolean;
  className?: string;
}) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const [position, setPosition] = useState({
    left: 0,
    top: 0,
    width: 0,
    maxHeight: 320,
  });
  const trigger = useRef<HTMLButtonElement>(null);
  const popup = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const typeahead = useRef({ text: "", at: 0 });
  const selected = options.find((option) => option.value === value);
  const filtered = options.filter((option) =>
    `${option.label} ${option.value} ${option.group || ""} ${option.description || ""}`
      .toLocaleLowerCase()
      .includes(query.trim().toLocaleLowerCase()),
  );
  const activeId = `${id}-option-${active}`;
  const close = (restoreFocus = false) => {
    setOpen(false);
    setQuery("");
    if (restoreFocus) trigger.current?.focus();
  };
  const show = () => {
    if (disabled) return;
    setQuery("");
    setActive(
      Math.max(
        0,
        options.findIndex(
          (option) => option.value === value && !option.disabled,
        ),
      ),
    );
    setOpen(true);
  };
  const choose = (option: SelectOption) => {
    if (option.disabled) return;
    onChange(option.value);
    close(true);
  };
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const rect = trigger.current?.getBoundingClientRect();
      if (!rect) return;
      const width = Math.min(
        Math.max(rect.width, searchable ? 380 : 150),
        window.innerWidth - 24,
      );
      const below = window.innerHeight - rect.bottom - 14;
      const above = rect.top - 14;
      const desired = searchable
        ? 380
        : Math.min(320, filtered.length * 38 + 12);
      const upwards = below < Math.min(desired, 180) && above > below;
      const height = Math.min(desired, Math.max(80, upwards ? above : below));
      setPosition({
        left: Math.max(12, Math.min(rect.left, window.innerWidth - width - 12)),
        top: upwards ? Math.max(12, rect.top - height - 6) : rect.bottom + 6,
        width,
        maxHeight: height,
      });
    };
    place();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [open, searchable, filtered.length]);
  useEffect(() => {
    if (!open) return;
    if (searchable) search.current?.focus();
    const outside = (event: Event) => {
      if (
        !popup.current?.contains(event.target as Node) &&
        !trigger.current?.contains(event.target as Node)
      )
        close();
    };
    document.addEventListener("pointerdown", outside, true);
    document.addEventListener("focusin", outside);
    return () => {
      document.removeEventListener("pointerdown", outside, true);
      document.removeEventListener("focusin", outside);
    };
  }, [open, searchable]);
  useEffect(() => {
    if (disabled) close();
  }, [disabled]);
  useEffect(() => {
    if (open)
      document.getElementById(activeId)?.scrollIntoView({ block: "nearest" });
  }, [active, open, activeId]);
  const keydown = (event: KeyboardEvent) => {
    if (event.key === "Escape" && open) {
      event.preventDefault();
      event.stopPropagation();
      close(true);
      return;
    }
    if (event.key === "Tab" && open) {
      close(true);
      return;
    }
    if (!open && ["ArrowDown", "ArrowUp", "Enter", " "].includes(event.key)) {
      event.preventDefault();
      show();
      return;
    }
    if (!open) return;
    if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
      event.preventDefault();
      const enabled = filtered
        .map((option, index) => (option.disabled ? -1 : index))
        .filter((index) => index >= 0);
      if (!enabled.length) return;
      if (event.key === "Home") setActive(enabled[0]);
      else if (event.key === "End") setActive(enabled[enabled.length - 1]);
      else {
        const step = event.key === "ArrowDown" ? 1 : -1;
        const current = enabled.indexOf(active);
        setActive(enabled[(current + step + enabled.length) % enabled.length]);
      }
    } else if (event.key === "Enter" || (event.key === " " && !searchable)) {
      event.preventDefault();
      if (filtered[active]) choose(filtered[active]);
    } else if (
      !searchable &&
      event.key.length === 1 &&
      !event.ctrlKey &&
      !event.metaKey
    ) {
      event.preventDefault();
      const now = Date.now();
      typeahead.current = {
        text:
          (now - typeahead.current.at > 700 ? "" : typeahead.current.text) +
          event.key.toLocaleLowerCase(),
        at: now,
      };
      const match = filtered.findIndex(
        (option) =>
          !option.disabled &&
          option.label.toLocaleLowerCase().startsWith(typeahead.current.text),
      );
      if (match >= 0) setActive(match);
    }
  };
  return (
    <div className={`ui-select ${className}`}>
      <button
        ref={trigger}
        type="button"
        role="combobox"
        aria-label={label}
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-controls={open ? `${id}-list` : undefined}
        aria-activedescendant={
          open && !searchable && filtered[active] ? activeId : undefined
        }
        disabled={disabled}
        className={`ui-select-trigger ${open ? "is-open" : ""}`}
        onClick={() => (open ? close() : show())}
        onKeyDown={keydown}
        title={selected?.label}
      >
        <span>{selected?.label || placeholder}</span>
        <ChevronDown size={14} />
      </button>
      {open &&
        createPortal(
          <div
            ref={popup}
            className={`ui-select-popup ${searchable ? "is-searchable" : ""}`}
            style={position}
            onKeyDown={keydown}
            data-select-popup
          >
            {searchable && (
              <div className="ui-select-search">
                <Search size={16} />
                <input
                  ref={search}
                  aria-label={`搜索${label}`}
                  aria-controls={`${id}-list`}
                  aria-activedescendant={
                    filtered[active] ? activeId : undefined
                  }
                  autoComplete="off"
                  placeholder="搜索名称或渠道"
                  value={query}
                  onChange={(event) => {
                    setQuery(event.target.value);
                    setActive(0);
                  }}
                />
                {query && (
                  <button
                    type="button"
                    aria-label="清空搜索"
                    onClick={() => {
                      setQuery("");
                      setActive(0);
                      search.current?.focus();
                    }}
                  >
                    <X size={14} />
                  </button>
                )}
              </div>
            )}
            <div
              id={`${id}-list`}
              role="listbox"
              aria-label={label}
              className="ui-select-list"
            >
              {filtered.map((option, index) => (
                <div key={option.value}>
                  {option.group &&
                    (index === 0 ||
                      filtered[index - 1].group !== option.group) && (
                      <div className="ui-select-group" role="presentation">
                        {option.group}
                      </div>
                    )}
                  <div
                    id={`${id}-option-${index}`}
                    role="option"
                    aria-label={option.label}
                    aria-selected={value === option.value}
                    aria-disabled={!!option.disabled}
                    data-value={option.value}
                    className={`ui-select-option ${index === active ? "is-active" : ""} ${value === option.value ? "is-selected" : ""} ${option.disabled ? "is-disabled" : ""}`}
                    onPointerMove={() => !option.disabled && setActive(index)}
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={() => choose(option)}
                  >
                    <span className="ui-select-option-copy">
                      <span className="ui-select-option-title">
                        {option.label}
                      </span>
                      {option.description && (
                        <small>{option.description}</small>
                      )}
                    </span>
                    {option.meta && (
                      <span className="ui-select-meta">{option.meta}</span>
                    )}
                    <span className="ui-select-check">
                      {value === option.value && <Check size={15} />}
                    </span>
                  </div>
                </div>
              ))}
              {!filtered.length && (
                <div className="ui-select-empty">没有匹配的选项</div>
              )}
            </div>
            {searchable && (
              <div className="ui-select-hint">
                ↑ ↓ 选择 <span>Enter 确认</span>
              </div>
            )}
          </div>,
          document.body,
        )}
    </div>
  );
}
