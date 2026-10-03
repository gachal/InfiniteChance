/**
 * 模型价格(36 号票补 UI):/admin/prices 挂网关管理面(JWT 会话)。
 * @infinitechance/api 按 36 号票定案保持原样,价格三端点走本应用自己的
 * requestClient(同一错误形状与 401 处理,错误提示由拦截器统一弹出)。
 * 金额在 API 边缘就是人类单位(USD/百万 token、USD/次、倍率 ×1.0),
 * 微美元换算发生在服务端 handler,前端不接触计费算术。
 */

import { requestClient } from '#/api/request';

/** 计价轨道:token = 聊天/视频按 token ×倍率;call = 生图按张;second = 生视频按秒。 */
export type PriceUnit = 'call' | 'second' | 'token';

/** 一条模型价格(GET 列表行 / PUT 响应)。哪些字段有意义随 unit 而定:
 * token 行用 per-mtokens/ratio 族,call/second 行用 usd_per_call/size_factors 族;
 * 视频 token 行(25 号票)另带秒折算表。 */
export interface ModelPrice {
  public_model: string;
  unit: PriceUnit;
  input_usd_per_mtokens: number;
  output_usd_per_mtokens: number;
  ratio: number;
  size_tokens_per_second?: Record<string, number>;
  default_tokens_per_second?: number;
  usd_per_call: number;
  size_factors?: Record<string, number>;
  created_at: string;
  updated_at: string;
}

/** PUT 请求体:服务端按 unit 只读对应字段族并做 Normalize 校验
 * (未配价模型在 /v1 一律 model_not_priced 拒绝,本模块不改变该语义)。 */
export interface ModelPriceInput {
  public_model: string;
  unit: PriceUnit;
  input_usd_per_mtokens?: number;
  output_usd_per_mtokens?: number;
  ratio?: number;
  size_tokens_per_second?: Record<string, number>;
  default_tokens_per_second?: number;
  usd_per_call?: number;
  size_factors?: Record<string, number>;
}

export async function listModelPrices(): Promise<ModelPrice[]> {
  const body = await requestClient.get<{ prices: ModelPrice[] }>('/admin/prices');
  return body.prices;
}

/** 新建或按公开模型名覆盖一条价格。 */
export function upsertModelPrice(input: ModelPriceInput): Promise<ModelPrice> {
  return requestClient.put('/admin/prices', input);
}

/** 删除一条价格;model 走查询参数(模型名可含斜杠,不能进路径)。 */
export async function deleteModelPrice(model: string): Promise<void> {
  await requestClient.delete(`/admin/prices?model=${encodeURIComponent(model)}`);
}
