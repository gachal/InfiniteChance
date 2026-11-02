/**
 * IP 画面风格段(69 号票):企划级一句话风格定位(ips.style)在生成路径上的
 * 统一拼装。形状与服务端 canvas.StyleSegment 一致 ——「画面风格:」+ trim
 * 后的风格文本,两侧各有字面量测试钉住;风格段放 prompt 前部,随提交落任务
 * 行可审计(生成记录面板可查)。
 *
 * 消费方:分镜 cut 画面直通(storyboard.buildCutFramePrompt)、cut→视频直通
 * (storyboard.buildCutVideoPrompt)、composer「带 IP 风格」开关的发送拼装;
 * 70 号票角色设定图预填复用同一对纯函数。Agent 聊天注入不走这里(52 号票
 * 规则不动)。
 *
 * 73 号票:段标记由「【画面风格】」换形为「画面风格:」(半角冒号)——
 * 官方 Seedance 提示词指南把 `【】` 定为字幕标记,旧形态可能把「画面风格」
 * 四字渲染成字幕;与服务端 canvas.StyleSegment 同步换形,69 号票口径随之
 * 修订(段判定/不叠段检查走本常量,自动换形)。
 */

/** 风格段固定标记(与服务端 canvas.StyleSegment 同形;73 号票换形躲开
 * 官方 `【】` 字幕标记语义)。 */
export const STYLE_SEGMENT_MARK = '画面风格:'

/** IP 画面风格 → 风格段;未定(空/全空白/缺省)= 空串,调用方据此不拼不
 * 显示 —— 存量行为逐字节不变。 */
export function styleSegment(style: string | null | undefined): string {
  const trimmed = (style ?? '').trim()
  return trimmed === '' ? '' : `${STYLE_SEGMENT_MARK}${trimmed}`
}

/** 把风格段拼到提示词前部(注入面统一出口):无风格原样返回;提示词已含
 * 同一风格段(直通预填已拼过、composer 开关又开着)不重复拼;提示词为空
 * (含全空白)只答风格段 —— 素材全空的直通预填仍带风格,用户在其后续写;
 * 否则「风格段 + 换行 + 提示词」。纯函数,不改入参。 */
export function prependStyleSegment(prompt: string, style: string | null | undefined): string {
  const segment = styleSegment(style)
  if (segment === '') {
    return prompt
  }
  if (prompt.trim() === '') {
    return segment
  }
  if (prompt.includes(segment)) {
    return prompt
  }
  return `${segment}\n${prompt}`
}

/** composer「带 IP 风格」开关的发送拼装:开关关 = 手写 prompt 原样发送
 * (默认关,不偷改);开 = 经 prependStyleSegment 拼风格段(无风格/已含同段
 * 时同样原样)。开关状态由编辑器按图片/视频模式各存一份草稿(37 号票透明
 * 背景开关范式)。 */
export function styledSendPrompt(prompt: string, withStyle: boolean, style: string | null | undefined): string {
  return withStyle ? prependStyleSegment(prompt, style) : prompt
}
