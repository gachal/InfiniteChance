package relay

import (
	"math/rand/v2"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// 调度(06 号票):同一公开模型挂多渠道时,优先级分层做故障转移——先
// 试最高层,层内按权重加权随机分流;某渠道失败换下一个,整层试穿降级到
// 下一层。Store.List 已按 priority DESC, id ASC 返回,下面的纯函数只在
// 该顺序上做过滤与层内乱序。

// eligibleChannels filters the channels that may serve publicModel for the
// given request kind: enabled, capable (07 号票:聊天/生图互不串道), and
// mapping the model. Store.List order preserved (best priority first, ties
// by id).
func eligibleChannels(channels []channel.Channel, publicModel string, need channel.Capability) []channel.Channel {
	var out []channel.Channel
	for _, ch := range channels {
		if !ch.Enabled || !ch.HasCapability(need) {
			continue
		}
		if _, ok := ch.ModelMap[publicModel]; ok {
			out = append(out, ch)
		}
	}
	return out
}

// weightedOrder returns the failover attempt order: priority tiers from
// highest down; within a tier the members leave in weighted-random order —
// each draw picks entry i with probability weight_i/Σweights, a weight
// below 1 counting as 1 so every configured channel keeps a share (all-zero
// weights degrade to uniform). intn supplies randomness, bound n ≥ 1.
func weightedOrder(cands []channel.Channel, intn func(int) int) []channel.Channel {
	out := make([]channel.Channel, 0, len(cands))
	rest := cands
	for len(rest) > 0 {
		tier := 1
		for tier < len(rest) && rest[tier].Priority == rest[0].Priority {
			tier++
		}
		pool := append([]channel.Channel(nil), rest[:tier]...)
		rest = rest[tier:]
		for len(pool) > 0 {
			total := 0
			weights := make([]int, len(pool))
			for i, ch := range pool {
				w := ch.Weight
				if w < 1 {
					w = 1
				}
				weights[i] = w
				total += w
			}
			pick := intn(total)
			chosen := len(pool) - 1
			for i, w := range weights {
				if pick < w {
					chosen = i
					break
				}
				pick -= w
			}
			out = append(out, pool[chosen])
			pool = append(pool[:chosen], pool[chosen+1:]...)
		}
	}
	return out
}

// randIntn returns the randomness source of the handler: the injected Rand
// for deterministic tests, the package-level generator otherwise
// (goroutine-safe).
func (h *Handlers) randIntn(n int) int {
	if h.Rand != nil {
		return h.Rand.IntN(n)
	}
	return rand.IntN(n)
}
