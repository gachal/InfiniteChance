# ACE-Step 1.5 自托管部署指南(Windows + RTX 4060)

> 80 号票(同曲换词通路二落)的宿主机装机文档。画布「同曲换词」创作态走 `acestep` 渠道直连本机服务(`docs/en/API.md` 契约,网关 adaptor `internal/relay/acestep.go`)。官方仓库**只认 github.com/ace-step**(官方有仿冒域名警告),权重 MIT、个人自用无合规约束。

## 机器与网络前提

| 项 | 要求 |
| --- | --- |
| 宿主机 | Windows,RTX 4060(8GB VRAM),与 Mac **同一局域网** |
| 磁盘 | ≥15GB 空闲(主仓权重 ≈10.1GB + 环境 + 临时产物) |
| Python | 3.11–3.12(uv 自动管理,无需预装) |
| CUDA | 驱动支持 CUDA 12.x(便携包路线要求 12.8) |

**IP 固定(必做)**:路由器侧给这台机器做 DHCP 保留或直接配静态 IP。IP 漂移 = 网关渠道失联(渠道 BaseURL 是 `http://<win-ip>:8001` 写死在配置里)。下文以 `192.168.1.100` 为例。

## 一、安装(uv venv 裸机路线,官方推荐)

PowerShell 执行:

```powershell
# 1. 装 uv 包管理器
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"

# 2. 克隆官方仓库并装依赖
git clone https://github.com/ace-step/ACE-Step-1.5.git
cd ACE-Step-1.5
uv sync
```

> 备选:**Windows 便携包**(依赖预装,7z 解压即用,要求 CUDA 12.8)——`https://files.acemusic.ai/acemusic/win/ACE-Step-1.5.7z`。两条路线任选其一,便携包免 `uv sync`。

## 二、配置(.env)

```powershell
copy .env.example .env
```

编辑 `.env`(`.env` 不进 git,跨更新存活):

```ini
# DiT 档:8GB 显存官方推荐 turbo(8 步蒸馏)
ACESTEP_CONFIG_PATH=acestep-v15-turbo
# LM:6–8GB 档官方推荐 0.6B + pt 后端(vllm 是 8GB+ 档,内存换不合适就 pt)
ACESTEP_LM_MODEL_PATH=acestep-5Hz-lm-0.6B
ACESTEP_LM_BACKEND=pt
# 下载源:国内走 ModelScope(GFW 无忧,官方原生支持)
ACESTEP_DOWNLOAD_SOURCE=modelscope
# 绑定:缺省 127.0.0.1 局域网不可达,必须 0.0.0.0
ACESTEP_API_HOST=0.0.0.0
ACESTEP_API_PORT=8001
# 鉴权:可选;设了则网关渠道配置里同值填入 api_key,不设则留空
# ACESTEP_API_KEY=sk-your-secret
# LLM 初始化:auto = 按 VRAM 自动(推荐)
ACESTEP_INIT_LLM=auto
```

权重集中盘/多安装共享时加 `ACESTEP_CHECKPOINTS_DIR=D:\ace-models`。

## 三、模型预下载(可选,建议)

首启会自动下载(主仓 ≈10.1GB:vae + Qwen3-Embedding-0.6B + turbo DiT + lm-1.7B;8GB 档另下 lm-0.6B)。想提前拉完:

```powershell
uv run acestep-download --download-source modelscope
```

## 四、启动与防火墙

```powershell
# 一键启动(官方脚本:自动环境检测 + 依赖核对 + 起服务)
start_api_server.bat
# 或直接
uv run acestep-api
```

脚本顶部变量(`DOWNLOAD_SOURCE`/`LM_MODEL_PATH`/`PORT` 等)可改,`.env` 优先级更高且不怕更新覆盖。

**防火墙放行 8001 入站**(缺省会拦局域网):Windows Defender 防火墙 → 入站规则 → 新建 → 端口 TCP 8001 允许;或按程序放行 python.exe。

**自验**(Windows 本机 + Mac 各跑一次):

```bash
curl http://192.168.1.100:8001/health
# 期望:{"data":{"status":"ok","service":"ACE-Step API",...},"code":200,...}
```

Mac 能拉通 `health` 即链路通;网关连通测试按钮走的就是这个端点。

## 五、开机自启(增量,未做)

官方无内置服务化;可 Task Scheduler 登录触发 `start_api_server.bat`(最小化)。**本票不做**,服务在线性由用户保证——离线时画布提交走既有换道/同步失败链,网关零特殊处理。

## 六、Mac 侧网关配置(部署欠账清单)

1. admin 台新建渠道:类型 `acestep`,BaseURL `http://192.168.1.100:8001`,music 能力位,**api_key 留空**(服务端未设 `ACESTEP_API_KEY` 时);设了 key 则同值填入。
2. model_map 配公开名 **`acestep-relyric`**(→ 档名任意非空占位,如 `acestep-v15-turbo`;adaptor 不透传 model)。
3. 计价:**call 轨配价 ¥0**(自托管无边际成本;未配价会被 `model_not_priced` 拒)。
4. 一键连通测试:绿 = health 链路全通。

## 七、真机验证点(票面定案 11,按序)

1. **cover 参数面**:`release_task` 对 `src_audio` 远程 URL 的实际接受度(文档只承诺服务器本机路径/multipart;当前 adaptor 走 multipart,URL 直传可行可另票省下载);`audio_cover_strength` 字段名与取值范围实测。
2. **旋律保持度**:色谱对撞 + 人耳终审,**0.3 / 0.55 / 0.9 三档 strength 各一曲**(带同曲阳性对照);CUDA 后端 strength 生效性(#1346 是 MLX 特有,CUDA 预期正常)。旋律级下限判级,不达 → 分轨 MIDI/SVS 管线另票(Plan C)。
3. **歌词跟随度**:全文新词漏唱/塌段(#1132 已知边界:音节对齐 + batch 多抽人工挑为可接受工作流)。
4. **可达性链**:Windows 拉 COS 产物 URL(素材存储切 COS 后);Mac worker 拉 Windows 产物 URL(防火墙放行核验)。
5. **性能**:4060 turbo 单曲耗时(基准 3090 <10s,预估 15–60s;网关 30min 计费时限、worker 15min 轮询时限充裕)。

## 坑位备忘

- `ACESTEP_API_HOST` 缺省 `127.0.0.1`——忘改 = Mac 侧连接拒绝,症状与防火墙拦截相同,先查绑定再查防火墙。
- 8GB 档若 OOM:`ACESTEP_OFFLOAD_TO_CPU=true`(换时长),或确认 LM 走 0.6B 而非主仓自带的 1.7B。
- 环境变量族速查见官方 `docs/en/API.md` §12(`ACESTEP_TMPDIR`/队列/缓存等)。
