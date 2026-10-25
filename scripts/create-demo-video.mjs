// Optional asset regeneration. The generated file is bundled; running the app does not require ffmpeg.
import { execFileSync } from "node:child_process";
const font = (
  process.env.DEMO_FONT ||
  (process.platform === "win32"
    ? "C:/Windows/Fonts/arial.ttf"
    : "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
)
  .replaceAll("\\", "/")
  .replace(":", "\\:");
const filters = [
  "drawgrid=width=48:height=48:thickness=1:color=0x284b40",
  `drawtext=fontfile='${font}':text='PHANTOMFRAME / DEMO PREVIEW':fontcolor=0xa6d5c3:fontsize=18:x=40:y=40`,
  `drawtext=fontfile='${font}':text='IMAGINATION IN MOTION':fontcolor=0xe3f7ed:fontsize=40:x='(w-tw)/2+30*sin(t*2)':y='(h-th)/2'`,
  `drawtext=fontfile='${font}':text='DEMO VIDEO - NOT AI GENERATED':fontcolor=0xa6b9c3:fontsize=17:x=40:y=470`,
];
execFileSync(
  process.env.FFMPEG_PATH || "ffmpeg",
  [
    "-hide_banner",
    "-loglevel",
    "error",
    "-y",
    "-f",
    "lavfi",
    "-i",
    "color=c=0x132b26:s=960x540:d=3:r=24",
    "-vf",
    filters.join(","),
    "-c:v",
    "libvpx",
    "-b:v",
    "500k",
    "-an",
    "server/demo-video.webm",
  ],
  { stdio: "inherit", windowsHide: true },
);
console.log("Created clearly labelled local demo video.");
