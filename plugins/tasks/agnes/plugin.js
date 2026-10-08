const MODELS = ["agnes-video-2.5"];
const SIZES = ["720P", "1080P", "1K", "2K"];
const MIN_SECONDS = 4;
const MAX_SECONDS = 12;
const MAX_IMAGES = 8;
const MAX_VIDEOS = 1;
const MAX_REFERENCES = 12;

const VIDEO_SECONDS_FIELD = {
  type: "number",
  unit: "second",
  description: { en: "Video generation unit price", zh: "视频生成单价" },
};

const INPUT_IMAGES_FIELD = {
  type: "number",
  unit: "count",
  description: { en: "Input image unit price", zh: "输入图片单价" },
};

export const meta = {
  apiVersion: 1,
  key: "agnes",
  name: "Agnes Video",
  icon: "text:AG",
  description: {
    en: "Agnes Video 2.5 generation through the OpenAI Videos-compatible API",
    zh: "通过 OpenAI Videos 兼容 API 使用 Agnes Video 2.5 生成视频",
  },
  version: "1.0.0",
  author: { name: "QuantumNous" },
  models: MODELS,
  fetchMode: "per_task",
  baseUrl: "https://apihub.agnes-ai.com/v1",
  website: "https://www.agnes-ai.com",
  upstreams: ["vendor", "new_api"],
  usageSchema: {
    seconds: VIDEO_SECONDS_FIELD,
    size: {
      enum: SIZES,
      description: { en: "Output video resolution", zh: "输出视频分辨率" },
    },
    input_images: INPUT_IMAGES_FIELD,
  },
  usageExamples: [
    { label: "720P 5s", facts: { seconds: 5, size: "720P", input_images: 0 } },
    { label: "2K 8s", facts: { seconds: 8, size: "2K", input_images: 0 } },
    { label: "720P 5s · 7 images", facts: { seconds: 5, size: "720P", input_images: 7 } },
  ],
  routes: [
    { method: "POST", path: "/agnes/v1/videos", type: "submit", decode: "createVideoTask", render: "taskCreated" },
    { method: "GET", path: "/agnes/agnesapi/:video_id", type: "query", taskIdParam: "video_id", render: "taskStatus" },
  ],
  protocols: [{ name: "openai_responses", supports: ["stream", "sync", "background"] }, "openai_video"],
};

function trimmed(value) {
  return String(value || "").trim();
}

function authHeaders(ctx) {
  return {
    Authorization: ctx.authHeader || "Bearer " + ctx.apiKey,
    "Content-Type": "application/json",
    Accept: "application/json",
  };
}

function appendPath(baseUrl, path) {
  return String(baseUrl || "").replace(/\/+$/, "") + path;
}

function queryBaseUrl(baseUrl) {
  return String(baseUrl || "")
    .replace(/\/+$/, "")
    .replace(/\/v1$/i, "");
}

function viaNewAPI(ctx) {
  return !!(ctx.upstream && ctx.upstream.kind === "new_api");
}

function newAPIPath(ctx, path) {
  return appendPath(queryBaseUrl(ctx.baseUrl), "/agnes" + path);
}

function finiteNumber(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}

function normalizeSeconds(value) {
  const seconds = finiteNumber(value === undefined || value === null || value === "" ? 5 : value);
  if (seconds === null || seconds < MIN_SECONDS || seconds > MAX_SECONDS) throw new Error("seconds must be between 4 and 12");
  return seconds;
}

function normalizeSize(value) {
  const size = trimmed(value || "720P");
  if (!SIZES.includes(size)) throw new Error("size must be one of 720P, 1080P, 1K, 2K");
  return size;
}

function stringArray(value, field, maximum) {
  if (value === undefined) return [];
  if (!Array.isArray(value)) throw new Error(field + " must be an array");
  const values = value.map(trimmed).filter(Boolean);
  if (values.length > maximum) throw new Error(field + " must contain at most " + maximum + " items");
  return values;
}

function inputImageCount(req) {
  let count = 0;
  if (trimmed(req.first_frame)) count += 1;
  if (trimmed(req.last_frame)) count += 1;
  if (Array.isArray(req.images)) count += req.images.map(trimmed).filter(Boolean).length;
  return count;
}

