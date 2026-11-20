package settings_test

import (
	"testing"

	"github.com/gachal/InfiniteChance/internal/settings"
)

// NormalizePetConfig 直接钉测(92 号票规则,94 号票沿用为逐条目校验):
// 缺省回落、动图键缺失放行、chips 形状收敛(空串剔除、全空规约 nil)。
func TestNormalizePetConfig(t *testing.T) {
	// 缺省回落:零值配置合法(未配置 = 前端 CSS 缺省形象)。
	var zero settings.PetConfig
	if err := settings.NormalizePetConfig(&zero); err != nil {
		t.Fatalf("zero config should be valid: %v", err)
	}

	// 动图键缺失:两个 image 键都空 = 合法(纯文案配置)。
	onlyText := settings.PetConfig{Name: "球球", Greeting: "嗨", Chips: []string{"a"}}
	if err := settings.NormalizePetConfig(&onlyText); err != nil {
		t.Fatalf("missing image keys should be valid: %v", err)
	}

	// chips 形状:空串剔除、trim、全空规约成 nil。
	chips := settings.PetConfig{Chips: []string{"  a  ", "", "b"}}
	if err := settings.NormalizePetConfig(&chips); err != nil {
		t.Fatalf("chips normalize: %v", err)
	}
	if len(chips.Chips) != 2 || chips.Chips[0] != "a" || chips.Chips[1] != "b" {
		t.Fatalf("chips = %v, want [a b]", chips.Chips)
	}
	empty := settings.PetConfig{Chips: []string{"", "   "}}
	if err := settings.NormalizePetConfig(&empty); err != nil {
		t.Fatalf("all-empty chips should be valid: %v", err)
	}
	if empty.Chips != nil {
		t.Fatalf("all-empty chips = %v, want nil", empty.Chips)
	}

	// 动图键形状:合法 uploads/ 键放行;非法前缀/穿越拒绝。
	okKey := settings.PetConfig{IdleImage: "uploads/20261119/x.webp"}
	if err := settings.NormalizePetConfig(&okKey); err != nil {
		t.Fatalf("valid uploads key rejected: %v", err)
	}
	for _, bad := range []string{
		"relay/20261119/x.gif", // 不是 uploads/ 前缀
		"uploads/../x.gif",     // 穿越
		"uploads/a\\b.gif",     // 反斜杠
		"/uploads/x.gif",       // 绝对路径
	} {
		badKey := settings.PetConfig{IdleImage: bad}
		if err := settings.NormalizePetConfig(&badKey); err == nil {
			t.Fatalf("idle_image %q should be rejected", bad)
		}
	}
}
