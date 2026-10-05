# 38 号票:计费全链人民币

状态:定案(2026-10-05,用户直接下令 + 两问定案:全链人民币 / 重配不迁移)

## 背景

04 号票起的库内金额单位为微美元(1e6 = $1)。实际付费上游(腾讯云 VOD、火山方舟)与作者本人账单都以人民币计,美元层是无意义的换算中介;管理台价格页、额度、用量审计全部以美元展示,与使用语境错位。

## 定案

1. **全链人民币**:库内单位改为**微人民币(micro-CNY,1e6 = ¥1)整数**,单一币种、无汇率层。价格表、API key 额度、扣费流水、用量审计、管理 API 边界全部随之改口径。
2. **重配不迁移**:不做任何存量数据迁移代码。
   - 价格表:`model_prices` 存量行按微美元存的数值不作换算,**由管理员在管理台清空重配**人民币价;
   - 额度:既有余额数值不追溯,管理员直接重设一个人民币整数额度;
   - 历史流水:`usage_logs` / `api_key_quota_log` 历史行保留原值(视作历史账,不追溯折算),`price_snapshot` 里旧 `usd_per_call_micros` 键名照旧可读(审计只解 `unit` 与 `request`,见 usage 侧代码)。
3. **命名随语义**:代码标识符与 admin API JSON 字段同步去美元化——`MicrosPerUSD`→`MicrosPerCNY`、`USDToMicros`/`MicrosToUSD`→`CNYToMicros`/`MicrosToCNY`、`MaxAmountUSD`→`MaxAmountCNY`、`MaxUSDPerMTokens`/`MaxUSDPerCall`→`MaxCNYPerMTokens`/`MaxCNYPerCall`、`CallPrice.USDPerCallMicros`(`usd_per_call_micros`)→`CNYPerCallMicros`(`cny_per_call_micros`);admin 边界字段 `quota_usd`/`initial_quota_usd`/`amount_usd`/`delta_usd`/`balance_usd`→`*_cny` 族、`input_usd_per_mtokens`/`output_usd_per_mtokens`/`usd_per_call`→`*_cny_*` 族、`charge_usd`→`charge_cny`。自用单实例,不留旧字段兼容层。
4. **扣费算术零改动**:微整数、向上取整、big.Int 防溢出、预扣-多退少补-退款时序全部不变——只换币种语义,不动不变量的形状。AGENTS.md 硬性约束 2 的「微美元」改写为「微人民币」。
5. **画布侧目录带价(顺带,服务成本可见性)**:`/image-models`、`/video-models`、`/prompt-models` 在既有 `models` 名单旁增补 `prices` 摘要映射(单位、人民币单价、尺寸/分辨率系数、token 轨每百万单价),响应对既有消费者向后兼容;画布生成对话框据此次级展示与发送前预估。

## 显式不做

- 汇率配置(settings 汇率项)、美元残留展示开关、多币种并存。
- 存量数据迁移脚本(见定案 2)。
- `/v1` 中转面错误形状变化:`insufficient_quota` 等错误体本就不带金额单位词,不动。
