package promptgen_test

// 69 号票:Agent 工具循环的画面风格链 —— 系统提示词按请求读 IP 风格注入
// (空 = 不带该段)、导演纪律「未定提议 → 确认 → set_style 落库 → 后续轮
// 遵循」全链、循环内越权拒绝,以及工具帧不进持久会话历史(63 号票纪律)。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gachal/InfiniteChance/internal/agenttools"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/promptgen"
)

// styleIPs 是 IP store 的双面桩:端点经 GetIP 读风格注入系统提示词,
// set_style 经 UpdateIP 写 —— 同一份 map,工具写入对下一请求可见(真实
// 装配里两面是同一个 IPStore)。styles 缺键 = IP 不存在;broken 非 nil 时
// 读面返回它。
type styleIPs struct {
	mu     sync.Mutex
	styles map[int64]string
	broken error
}

func (f *styleIPs) GetIP(_ context.Context, id int64) (canvas.IP, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.broken != nil {
		return canvas.IP{}, f.broken
	}
	style, ok := f.styles[id]
	if !ok {
		return canvas.IP{}, canvas.ErrIPNotFound
	}
	return canvas.IP{ID: id, Name: "测试 IP", Style: style}, nil
}

func (f *styleIPs) UpdateIP(_ context.Context, id int64, u *canvas.IPUpdate) (canvas.IP, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.styles[id]; !ok {
		return canvas.IP{}, canvas.ErrIPNotFound
	}
	if u.Style != nil {
		f.styles[id] = *u.Style
	}
	return canvas.IP{ID: id, Name: "测试 IP", Style: f.styles[id]}, nil
}

func (f *styleIPs) set(id int64, style string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.styles[id] = style
}

func (f *styleIPs) get(id int64) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.styles[id]
}

// systemContent 取第 n 次上行请求的首条 system 消息正文(无则失败)。
func systemContent(t *testing.T, relay *fakeRelay, n int) string {
	t.Helper()
	msgs, _ := relay.request(n).Body["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatalf("request %d carries no messages", n)
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" {
		t.Fatalf("request %d messages[0] = %v, want the director system message", n, first)
	}
	content, _ := first["content"].(string)
	return content
}

// endsWithStyleSegment 判定系统提示词是否以「画面风格:…」段收尾(73 号票
// 换形;导演纪律正文里也会提到该标记,只看末段)。
func endsWithStyleSegment(content string) bool {
	tail := content[strings.LastIndex(content, "\n")+1:]
	return strings.HasPrefix(tail, "画面风格:")
}

// 注入按请求读表、无缓存:同一画布两次请求之间 IP 风格被改(IP 编辑对话框
// 手填),第二次的系统提示词恰好多出末尾的风格段 —— 其余逐字不变;未定
// 风格时不带该段,导演纪律本身恒在(提议/确认/set_style)。
func TestAgentStreamInjectsIPStylePerRequest(t *testing.T) {
	relay := &fakeRelay{rounds: []string{
		relayTextRound(t, "先定方案。"),
		relayTextRound(t, "按风格推进。"),
	}}
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	ips := &styleIPs{styles: map[int64]string{3: ""}}
	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
		p.agentTools = promptgen.NewAgentToolRegistry()
		p.agentTools.Register(&stubAgentTool{name: "echo_tool"})
		p.ips = ips
	})
	body := map[string]any{"node_id": "agent-1-1", "topic": "写第 1 集", "model": "chat-m"}

	if _, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", body); !strings.Contains(raw, "[DONE]") {
		t.Fatalf("first stream = %s", raw)
	}
	unstyled := systemContent(t, relay, 0)
	if endsWithStyleSegment(unstyled) {
		t.Fatalf("system prompt without IP style ends with a style segment:\n%s", unstyled)
	}
	for _, want := range []string{"set_style", "画面风格"} {
		if !strings.Contains(unstyled, want) {
			t.Errorf("director prompt lacks %q (the propose-confirm-set_style discipline)", want)
		}
	}

	ips.set(3, "赛博朋克霓虹,高对比冷暖撞色")
	if _, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", body); !strings.Contains(raw, "[DONE]") {
		t.Fatalf("second stream = %s", raw)
	}
	styled := systemContent(t, relay, 1)
	if styled != unstyled+"\n\n画面风格:赛博朋克霓虹,高对比冷暖撞色" {
		t.Fatalf("styled system prompt = %q\nwant the unstyled prompt + the style segment suffix", styled)
	}
}

