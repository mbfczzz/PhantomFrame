import { Link } from "react-router-dom";
export function Brand({
  name = "PhantomFrame",
  compact = false,
}: {
  name?: string;
  compact?: boolean;
}) {
  const defaultBrand = name === "PhantomFrame";
  return (
    <Link
      className={`brand ${compact ? "compact" : ""}`}
      to="/generate"
      aria-label={name + " 首页"}
    >
      <img src="/brand-mark.svg" alt="" width="34" height="34" />
      <span className="brand-copy">
        <strong>{defaultBrand ? "幻帧" : name}</strong>
        {!compact && (
          <small>{defaultBrand ? "PhantomFrame" : "图像与视频创作"}</small>
        )}
      </span>
    </Link>
  );
}
