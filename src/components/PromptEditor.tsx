import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from "react";
import { Music, Video } from "lucide-react";
import type { Asset } from "../lib";

export type ReferenceAsset = Asset & { referenceLabel: string };
export type PromptEditorHandle = {
  focus: () => void;
  insertReference: (asset: ReferenceAsset) => void;
};
export const PromptEditor = forwardRef<
  PromptEditorHandle,
  {
    value: string;
    onChange: (value: string) => void;
    references: ReferenceAsset[];
    supportsReferences: boolean;
    onFiles: (files: File[]) => Promise<ReferenceAsset[]>;
    onSubmit: () => void;
    canSubmit: boolean;
  }
>(
  (
    {
      value,
      onChange,
      references,
      supportsReferences,
      onFiles,
      onSubmit,
      canSubmit,
    },
    ref,
  ) => {
    const textarea = useRef<HTMLTextAreaElement>(null);
    const latest = useRef(value);
    latest.current = value;
    const [mention, setMention] = useState<{
      start: number;
      end: number;
      query: string;
    } | null>(null);
    const [active, setActive] = useState(0);
    const matching = mention
      ? references.filter((asset) =>
          `${asset.referenceLabel} ${asset.name}`
            .toLocaleLowerCase()
            .includes(mention.query.toLocaleLowerCase()),
        )
      : [];
    const insert = (text: string, range = mention) => {
      const el = textarea.current;
      const start = range?.start ?? el?.selectionStart ?? latest.current.length;
      const end = range?.end ?? el?.selectionEnd ?? start;
      const next =
        latest.current.slice(0, start) + text + latest.current.slice(end);
      if (next.length > 10000) return;
      onChange(next);
      setMention(null);
      requestAnimationFrame(() => {
        el?.focus();
        el?.setSelectionRange(start + text.length, start + text.length);
      });
    };
    useImperativeHandle(ref, () => ({
      focus: () => {
        textarea.current?.focus();
        textarea.current?.scrollIntoView({
          behavior: "smooth",
          block: "nearest",
        });
      },
      insertReference: (asset) => insert(`@${asset.referenceLabel} `, null),
    }));
    const updateMention = (text: string, caret: number) => {
      const match = text.slice(0, caret).match(/@([^\s@]{0,40})$/);
      setMention(
        match && references.length
          ? { start: caret - match[0].length, end: caret, query: match[1] }
          : null,
      );
      setActive(0);
    };
    useEffect(() => {
      if (!references.length) setMention(null);
    }, [references.length]);
    useEffect(() => {
      if (mention)
        document
          .getElementById(`reference-mention-${active}`)
          ?.scrollIntoView({ block: "nearest" });
    }, [active, mention]);
    return (
      <div className="surface-writing">
        <textarea
          ref={textarea}
          id="creation-prompt"
          aria-label="提示词"
          aria-controls={
            mention && matching.length ? "reference-mentions" : undefined
          }
          aria-autocomplete="list"
          aria-activedescendant={
            mention && matching[active]
              ? `reference-mention-${active}`
              : undefined
          }
          placeholder="描述你想要的画面、风格与细节…"
          value={value}
          maxLength={10000}
          onChange={(event) => {
            onChange(event.target.value);
            updateMention(event.target.value, event.target.selectionStart);
          }}
          onClick={(event) =>
            updateMention(
              event.currentTarget.value,
              event.currentTarget.selectionStart,
            )
          }
          onBlur={() => setMention(null)}
          onPaste={(event) => {
            if (event.clipboardData.files.length) {
              event.preventDefault();
              void onFiles(Array.from(event.clipboardData.files)).then(
                (added) => {
                  if (added.length)
                    insert(
                      added
                        .map((asset) => `@${asset.referenceLabel}`)
                        .join(" ") + " ",
                      null,
                    );
                },
              );
            }
          }}
          onKeyDown={(event) => {
            if (event.nativeEvent.isComposing) return;
            if (event.key === "Escape" && mention) {
              event.preventDefault();
              setMention(null);
              return;
            }
            if (
              mention &&
              matching.length &&
              ["ArrowDown", "ArrowUp", "Enter"].includes(event.key) &&
              !event.ctrlKey &&
              !event.metaKey
            ) {
              event.preventDefault();
              if (event.key === "Enter")
                insert(
                  `@${matching[active]?.referenceLabel || matching[0].referenceLabel} `,
                );
              else
                setActive(
                  (current) =>
                    (current +
                      (event.key === "ArrowDown" ? 1 : -1) +
                      matching.length) %
                    matching.length,
                );
              return;
            }
            if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
              event.preventDefault();
              if (canSubmit) onSubmit();
            }
          }}
        />
        {mention && matching.length > 0 && (
          <div
            className="prompt-mentions"
            id="reference-mentions"
            role="listbox"
            aria-label="引用参考素材"
          >
            <span className="prompt-mention-heading">选择要引用的素材</span>
            {matching.map((asset, index) => (
              <button
                key={asset.id}
                type="button"
                id={`reference-mention-${index}`}
                role="option"
                aria-selected={index === active}
                className={index === active ? "active" : ""}
                tabIndex={-1}
                onMouseDown={(event) => event.preventDefault()}
                onPointerMove={() => setActive(index)}
                onClick={() => insert(`@${asset.referenceLabel} `)}
              >
                {asset.type === "image" ? (
                  <img src={asset.url} alt="" />
                ) : asset.type === "video" ? (
                  <Video size={19} />
                ) : (
                  <Music size={19} />
                )}
                <span>
                  <strong>{asset.referenceLabel}</strong>
                  <small>{asset.name}</small>
                </span>
              </button>
            ))}
          </div>
        )}
        <div className="surface-tools">
          <span className="surface-input-hint">
            <span className="input-hint-desktop">
              {supportsReferences
                ? "粘贴图片，或用 @ 引用素材"
                : "Ctrl + Enter 生成"}
            </span>
            <span className="input-hint-mobile">
              {supportsReferences ? "可粘贴参考图" : "描述你的想法"}
            </span>
          </span>
          {value.length > 0 && (
            <span className="surface-count">{value.length} / 10000</span>
          )}
        </div>
      </div>
    );
  },
);
PromptEditor.displayName = "PromptEditor";
