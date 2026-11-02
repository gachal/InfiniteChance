package canvas_test

import (
	"testing"

	"github.com/gachal/InfiniteChance/internal/canvas"
)

// 风格段形状(69 号票,73 号票换形;与前端 canvas/web/src/ipStyle.ts
// styleSegment 同形):「画面风格:」+ trim 后的风格文本;未定(空/全空白)
// = 空串,调用方据此不拼不显示。70 号票角色设定图 prompt 消费同一形状。
func TestStyleSegment(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"赛博朋克霓虹,高对比冷暖撞色", "画面风格:赛博朋克霓虹,高对比冷暖撞色"},
		{"  水彩手绘  ", "画面风格:水彩手绘"},
		{"", ""},
		{" \n\t ", ""},
	}
	for _, tc := range cases {
		if got := canvas.StyleSegment(tc.in); got != tc.want {
			t.Errorf("StyleSegment(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