function videoArray(value) {
  if (value === undefined) return [];
  if (!Array.isArray(value)) throw new Error("videos must be an array");
  if (value.length > MAX_VIDEOS) throw new Error("videos must contain at most 1 item");
  return value.map(function (item) {
    const url = typeof item === "string" ? trimmed(item) : trimmed(item && item.url);
    if (!url) throw new Error("each video must be a URL string or an object with a url");
    if (typeof item === "string") return { url: url };
    const video = { url: url };
    ["start_seconds", "require_audio"].forEach(function (key) {
      if (item[key] !== undefined && item[key] !== null) video[key] = item[key];
    });
    return video;
  });
}

function validateMedia(req) {
  const images = stringArray(req.images, "images", MAX_IMAGES);
  const audios = stringArray(req.audios, "audios", 3);
  const videos = videoArray(req.videos);
  const imageCount = inputImageCount(Object.assign({}, req, { images: images }));
  if (imageCount > MAX_IMAGES) throw new Error("input images must contain at most 8 items");
  if (imageCount + audios.length + videos.length > MAX_REFERENCES) throw new Error("reference media must contain at most 12 items");
  return { images: images, audios: audios, videos: videos };
}

function inferMode(req) {
  if (trimmed(req.mode)) return trimmed(req.mode);
  if (trimmed(req.first_frame) || trimmed(req.last_frame)) return "keyframe";
  if ((Array.isArray(req.images) && req.images.length) || (Array.isArray(req.audios) && req.audios.length) || (Array.isArray(req.videos) && req.videos.length))
    return "reference";
  return "text";
}

function validateMode(req, media) {
  const mode = inferMode(req);
  if (!["text", "keyframe", "reference"].includes(mode)) throw new Error("mode must be text, keyframe, or reference");
  if (mode === "text" && (trimmed(req.first_frame) || trimmed(req.last_frame) || media.images.length || media.audios.length || media.videos.length))
    throw new Error("text mode does not accept reference media");
  if (mode === "keyframe" && !trimmed(req.first_frame) && !trimmed(req.last_frame)) throw new Error("keyframe mode requires first_frame or last_frame");
  if (mode === "keyframe" && (media.images.length || media.audios.length || media.videos.length))
    throw new Error("keyframe mode does not accept reference media");
  if (mode === "reference" && !media.images.length && !media.audios.length && !media.videos.length)
    throw new Error("reference mode requires images, audios, or videos");
  return mode;
}

function normalizeRequest(req, model) {
  const input = Object.assign({}, req || {});
  const prompt = trimmed(input.prompt);
  if (!prompt) throw new Error("field prompt is required");
  const seconds = normalizeSeconds(input.seconds === undefined ? input.duration : input.seconds);
  const size = normalizeSize(input.size);
  if (input.n !== undefined && Number(input.n) !== 1) throw new Error("n must be 1");

  if (!trimmed(input.first_frame) && !trimmed(input.last_frame)) {
    const legacyImage = trimmed(input.input_reference || input.image);
    if (legacyImage) input.first_frame = legacyImage;
  }
  const media = validateMedia(input);
  const mode = validateMode(input, media);
  const body = {
    model: model,
    prompt: prompt,
    seconds: String(seconds),
    mode: mode,
    size: size,
  };
  ["aspect_ratio", "seed", "first_frame", "last_frame"].forEach(function (key) {
    if (input[key] !== undefined && input[key] !== null && input[key] !== "") body[key] = input[key];
  });
  if (media.images.length) body.images = media.images;
  if (media.audios.length) body.audios = media.audios;
  if (media.videos.length) body.videos = media.videos;
  return body;
}

function responsesInput(req) {
  const texts = [];
  const images = [];
  const input = req.input;
  if (typeof input === "string") texts.push(input);
  else if (Array.isArray(input)) {
    for (const item of input) {
      if (typeof item === "string") {
        texts.push(item);
        continue;
      }
      if (!item || typeof item !== "object" || Array.isArray(item)) continue;
      const content = item.content === undefined ? [item] : Array.isArray(item.content) ? item.content : [item.content];
      for (const part of content) {
        if (typeof part === "string") {
          texts.push(part);
          continue;
        }
        if (!part || typeof part !== "object" || Array.isArray(part)) continue;
        if (["input_text", "text"].includes(part.type) && typeof part.text === "string") texts.push(part.text);
        if (["input_image", "image_url"].includes(part.type)) {
          let image = part.image_url;
          if (image && typeof image === "object") image = image.url;
          if (trimmed(image)) images.push(trimmed(image));
        }
      }
    }
  }
  return {
    prompt: texts
      .filter(function (text) {
        return trimmed(text);
      })
      .join("\n"),
    images: images,
  };
}