// 注入面只在带工具的会话:注册表为空时无 system、无风格段(请求与升级前
// 逐字段一致);IP 行不存在按无风格放行;读 IP 失败流前回答 JSON 500
// (校验不过不给流,31 号票纪律),上游一次都不打。
func TestAgentStreamStyleInjectionEdges(t *testing.T) {
	t.Run("空注册表不注入", func(t *testing.T) {
		relay := &fakeRelay{rounds: []string{relayTextRound(t, "纯会话。")}}
		relayServer := httptest.NewServer(relay.handler())
		t.Cleanup(relayServer.Close)
		env := newHandlerEnv(t, func(p *envParams) {
			p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
			p.ips = &styleIPs{styles: map[int64]string{3: "水彩手绘"}}
		})
		_, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{"topic": "写个提示词", "model": "chat-m"})
		if !strings.Contains(raw, "[DONE]") {
			t.Fatalf("stream = %s", raw)
		}
		msgs, _ := relay.request(0).Body["messages"].([]any)
		if len(msgs) != 1 || msgs[0].(map[string]any)["role"] != "user" {
			t.Fatalf("messages = %v, want the single legacy user turn", msgs)
		}
		if strings.Contains(msgs[0].(map[string]any)["content"].(string), "水彩手绘") {
			t.Errorf("plain session leaked the IP style into the conversation")
		}
	})

	t.Run("IP 行缺失按无风格", func(t *testing.T) {
		relay := &fakeRelay{rounds: []string{relayTextRound(t, "好。")}}
		relayServer := httptest.NewServer(relay.handler())
		t.Cleanup(relayServer.Close)
		env := newHandlerEnv(t, func(p *envParams) {
			p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
			p.agentTools = promptgen.NewAgentToolRegistry()
			p.agentTools.Register(&stubAgentTool{name: "echo_tool"})
			p.ips = &styleIPs{styles: map[int64]string{}}
		})
		_, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{"topic": "写第 1 集", "model": "chat-m"})
		if !strings.Contains(raw, "[DONE]") {
			t.Fatalf("stream = %s, want the session to proceed without style", raw)
		}
		if endsWithStyleSegment(systemContent(t, relay, 0)) {
			t.Errorf("missing IP row must not inject a style segment")
		}
	})

	t.Run("读 IP 失败流前 500", func(t *testing.T) {
		relay := &fakeRelay{}
		relayServer := httptest.NewServer(relay.handler())
		t.Cleanup(relayServer.Close)
		env := newHandlerEnv(t, func(p *envParams) {
			p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
			p.agentTools = promptgen.NewAgentToolRegistry()
			p.agentTools.Register(&stubAgentTool{name: "echo_tool"})
			p.ips = &styleIPs{styles: map[int64]string{3: ""}, broken: context.DeadlineExceeded}
		})
		res, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{"topic": "写第 1 集", "model": "chat-m"})
		if res.StatusCode != http.StatusInternalServerError {
			t.Fatalf("status = %d (%s), want 500 before any stream", res.StatusCode, raw)
		}
		if ct := res.Header.Get("Content-Type"); ct == "text/event-stream" {
			t.Fatalf("content-type = %q, want a JSON error", ct)
		}
		if relay.count() != 0 {
			t.Errorf("relay calls = %d, want none", relay.count())
		}
	})
}

