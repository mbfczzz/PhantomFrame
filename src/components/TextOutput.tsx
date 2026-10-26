import { useEffect, useState } from "react";
import { FileText, Copy } from "lucide-react";
import { copy, useToast } from "../lib";

export default function TextOutput({
  url,
  compact = false,
}: {
  url: string;
  compact?: boolean;
}) {
  const [text, setText] = useState("");
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const toast = useToast();
  useEffect(() => {
    if (compact) return;
    const abort = new AbortController();
    setText("");
    setError("");
    fetch(url, { credentials: "same-origin", signal: abort.signal })
      .then(async (response) => {
        if (!response.ok) throw new Error("文本加载失败，请重试");
        return response.text();
      })
      .then((value) => {
        if (!abort.signal.aborted) setText(value);
      })
      .catch((error) => {
        if (!abort.signal.aborted) setError(error.message);
      });
    return () => abort.abort();
  }, [url, compact, attempt]);
  if (compact)
    return (
      <div className="text-output-summary">
        <FileText size={32} />
        <span>文本结果 · 点击查看</span>
      </div>
    );
  return (
    <div className="text-output">
      {error ? (
        <>
          <p role="alert">{error}</p>
          <button className="button" onClick={() => setAttempt((x) => x + 1)}>
            重新加载文本
          </button>
        </>
      ) : (
        <>
          <div className="text-output-toolbar">
            <span>生成文本</span>
            <button
              type="button"
              className="button small"
              disabled={!text}
              onClick={() =>
                copy(text)
                  .then(() => toast("文本已复制"))
                  .catch(() => toast("复制失败", true))
              }
            >
              <Copy size={14} />
              复制文本
            </button>
          </div>
          <pre>{text || "正在加载文本…"}</pre>
        </>
      )}
    </div>
  );
}
