# ACE-Step 1.5 acestep-api 官方 REST 契约(2026-11-05 钉定,来源 docs/en/API.md @ ace-step/ACE-Step-1.5)

80 号票 adaptor 直连的地基事实。`[官方]` = 厂商文档原文。

## 端点

| 端点 | 方法 | 说明 |
| --- | --- | --- |
| `/release_task` | POST | 提交任务(JSON / multipart/form-data / urlencoded 皆收) |
| `/query_result` | POST | 批量查询;body `task_id_list`(JSON 数组或数组) |
| `/v1/audio?path=…` | GET | 下载产物;`file` 字段给的是**相对路径**,须拼 BaseURL |
| `/v1/models` | GET | 列已加载 DiT 模型 |
| `/health` | GET | 健康检查 → **连通测试用这个** |

BaseURL = `http://<win-ip>:8001`(部署文档钉死端口)。

## 鉴权

- 可选;服务端 `ACESTEP_API_KEY` 空 = 鉴权关闭。
- 两种传法:`Authorization: Bearer <key>` 头或 body `ai_token` 字段。**adaptor 用 Bearer 头**。
- 401 = 未授权。

## cover 任务提交(JSON 形状)

```json
{
  "task_type": "cover",
  "src_audio_path": "<服务器本机绝对路径>",
  "lyrics": "<全文新词>",
  "prompt": "<风格描述,选填>",
  "audio_cover_strength": 0.55,
  "vocal_language": "zh",
  "model": "acestep-v15-turbo"
}
```

- `task_type` 枚举:text2music(缺省)/ cover / repaint / lego / extract / complete。
- `src_audio_path` 文档原文 **「requires absolute path on server」**——远程 URL 直传未获文档支持,真机才能证实。
- **multipart 替代(官方一等支持)**:`src_audio` 或 `ctx_audio` 文件字段(上传后 `_path` 参数被忽略、用临时路径),其余参数照收 Form Fields。**80 号票 adaptor 裁决:走 multipart**(下载参考字节 → multipart 提交),与 mureka 上传链同构,不算 wrapper;URL 直传留真机验证点,可行再省下载(另票)。
- `audio_cover_strength`:float 0.0–1.0,**缺省 1.0**(注意:上游缺省是 1.0 不是 0.55;画布缺省 0.55 是我们自己的表单缺省,恒显式送所以不受上游缺省影响)。
- `model`:DiT 档名(如 `acestep-v15-turbo`);**票面定案:不透传**(cover 提交体是否收 model 未证实——文档显示收,但公开名 model_map 只作调度单元,adaptor 不映射上游名,留空走服务端缺省模型)。
- `lyrics`:全文新词(词是身份必需)。
- `prompt`(alias `caption`):风格描述。
- `vocal_language`:歌词语言,en 缺省;**不透传**(上游 CoT 自侦测 `use_cot_language` 缺省 true)。
- `instrumental`:**API 无此参数**——cover 任务恒人声,与票面「instrumental:false 恒定」语义一致,请求体不送。
- 时长参数不送(cover 跟随 src_audio)。

## 查询

`POST /query_result` body `{"task_id_list": ["<uuid>"]}`:

```json
{"data": [{"task_id": "…", "status": 1, "result": "[{\"file\": \"/v1/audio?path=…\", …}]"}], "code": 200, …}
```

- 顶层响应信封:`{data, code(200), error, timestamp, extra}`;`data` 是**数组**。
- **`status` 是整数**:0 = queued/running,1 = succeeded,2 = failed。
- `result` 是 **JSON 字符串**(双层编码,须二次 unmarshal),解析后是数组,元素含 `file`(`"/v1/audio?path=…"` 相对路径)、`status`、`metas` 等。
- 任务不存在时形状未实证(可能 404 或 data 内无此元素)。

## 产物地址

`file` = `/v1/audio?path=…` → 完整产物 URL = `{BaseURL}{file}`(LAN 可达)。canvas worker `archiveAudio` 照旧转存,零新逻辑。

## 错误形状

- FastAPI:HTTP 400/401/404/415/**429(队列满)**/500,body `{"detail": "…"}`。
- **429 = 队列满 = 上游临时失败 → 须映射为可换道**(502 语义);415 Content-Type 不支持同样临时态。
- 400 = 参数错(网关 sentinel 已前置拦截,透传 400 属请求级)。

## 连通测试

`GET {base}/health`:2xx = 连通全通(带配置 key 时 401 = 凭据失败);其余如实报。官方一等端点,形状确定,优于假任务 POST /query_result。

---

## Windows 4060 装机事实(2026-11-05,来源 docs/en/INSTALL.md @ 官方仓库,部署文档用)

- **安装**:PowerShell 装 uv(`powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"`)→ `git clone https://github.com/ACE-Step-1.5.git`(官方只认 github.com/ace-step)→ `uv sync`;或 Windows 便携包(7z,内置 python_embedded,要求 CUDA 12.8)。
- **8GB VRAM 档(4060)官方推荐**:DiT 2B **turbo** + LM `acestep-5Hz-lm-0.6B` + backend **`pt`**(6–8GB 档);offload VRAM<20GB 自动开(`ACESTEP_OFFLOAD_TO_CPU` 可显式)。
- **配置放 `.env`**(`cp .env.example .env`,跨更新存活):`ACESTEP_CONFIG_PATH=acestep-v15-turbo`、`ACESTEP_LM_MODEL_PATH=acestep-5Hz-lm-0.6B`、`ACESTEP_DOWNLOAD_SOURCE=modelscope`(GFW)、`ACESTEP_API_HOST=0.0.0.0`(**缺省 127.0.0.1 局域网不可达——关键**)、`ACESTEP_API_PORT=8001`、`ACESTEP_API_KEY=<可选>`、`ACESTEP_INIT_LLM=auto`。
- **启动**:`start_api_server.bat`(CUDA)或 `uv run acestep-api`;模型首启自动下载(主仓 Ace-Step1.5 ≈10GB 含 vae + Qwen3-Embedding-0.6B + turbo + lm-1.7B;8GB 档另下 lm-0.6B),显式预下:`uv run acestep-download --download-source modelscope`。
- **共享模型目录**:`ACESTEP_CHECKPOINTS_DIR` 可指向集中目录省盘。
- **防火墙**:Windows Defender 放行 8001 入站(Python.exe 或端口规则)。
- **IP 固定**:DHCP 保留/静态(路由器侧),IP 漂移 = 渠道失联(票面定案 2)。