// 对话内确认全链(验证点):未定风格 → 导演提议(纯文本轮,不落库)→ 用户
// 确认 → set_style 落库 → 下一请求系统提示词带上该风格(后续轮遵循)。
// 第三次请求的历史按前端持久形状回传(assistant = done.text + 摘要行),
// 上行会话里不出现任何工具帧 —— 工具调用不进持久会话历史(63 号票)。
func TestAgentStreamStyleProposeConfirmFollowChain(t *testing.T) {
	relay := &fakeRelay{rounds: []string{
		relayTextRound(t, "画面风格还没定,给你三个方案:A 日系赛璐璐;B 赛博朋克霓虹;C 水彩手绘。选哪个?"),
		relayToolRound(t, "", "call_style", "set_style", map[string]any{"style": "赛博朋克霓虹,高对比冷暖撞色"}),
		relayTextRound(t, "画面风格已定。"),
		relayTextRound(t, "按已定风格写分镜。"),
	}}
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	ips := &styleIPs{styles: map[int64]string{3: ""}}
	registry := agenttools.NewRegistry(agenttools.Deps{Canvases: fakeCanvases{}, IPs: ips})
	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
		p.agentTools = registry
		p.ips = ips
	})

	// 第一轮:未定 → 提议(不调工具、不落库)。
	_, raw1 := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{
		"node_id": "agent-1-1", "topic": "写第 1 集", "model": "chat-m",
	})
	done1 := doneFrame(t, raw1)
	if endsWithStyleSegment(systemContent(t, relay, 0)) {
		t.Fatalf("round-1 system prompt carries a style before any was set")
	}
	if ips.get(3) != "" {
		t.Fatalf("style = %q after the proposal turn, want still unset", ips.get(3))
	}

	// 第二轮:用户确认 → set_style 落库。
	history := []map[string]any{
		{"role": "user", "content": "写第 1 集"},
		{"role": "assistant", "content": persistedAssistant(done1)},
	}
	_, raw2 := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{
		"node_id": "agent-1-1", "topic": "就用 B", "model": "chat-m", "history": history,
	})
	var result map[string]any
	for _, payload := range sseFrames(t, raw2) {
		if m := frameMap(t, payload); m != nil && m["type"] == "tool_result" {
			result = m
		}
	}
	if result == nil || result["name"] != "set_style" || !strings.Contains(result["summary"].(string), "赛博朋克霓虹") {
		t.Fatalf("tool_result = %v, want the set_style receipt", result)
	}
	if p, _ := result["payload"].(map[string]any); p["kind"] != "style" || p["ip_id"].(float64) != 3 {
		t.Errorf("set_style payload = %v, want {kind:style, ip_id:3}", result["payload"])
	}
	if got := ips.get(3); got != "赛博朋克霓虹,高对比冷暖撞色" {
		t.Fatalf("ip 3 style = %q, want the confirmed style persisted", got)
	}
	done2 := doneFrame(t, raw2)

	// 第三轮:后续轮遵循 —— 系统提示词带上已定风格;持久历史无工具帧。
	history = append(history,
		map[string]any{"role": "user", "content": "就用 B"},
		map[string]any{"role": "assistant", "content": persistedAssistant(done2)},
	)
	_, raw3 := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{
		"node_id": "agent-1-1", "topic": "继续写分镜", "model": "chat-m", "history": history,
	})
	if !strings.Contains(raw3, "[DONE]") {
		t.Fatalf("third stream = %s", raw3)
	}
	third := relay.count() - 1
	if !strings.HasSuffix(systemContent(t, relay, third), "\n\n画面风格:赛博朋克霓虹,高对比冷暖撞色") {
		t.Fatalf("follow-up system prompt = %q, want the set style injected", systemContent(t, relay, third))
	}
	msgs, _ := relay.request(third).Body["messages"].([]any)
	for i, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "tool" {
			t.Errorf("messages[%d] = %v: tool frames must not ride the persisted history", i, mm)
		}
		if _, has := mm["tool_calls"]; has {
			t.Errorf("messages[%d] carries tool_calls: persisted history is text only", i)
		}
	}
}

