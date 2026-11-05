package relay

// 80 号票 acestep adaptor 的内部面测试:三个形状 sentinel 的第三枚
// (参数形状非法 —— cover_strength 越界)在网关 prepareMusic 已前置校验
// 之外作纵深防御,公共 HTTP 面构造不出越界载荷,只能在本包直打 adaptor;
// coverStrengthOutOfRange 闭域判定的 NaN/Inf 分支同理(合法 JSON 载不进
// NaN,1e999 在 unmarshal 就上溢报错,公共面只测得到负数与 >1)。
//
// sentinel 前两枚(audio_ref 空 / 歌词空)与上传链、multipart 形状、
// 状态归一已由 music_acestep_test.go 的端到端面覆盖,此处不重复。

import (
	"errors"
	"math"
	"testing"

	"github.com/gachal/InfiniteChance/internal/channel"
)

// 参数形状非法 sentinel:audio_ref 与歌词齐备而 cover_strength 越界,
// adaptor 必须在拨号之前以 errAcestepInvalidShape 拒绝(纵深防御 ——
// 请求体在网关校验后被改写过的未来路径也拦得住)。
func TestAcestepAdaptorRejectsOutOfRangeCoverStrength(t *testing.T) {
	a := newAcestepAdaptor()
	ch := channel.Channel{Type: channel.TypeAcestep, BaseURL: "http://127.0.0.1:1", APIKey: ""}

	payload := []byte(`{"model":"m","prompt":"p","lyrics":"夜色","audio_ref":"http://127.0.0.1:1/ref.mp3","cover_strength":1.5}`)
	resp, err := a.MusicSubmit(t.Context(), ch, payload)
	if !errors.Is(err, errAcestepInvalidShape) {
		t.Fatalf("err = %v, want errAcestepInvalidShape", err)
	}
	if resp != nil {
		t.Errorf("resp = %+v, want nil (no fabricated response for a request-level refusal)", resp)
	}
}

// coverStrengthOutOfRange 闭域 [0,1]:负数、>1、NaN、±Inf 拒;0、1、0.55
// 收。
func TestCoverStrengthOutOfRange(t *testing.T) {
	cases := []struct {
		name string
		v    float64
		want bool
	}{
		{"0 合法", 0, false},
		{"1 合法", 1, false},
		{"0.55 合法", 0.55, false},
		{"负数拒", -0.001, true},
		{"大于 1 拒", 1.001, true},
		{"NaN 拒", math.NaN(), true},
		{"+Inf 拒", math.Inf(1), true},
		{"-Inf 拒", math.Inf(-1), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := coverStrengthOutOfRange(tc.v); got != tc.want {
				t.Errorf("coverStrengthOutOfRange(%v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}
