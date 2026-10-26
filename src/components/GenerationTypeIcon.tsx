import { Image, Video, FileText, Music, Maximize2 } from "lucide-react";

export default function GenerationTypeIcon({
  kind,
  size = 20,
}: {
  kind: string;
  size?: number;
}) {
  const icons = {
    image: Image,
    video: Video,
    text: FileText,
    audio: Music,
    upscale: Maximize2,
  };
  const Icon = icons[kind as keyof typeof icons] || FileText;
  return <Icon size={size} strokeWidth={1.5} />;
}