function responsesVideoText(ctx) {
  const artifact = ctx && ctx.artifacts && ctx.artifacts.video;
  const url = trimmed(artifact && artifact.url);
  if (!url) throw new Error("video artifact is unavailable");
  const escaped = url.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  return '<video controls src="' + escaped + '"></video>';
}

export function buildSubmitRequest(ctx) {
  const body = normalizeRequest(ctx.requestBody || {}, ctx.upstreamModel || ctx.model);
  const url = viaNewAPI(ctx) ? newAPIPath(ctx, "/v1/videos") : appendPath(ctx.baseUrl, "/videos");
  return { url: url, method: "POST", headers: authHeaders(ctx), body: body };
}

export function parseSubmitResponse(_ctx, resp) {
  const body = resp.body || {};
  if (body.error && body.error.message) throw new Error(body.error.message);
  const taskId = body.id || body.task_id || body.video_id;
  if (!taskId) throw new Error("task_id is empty");
  const videoId = body.video_id || body.videoId || taskId;
  return { taskId: taskId, taskData: body, state: { video_id: videoId } };
}

export function extractUsage(ctx) {
  const req = ctx.requestBody || {};
  return {
    seconds: normalizeSeconds(req.seconds === undefined ? req.duration : req.seconds),
    size: normalizeSize(req.size),
    input_images: inputImageCount(req),
  };
}

export function extractUsageOnComplete(_task, _taskResult, body) {
  const facts = {};
  const seconds = finiteNumber((body || {}).seconds);
  if (seconds !== null && seconds >= MIN_SECONDS && seconds <= MAX_SECONDS) facts.seconds = seconds;
  const size = trimmed((body || {}).size);
  if (SIZES.includes(size)) facts.size = size;
  return Object.keys(facts).length ? facts : null;
}

export function buildQueryRequest(ctx) {
  const state = ctx.state && typeof ctx.state === "object" ? ctx.state : {};
  const data = ctx.data && typeof ctx.data === "object" ? ctx.data : {};
  const videoId = state.video_id || data.video_id || data.videoId || ctx.taskId;
  if (!videoId) throw new Error("video_id is empty");
  const model = ctx.upstreamModel || ctx.model || MODELS[0];
  const query = "?video_id=" + encodeURIComponent(videoId) + "&model_name=" + encodeURIComponent(model);
  const url = viaNewAPI(ctx)
    ? newAPIPath(ctx, "/agnesapi/" + encodeURIComponent(videoId)) + "?model_name=" + encodeURIComponent(model)
    : appendPath(queryBaseUrl(ctx.baseUrl), "/agnesapi") + query;
  return { url: url, method: "GET", headers: { Authorization: ctx.authHeader || "Bearer " + ctx.apiKey, Accept: "application/json" } };
}

export function parseTaskResult(_ctx, body) {
  const error = body && body.error;
  const rawStatus = trimmed(body && body.status).toLowerCase();
  const statuses = { queued: "QUEUED", in_progress: "IN_PROGRESS", processing: "IN_PROGRESS", completed: "SUCCESS", failed: "FAILURE" };
  const status = statuses[rawStatus];
  if (!status) return { status: "UNKNOWN", reason: "unrecognized status: " + String((body || {}).status || "") };
  const progressValue = finiteNumber(body.progress);
  const result = { status: status };
  if (progressValue !== null && progressValue >= 0 && progressValue <= 100) result.progress = progressValue + "%";
  if (status === "FAILURE") result.reason = trimmed(error && error.message) || "task failed";
  return result;
}

export function listArtifacts(task) {
  const data = task && task.data && typeof task.data === "object" ? task.data : {};
  const result = data.metadata && typeof data.metadata === "object" ? data.metadata : {};
  return task.status === "SUCCESS" && trimmed(data.url || result.url) ? [{ key: "video", type: "video", mimeType: "video/mp4" }] : [];
}

export function buildContentRequest(ctx) {
  if (ctx.artifactKey !== "video") throw new Error("artifact_not_found");
  const data = ctx.data && typeof ctx.data === "object" ? ctx.data : {};
  const result = data.metadata && typeof data.metadata === "object" ? data.metadata : {};
  const url = trimmed(data.url || result.url);
  if (!url) throw new Error("artifact_not_found");
  return { url: url, method: ctx.clientRequest.method, credentialless: true };
}

