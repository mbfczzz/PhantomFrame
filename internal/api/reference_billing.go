package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func billingExtras(p Row) Row {
	e := mapField(p, "extras")
	if len(e) == 0 {
		return Row{}
	}
	for key := range e {
		if key != "imagePrice" && key != "includedImages" && key != "videoSecondPrice" && key != "videoOutputSecondPrice" && key != "videoRounding" {
			fail(400, "不支持的附加计费字段")
		}
	}
	result := Row{}
	for _, key := range []string{"imagePrice", "videoSecondPrice", "videoOutputSecondPrice"} {
		if e[key] != nil {
			result[key] = priceField(e, key)
		}
	}
	if e["includedImages"] != nil {
		v := numberField(e, "includedImages", 0, 1000000)
		if v != math.Trunc(v) {
			fail(400, "免费参考图片数量必须为整数")
		}
		result["includedImages"] = v
	}
	if e["videoRounding"] != nil {
		result["videoRounding"] = enumField(e, "videoRounding", "total", "each")
	}
	return result
}

type probeOutput struct{ bytes.Buffer }

func (b *probeOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 65536 {
		return 0, fmt.Errorf("media metadata too large")
	}
	return b.Buffer.Write(p)
}

func (a *App) referenceDurationMillis(asset Row) int64 {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cacheKey := a.Config.RedisPrefix + "media-duration:" + str(asset["id"])
	if value, e := a.Redis.Get(ctx, cacheKey).Int64(); e == nil && value > 0 {
		return value
	}
	path := env("FFPROBE_PATH", "")
	if path == "" {
		local, _ := filepath.Abs(".tools/ffmpeg/ffprobe.exe")
		if _, e := os.Stat(local); e == nil {
			path = local
		} else {
			path = "ffprobe"
		}
	}
	command := exec.CommandContext(ctx, path, "-v", "error", "-protocol_whitelist", "file,pipe", "-show_entries", "format=duration:stream=duration", "-of", "json", a.filePath(str(asset["file"])))
	hideCommandWindow(command)
	var output probeOutput
	command.Stdout = &output
	if e := command.Run(); e != nil {
		fail(400, "无法核实参考视频时长，请检查视频文件或服务端 ffprobe 配置，未扣费")
	}
	var metadata Row
	if json.Unmarshal(output.Bytes(), &metadata) != nil {
		fail(400, "参考视频时长信息无效，未扣费")
	}
	seconds := num(object(metadata["format"])["duration"])
	if seconds <= 0 {
		for _, stream := range array(metadata["streams"]) {
			seconds = math.Max(seconds, num(object(stream)["duration"]))
		}
	}
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > 3600 {
		fail(400, "无法核实参考视频有效时长，未扣费")
	}
	millis := int64(math.Ceil(seconds * 1000))
	_ = a.Redis.Set(ctx, cacheKey, millis, 30*24*time.Hour).Err()
	return millis
}
func (a *App) referenceCharges(s Store, uid string, defs []any, params, rate Row) (int64, []Row) {
	extras := object(rate["billing_extras"])
	if len(extras) == 0 {
		return 0, []Row{}
	}
	imageCount := int64(0)
	videoIDs := []any{}
	for _, raw := range defs {
		p := object(raw)
		if p["type"] != "assets" {
			continue
		}
		ids := array(params[str(p["name"])])
		switch first(p["itemType"], "image") {
		case "image":
			imageCount += int64(len(ids))
		case "video":
			videoIDs = append(videoIDs, ids...)
		}
	}
	amount := int64(0)
	items := []Row{}
	if len(videoIDs) > 0 && num(extras["videoOutputSecondPrice"]) > 0 {
		seconds := num(params["duration"])
		if seconds < 1 || seconds > 300 || seconds != math.Trunc(seconds) {
			fail(400, "带参考视频时需要明确的整数生成时长")
		}
		micros := int64(math.Round(num(extras["videoOutputSecondPrice"]) * 1000000))
		amount += int64(seconds) * micros
		items = append(items, Row{"label": "参考视频场景附加费（按生成时长）", "unit": "second", "unitPrice": num(extras["videoOutputSecondPrice"]), "quantity": int64(seconds)})
	}
	imageCount -= integer(extras["includedImages"])
	if imageCount > 0 && num(extras["imagePrice"]) > 0 {
		micros := int64(math.Round(num(extras["imagePrice"]) * 1000000))
		amount += micros * imageCount
		items = append(items, Row{"label": "额外参考图片", "unit": "image", "unitPrice": num(extras["imagePrice"]), "quantity": imageCount})
	}
	if len(videoIDs) > 0 && num(extras["videoSecondPrice"]) > 0 {
		millis, seconds := int64(0), int64(0)
		for _, assetID := range videoIDs {
			asset := s.One("SELECT id,file FROM assets WHERE id=? AND user_id=? AND type='video'", assetID, uid)
			if asset == nil {
				fail(400, "参考视频不可用")
			}
			duration := a.referenceDurationMillis(asset)
			millis += duration
			seconds += (duration + 999) / 1000
		}
		if extras["videoRounding"] != "each" {
			seconds = (millis + 999) / 1000
		}
		micros := int64(math.Round(num(extras["videoSecondPrice"]) * 1000000))
		amount += seconds * micros
		items = append(items, Row{"label": "参考视频时长", "unit": "second", "unitPrice": num(extras["videoSecondPrice"]), "quantity": seconds})
	}
	return amount, items
}
