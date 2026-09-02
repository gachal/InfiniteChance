package relay

import (
	"testing"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// 调度顺序(06 号票)的纯函数测试:随机源由测试注入,断言是确定性的 ——
// 优先级分层做故障转移,层内按权重加权随机。

func chOf(id int64, priority, weight int) channel.Channel {
	return channel.Channel{ID: id, Name: string(rune('A' + id)), Priority: priority, Weight: weight}
}

func TestWeightedOrderTiersDominateAndPreserveTieOrder(t *testing.T) {
	// intn 恒 0:每层每次都抽中剩余的第一个 —— 高优先级层整层在前,
	// 层内相对顺序保持;低优先级层整层垫底。
	cands := []channel.Channel{chOf(1, 10, 0), chOf(2, 10, 0), chOf(3, 5, 99)}
	got := weightedOrder(cands, func(int) int { return 0 })
	want := []int64{1, 2, 3}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("order = %v, want %v", ids(got), want)
		}
	}
}

func TestWeightedOrderDrawsByWeight(t *testing.T) {
	// A 权重 9、B 权重 1:总份 10。注入的抽签 9 → 落在 B 的那一份,
	// B 先出列;第二轮只剩 A,intn 照常被问(抽几都一样)。
	a, b := chOf(1, 0, 9), chOf(2, 0, 1)
	draws := []int{9, 0}
	i := 0
	got := weightedOrder([]channel.Channel{a, b}, func(int) int { v := draws[i]; i++; return v })
	if got[0].ID != 2 || got[1].ID != 1 {
		t.Fatalf("order = %v, want [2 1] for the scripted draw 9/10", ids(got))
	}
}

func TestWeightedOrderZeroWeightStillGetsAShare(t *testing.T) {
	// 权重 0 按 1 计:A、B 各占一份,total=2;抽签 1 抽中 B。
	a, b := chOf(1, 0, 0), chOf(2, 0, 0)
	got := weightedOrder([]channel.Channel{a, b}, func(int) int { return 1 })
	if got[0].ID != 2 || got[1].ID != 1 {
		t.Fatalf("order = %v, want [2 1]: zero weights degrade to uniform", ids(got))
	}
}

func TestWeightedOrderEmptyAndSingle(t *testing.T) {
	if got := weightedOrder(nil, func(int) int { return 0 }); len(got) != 0 {
		t.Fatalf("order = %v, want empty", ids(got))
	}
	one := []channel.Channel{chOf(1, 0, 0)}
	if got := weightedOrder(one, func(int) int { return 0 }); got[0].ID != 1 {
		t.Fatalf("order = %v, want the single channel", ids(got))
	}
}

func TestEligibleChannelsFiltersEnabledAndModel(t *testing.T) {
	channels := []channel.Channel{
		{ID: 1, Enabled: true, ModelMap: map[string]string{"m": "up"}},
		{ID: 2, Enabled: false, ModelMap: map[string]string{"m": "up"}},
		{ID: 3, Enabled: true, ModelMap: map[string]string{"other": "up"}},
		{ID: 4, Enabled: true, ModelMap: map[string]string{"m": "up4"}},
	}
	got := eligibleChannels(channels, "m")
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 4 {
		t.Fatalf("eligible = %+v, want channels 1 and 4 in list order", got)
	}
}

func ids(channels []channel.Channel) []int64 {
	out := make([]int64, len(channels))
	for i, ch := range channels {
		out[i] = ch.ID
	}
	return out
}