export const native = {
  createVideoTask: function (ctx) {
    if (!ctx.body || ctx.body.kind !== "json" || !ctx.body.value || typeof ctx.body.value !== "object" || Array.isArray(ctx.body.value))
      throw new Error("JSON object required");
    const req = ctx.body.value;
    const model = trimmed(req.model);
    if (!MODELS.includes(model)) throw new Error("unsupported model: " + model);
    const requestBody = normalizeRequest(req, model);
    return {
      kind: "submit",
      model: model,
      action: requestBody.mode === "text" ? "text_to_video" : requestBody.mode === "keyframe" ? "keyframe_to_video" : "reference_to_video",
      requestBody: requestBody,
    };
  },
  taskCreated: function (ctx, task) {
    const data = task.data && typeof task.data === "object" && !Array.isArray(task.data) ? task.data : {};
    return Object.assign({}, data, { id: task.task_id, task_id: task.task_id, video_id: task.task_id });
  },
  taskStatus: function (ctx, task) {
    const data = task.data && typeof task.data === "object" && !Array.isArray(task.data) ? task.data : {};
    return Object.assign({}, data, { id: task.task_id, task_id: task.task_id, video_id: task.task_id });
  },
  error: function (ctx, error) {
    return { code: error.code, message: error.message };
  },
};

export const protocols = {
  openai_responses: {
    decodeRequest: function (ctx) {
      if (!ctx.body || ctx.body.kind !== "json") throw new Error("JSON body required");
      const req = ctx.body.value;
      if (!req || typeof req !== "object" || Array.isArray(req)) throw new Error("request body must be an object");
      if (req.input !== undefined && typeof req.input !== "string" && !Array.isArray(req.input)) throw new Error("input must be a string or array");
      const input = responsesInput(req);
      const prompt = input.prompt || trimmed(req.prompt);
      if (!prompt) throw new Error("input is required");
      const requestBody = Object.assign({}, req, { model: ctx.model, prompt: prompt });
      delete requestBody.input;
      if (input.images.length) requestBody.images = (Array.isArray(req.images) ? req.images : []).concat(input.images);
      const normalized = normalizeRequest(requestBody, ctx.model);
      return {
        kind: "submit",
        model: ctx.model,
        action: normalized.mode === "text" ? "text_to_video" : normalized.mode === "keyframe" ? "keyframe_to_video" : "reference_to_video",
        requestBody: normalized,
      };
    },
    renderEvents: function (ctx, task, previousState) {
      const status = String(task.status || "UNKNOWN").toUpperCase();
      const value = Number(String(task.progress || "").replace("%", ""));
      const progress = Number.isFinite(value) && value >= 0 && value <= 100 ? value : null;
      const state = { status: status, progress: progress };
      if (status === "SUCCESS") {
        const text = responsesVideoText(ctx);
        const events = previousState && previousState.status === status ? [] : [{ type: "output", data: text }];
        return { events: events, state: state, done: true };
      }
      if (status === "FAILURE")
        return { events: [{ type: "error", code: "task_failed", message: task.fail_reason || "task failed" }], state: state, done: true };
      if (previousState && previousState.status === status && previousState.progress === progress) return { events: [], state: state, done: false };
      const event = { type: "progress", message: status.toLowerCase() };
      if (progress !== null) event.progress = progress;
      return { events: [event], state: state, done: false };
    },
    renderFinal: function (ctx, _task) {
      return {
        output: [
          {
            type: "message",
            status: "completed",
            role: "assistant",
            content: [{ type: "output_text", text: responsesVideoText(ctx), annotations: [], logprobs: [] }],
          },
        ],
        metadata: { vendor: "agnes" },
      };
    },
  },
  openai_video: {
    decodeRequest: function (ctx) {
      if (!ctx.body || ctx.body.kind !== "json") throw new Error("JSON body required");
      const req = ctx.body.value;
      if (!req || typeof req !== "object" || Array.isArray(req)) throw new Error("JSON object required");
      const normalized = normalizeRequest(Object.assign({}, req, { model: ctx.model }), ctx.model);
      return {
        kind: "submit",
        model: ctx.model,
        action: normalized.mode === "text" ? "text_to_video" : normalized.mode === "keyframe" ? "keyframe_to_video" : "reference_to_video",
        requestBody: normalized,
      };
    },
    render: function (ctx, task) {
      if (task.data && typeof task.data === "object" && !Array.isArray(task.data)) return task.data;
      return {
        id: task.task_id,
        object: "video",
        model: ctx.model,
        status: task.status === "SUCCESS" ? "completed" : task.status === "FAILURE" ? "failed" : "in_progress",
      };
    },
  },
};
