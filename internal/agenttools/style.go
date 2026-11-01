package agenttools

// set_style(69 号票定案 3):把用户确认的画面风格写进本画布所属 IP 的
// ips.style —— 库表类后端直写(ADR 0006,create_character 同范式),Execute
// 落库即完成;payload 回带 {kind:"style", ip_id, style},前端据此即时刷新
// 本地风格缓存(下一次直通预填即用新值)。幂等可覆盖:同值重写不变,换值
// 即覆盖;清除风格只走 IP 编辑对话框(工具拒收空串)。
//
// 越权防线:写入目标 = 会话作用域的 IP,落笔前实时读画布行核对它「仍是」
// 画布当前所属 IP —— 画布中途被移到别的 IP(作用域过期)或作用域与画布不
// 符时一律拒绝,任何 IP 都不落笔。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/promptgen"
)

type setStyleTool struct {
	deps Deps
}

func newSetStyle(deps Deps) promptgen.AgentTool { return setStyleTool{deps: deps} }

func (t setStyleTool) Name() string { return "set_style" }

func (t setStyleTool) Definition() json.RawMessage {
	return functionDefinition(t.Name(),
		"设定本画布所属 IP 的画面风格(企划级一句话风格定位:画风/色调/质感,如「日系赛璐璐动画,明亮高饱和,柔和阴影」),写入 IP 资料;此后分镜画面与 cut→视频直通生成的提示词预填会自动带上。只在用户明确确认风格后调用;已定风格不要擅自改动,用户要求改风格并确认新风格后才可再次调用覆盖。",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"style": map[string]any{"type": "string", "description": fmt.Sprintf("用户已确认的画面风格,一句话(最多 %d 字)", canvas.MaxIPStyleRunes)},
			},
			"required": []string{"style"},
		})
}

func (t setStyleTool) Execute(ctx context.Context, args json.RawMessage) (string, json.RawMessage, error) {
	scope, err := scopeOf(ctx)
	if err != nil {
		return "", nil, err
	}
	m, err := argsOf(args)
	if err != nil {
		return "", nil, err
	}
	style, err := stringArg(m, "style")
	if err != nil {
		return "", nil, err
	}
	style = strings.TrimSpace(style)
	if style == "" {
		return "", nil, fmt.Errorf("画面风格不能为空;清除风格请在 IP 编辑对话框里操作")
	}
	if err := limitRunes("画面风格", style, canvas.MaxIPStyleRunes); err != nil {
		return "", nil, err
	}
	if t.deps.Canvases == nil {
		return "", nil, fmt.Errorf("画布存储未装配,无法核对画布所属 IP")
	}
	if t.deps.IPs == nil {
		return "", nil, fmt.Errorf("IP 存储未装配,无法设定画面风格")
	}
	cv, err := t.deps.Canvases.Get(ctx, scope.CanvasID)
	if err != nil {
		return "", nil, fmt.Errorf("读取画布失败:%w", err)
	}
	if cv.IPID != scope.IPID {
		return "", nil, fmt.Errorf("越权拒绝:只能设定当前画布所属 IP 的画面风格(画布当前属于 IP %d,会话作用域是 IP %d),请在画布所属 IP 下重新发起会话", cv.IPID, scope.IPID)
	}
	ip, err := t.deps.IPs.UpdateIP(ctx, scope.IPID, &canvas.IPUpdate{Style: &style})
	if errors.Is(err, canvas.ErrIPNotFound) {
		return "", nil, fmt.Errorf("画布所属 IP 不存在,无法设定画面风格")
	}
	if err != nil {
		return "", nil, fmt.Errorf("写入画面风格失败:%w", err)
	}
	summary := fmt.Sprintf("已设定本 IP 画面风格「%s」,后续分镜画面/视频直通生成的预填会自动带上", ip.Style)
	payload, err := json.Marshal(map[string]any{"kind": "style", "ip_id": ip.ID, "style": ip.Style})
	if err != nil {
		return summary, nil, nil
	}
	return summary, payload, nil
}
