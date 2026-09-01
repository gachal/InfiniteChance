# 01 厂商 API 适配矩阵

Type: research
Status: open
Blocked by:

## Question

候选厂商在**聊天、生图、生视频**三类能力上的 API 形态、鉴权方式、同步/异步模式、计费单位与价格档位分别是什么?产出一张适配矩阵,标注:哪些厂商三类齐全、哪些只有部分能力;生视频厂商异步任务模式的共性(提交/轮询/回调、任务生命周期状态);各家与 OpenAI 兼容风格的偏差点。这是网关 API 契约(03)与计费模型(04)的直接输入。

候选厂商清单(调研中可增删,标注理由):
- 聊天:OpenAI、Anthropic、Google Gemini、DeepSeek、阿里通义 Qwen、智谱 GLM、Moonshot Kimi、字节豆包
- 生图:OpenAI gpt-image、即梦、豆包 Seedream、通义万相、Flux(经聚合平台)
- 生视频:快手可灵 Kling、即梦、Vidu、通义万相、Google Veo、Runway、MiniMax 海螺

## Answer
