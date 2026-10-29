import assert from "node:assert/strict";
import { test } from "node:test";
import { build } from "esbuild";

const result = await build({
  entryPoints: ["src/studio-modes.ts"],
  bundle: true,
  platform: "node",
  format: "esm",
  write: false,
});
const modes = await import(
  "data:text/javascript;base64," +
    Buffer.from(result.outputFiles[0].text).toString("base64")
);
const asset = (name, itemType, max, other = {}) => ({
  name,
  type: "assets",
  itemType,
  max,
  ...other,
});
const video = {
  type: "video",
  parameters: [
    {
      name: "function_mode",
      type: "select",
      options: ["omni_reference", "first_last_frames", "multi_frame"],
    },
    asset("imageUrls", "image", 9),
    asset("videoUrls", "video", 3),
    asset("audioUrls", "audio", 3),
  ],
};
assert.deepEqual(
  modes.studioModeOptions(video).map((o) => [o.label, o.disabled]),
  [
    ["文生视频", false],
    ["图生视频", false],
    ["全能参考", false],
  ],
);
const frameParams = modes.studioModeParams(
  video,
  { resolution: "720p" },
  "image",
);
assert.equal(frameParams.function_mode, "first_last_frames");
assert.equal(frameParams.resolution, "720p");
assert.deepEqual(
  modes
    .studioReferenceModel(video, "image", frameParams)
    .parameters.filter((p) => p.type === "assets")
    .map((p) => p.max),
  [2, 0, 0],
);
assert.deepEqual(
  modes
    .studioReferenceModel(video, "text", {})
    .parameters.filter((p) => p.type === "assets")
    .map((p) => p.max),
  [0, 0, 0],
);
assert.equal(
  modes.studioModeParams(video, frameParams, "text").function_mode,
  "omni_reference",
);
assert.equal(
  modes.studioModeParams(video, frameParams, "reference").function_mode,
  "omni_reference",
);
assert.notEqual(modes.studioModeError(video, "image", frameParams, []), "");
assert.equal(
  modes.studioModeError(video, "image", frameParams, [{ type: "image" }]),
  "",
);
assert.notEqual(modes.studioModeError(video, "reference", {}, []), "");
assert.equal(
  modes.initialStudioMode(video, {}, ["image", "image", "image"]),
  "reference",
);
const strictFrames = {
  type: "video",
  parameters: [
    { name: "mode", type: "select", options: ["frames", "references"] },
    asset("images", "image", 2),
  ],
};
assert.notEqual(
  modes.studioModeError(strictFrames, "image", { mode: "frames" }, [
    { type: "image" },
  ]),
  "",
);
assert.equal(
  modes.studioModeError(strictFrames, "image", { mode: "frames" }, [
    { type: "image" },
    { type: "image" },
  ]),
  "",
);
const image = {
  type: "image",
  parameters: [
    asset("images", "image", undefined),
    asset("extraImages", "image", undefined),
  ],
};
assert.deepEqual(
  modes.studioModeOptions(image).map((o) => o.label),
  ["文生图", "图生图"],
);
assert.ok(
  modes
    .studioReferenceModel(image, "image", {})
    .parameters.every((p) => p.max === Infinity),
);
assert.ok(
  modes
    .studioModeOptions({ type: "image", parameters: [] })
    .find((o) => o.value === "image").disabled,
);
assert.ok(
  modes
    .studioModeOptions({
      type: "video",
      parameters: [asset("videos", "video", 1, { required: true })],
    })
    .find((o) => o.value === "text").disabled,
);
const limited = {
  type: "video",
  parameters: [
    asset("videoUrls", "video", 3, { maxByResolution: { "720p": 0 } }),
  ],
};
assert.equal(
  modes.studioReferenceModel(limited, "reference", { resolution: "720p" })
    .parameters[0].max,
  0,
);
assert.deepEqual(
  modes.studioModeOptions({ type: "audio", parameters: [] }),
  [],
);
assert.equal(
  modes.studioModeParameter({
    type: "video",
    parameters: [
      { name: "mode", type: "select", options: ["quality", "speed"] },
    ],
  }),
  undefined,
);
const requiredImages = {
  ...video,
  parameters: [
    ...video.parameters.filter((p) => p.type !== "assets"),
    asset("imageUrls", "image", 9, { min: 3 }),
  ],
};
assert.equal(modes.initialStudioMode(requiredImages, {}), "reference");
assert.ok(
  modes.studioModeOptions(requiredImages).find((o) => o.value === "image")
    .disabled,
);
console.log(
  "PASS mode mapping, text isolation, frame requirements, optional references, unavailable capabilities, resolution limits and unrelated modes",
);

test("two images choose a reference mode that preserves both on non-frame video models", () => {
  const generic = { type: "video", parameters: [asset("images", "image", 9)] };
  assert.equal(
    modes.initialStudioMode(generic, {}, ["image", "image"]),
    "reference",
  );
});
test("multi-frame wire protocol does not expose audio or video upload slots", () => {
  const multi = {
    ...video,
    parameters: [
      { name: "function_mode", type: "select", options: ["multi_frame"] },
      ...video.parameters.filter((p) => p.type === "assets"),
    ],
  };
  const params = modes.studioModeParams(multi, {}, "reference");
  assert.deepEqual(
    modes
      .studioReferenceModel(multi, "reference", params)
      .parameters.filter((p) => p.type === "assets")
      .map((p) => p.max),
    [9, 0, 0],
  );
});
test("multi-frame without image capability is unavailable", () => {
  const multi = {
    type: "video",
    parameters: [
      { name: "function_mode", type: "select", options: ["multi_frame"] },
      asset("audio", "audio", 3),
    ],
  };
  assert.ok(modes.studioModeOptions(multi).every((o) => o.disabled));
});
test("one-image capability falls back from strict two-frame wire mode to references", () => {
  const single = {
    ...strictFrames,
    parameters: [strictFrames.parameters[0], asset("images", "image", 1)],
  };
  assert.equal(modes.studioModeParams(single, {}, "image").mode, "references");
});
test("strict two-frame mode cannot be selected when the total reference limit is one", () => {
  const single = {
    type: "video",
    parameters: [
      { name: "mode", type: "select", options: ["frames"] },
      asset("images", "image", 2, { maxTotal: 1 }),
    ],
  };
  assert.ok(
    modes.studioModeOptions(single).find((o) => o.value === "image").disabled,
  );
});
