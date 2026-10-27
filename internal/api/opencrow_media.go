package api

import (
	"context"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func (a *App) validateOpenCrowMotionAsset(asset, params Row) {
	if asset["type"] == "image" {
		if asset["mime"] != "image/png" && asset["mime"] != "image/jpeg" {
			fail(400, "OpenCrow 动作控制形象图仅支持 PNG/JPEG，未扣费")
		}
		file, err := os.Open(a.filePath(str(asset["file"])))
		if err != nil {
			fail(400, "无法读取动作控制形象图，未扣费")
		}
		defer file.Close()
		info, _, err := image.DecodeConfig(file)
		if err != nil || info.Width < 300 || info.Height < 300 || float64(info.Width)/float64(info.Height) > 2.5 || float64(info.Height)/float64(info.Width) > 2.5 {
			fail(400, "动作控制形象图宽高至少 300 像素，画幅比需在 1:2.5 至 2.5:1 之间，未扣费")
		}
		return
	}
	if asset["type"] != "video" {
		return
	}
	if asset["mime"] != "video/mp4" {
		fail(400, "OpenCrow 动作控制参考视频需使用 MP4 格式，未扣费")
	}
	info := a.openCrowMotionVideoInfo(asset)
	width, height, duration := integer(info["width"]), integer(info["height"]), integer(info["durationMillis"])
	if width < 340 || width > 3850 || height < 340 || height > 3850 {
		fail(400, "动作控制参考视频宽高须在 340–3850 像素之间，未扣费")
	}
	limit := int64(10000)
	if params["characterOrientation"] == "video" {
		limit = 30000
	}
	if duration < 3000 || duration > limit {
		fail(400, "动作参考视频时长不符合当前人物朝向的限制，未扣费")
	}
}

func (a *App) openCrowMotionVideoInfo(asset Row) Row {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cacheKey := a.Config.RedisPrefix + "opencrow-motion-media:" + str(asset["id"])
	if value, err := a.Redis.Get(ctx, cacheKey).Result(); err == nil {
		var info Row
		if json.Unmarshal([]byte(value), &info) == nil && integer(info["width"]) > 0 && integer(info["height"]) > 0 && integer(info["durationMillis"]) > 0 {
			return info
		}
	}
	path := env("FFPROBE_PATH", "")
	if path == "" {
		local, _ := filepath.Abs(".tools/ffmpeg/ffprobe.exe")
		if _, err := os.Stat(local); err == nil {
			path = local
		} else {
			path = "ffprobe"
		}
	}
	command := exec.CommandContext(ctx, path, "-v", "error", "-protocol_whitelist", "file,pipe", "-select_streams", "v:0", "-show_entries", "format=duration:stream=duration,width,height", "-of", "json", a.filePath(str(asset["file"])))
	hideCommandWindow(command)
	var output probeOutput
	command.Stdout = &output
	if command.Run() != nil {
		fail(400, "无法核实动作视频尺寸和时长，请检查文件及 ffprobe 配置，未扣费")
	}
	var metadata Row
	if json.Unmarshal(output.Bytes(), &metadata) != nil {
		fail(400, "动作视频元数据无效，未扣费")
	}
	streams := array(metadata["streams"])
	if len(streams) == 0 {
		fail(400, "动作参考文件没有有效视频流，未扣费")
	}
	stream := object(streams[0])
	seconds := num(stream["duration"])
	if seconds <= 0 {
		seconds = num(object(metadata["format"])["duration"])
	}
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > 3600 {
		fail(400, "动作视频时长无效，未扣费")
	}
	info := Row{"width": integer(stream["width"]), "height": integer(stream["height"]), "durationMillis": int64(math.Ceil(seconds * 1000))}
	_ = a.Redis.Set(ctx, cacheKey, jsonText(info), 24*time.Hour).Err()
	return info
}