// 循环内越权拒绝:会话开始时画布属 IP 3(作用域 = 3),执行 set_style 前
// 画布被移到 IP 5 —— 工具拒绝、两个 IP 都不落笔,拒因作为错误回执喂回
// 模型,会话照常收尾。
func TestAgentStreamSetStyleRefusedWhenCanvasMovedMidSession(t *testing.T) {
	relay := &fakeRelay{rounds: []string{
		relayToolRound(t, "", "call_style", "set_style", map[string]any{"style": "水彩手绘"}),
		relayTextRound(t, "好的,风格暂不设定。"),
	}}
	relayServer := httptest.NewServer(relay.handler())
	t.Cleanup(relayServer.Close)

	ips := &styleIPs{styles: map[int64]string{3: "", 5: ""}}
	moving := &movingCanvases{ipIDs: []int64{3, 5}}
	registry := agenttools.NewRegistry(agenttools.Deps{Canvases: moving, IPs: ips})
	env := newHandlerEnv(t, func(p *envParams) {
		p.gateway = promptgen.NewClient(relayServer.URL, "sk-relay")
		p.agentTools = registry
		p.canvases = moving
		p.ips = ips
	})

	_, raw := env.do(t, http.MethodPost, "/canvases/7/agent/stream", map[string]any{
		"node_id": "agent-1-1", "topic": "定个风格", "model": "chat-m",
	})
	var summary string
	for _, payload := range sseFrames(t, raw) {
		if m := frameMap(t, payload); m != nil && m["type"] == "tool_result" {
			summary, _ = m["summary"].(string)
		}
	}
	if !strings.Contains(summary, "越权") {
		t.Fatalf("tool_result summary = %q, want the 越权 refusal", summary)
	}
	if ips.get(3) != "" || ips.get(5) != "" {
		t.Fatalf("styles = %q / %q, want neither IP written", ips.get(3), ips.get(5))
	}
	msgs, _ := relay.request(1).Body["messages"].([]any)
	last, _ := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "tool" || !strings.Contains(last["content"].(string), "越权") {
		t.Errorf("fed-back tool frame = %v, want the refusal visible to the model", last)
	}
	if !strings.Contains(raw, "[DONE]") {
		t.Errorf("stream = %s, want the session to wrap up normally", raw)
	}
}

// movingCanvases 依次以 ipIDs 回答画布 7 的归属(用尽后停在最后一个):模拟
// 会话进行中画布被跨 IP 移动。
type movingCanvases struct {
	mu    sync.Mutex
	ipIDs []int64
	calls int
}

func (f *movingCanvases) Get(_ context.Context, id int64) (canvas.Canvas, error) {
	if id != 7 {
		return canvas.Canvas{}, canvas.ErrNotFound
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.calls
	if i >= len(f.ipIDs) {
		i = len(f.ipIDs) - 1
	}
	f.calls++
	return canvas.Canvas{ID: 7, Name: "EP01", Version: 1, IPID: f.ipIDs[i], Graph: []byte(`{"nodes":[],"edges":[]}`)}, nil
}

// doneFrame 取一次流的 done 事件(缺失即失败)。
func doneFrame(t *testing.T, raw string) map[string]any {
	t.Helper()
	for _, payload := range sseFrames(t, raw) {
		if m := frameMap(t, payload); m != nil && m["type"] == "done" {
			return m
		}
	}
	t.Fatalf("stream = %s, want a done frame", raw)
	return nil
}

// persistedAssistant 照前端持久纪律拼 assistant 轮(63 号票):最终回答
// 全文 + 工具调用摘要行(有则);循环中间帧不入持久历史。
func persistedAssistant(done map[string]any) string {
	text, _ := done["text"].(string)
	if summary, _ := done["summary"].(string); summary != "" {
		return text + "\n\n" + summary
	}
	return text
}
