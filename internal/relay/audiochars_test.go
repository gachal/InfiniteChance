package relay

// 56 号票:台词合成的计费字符数由服务端自算,不信客户端 —— CJK 汉字与
// 全角字符计 2、其余 rune 计 1(基数 = text + instruction 之和,扣费向上
// 取整不低估的保守口径)。表驱动钉死每类字符的口径。

import "testing"

func TestBillableChars(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want int64
	}{
		{"empty", "", 0},
		{"ascii", "hello", 5},
		{"ascii with space", "hi there", 8},
		{"han chars count 2", "你好", 4},
		{"fullwidth punctuation counts 2", "你好\uFF0C世界\uFF01", 12}, // 全角逗号/叹号各计 2
		{"halfwidth punctuation counts 1", "hi!", 3},               // ASCII ! 计 1
		{"mixed ascii and han", "hi你好", 6},                         // 2 + 4
		{"mixed sentence", "你好world,\uFF0C再见!", 17},                // 4×2 + 5 + 1(半角) + 2(全角) + 2×2 + 1
		{"fullwidth letters and digits", "ＡＢ１２", 8},                // U+FF21 FF21…
		{"halfwidth katakana counts 1", "ｶﾀｶﾅ", 4},                 // 半角片假名 U+FF76 起,计 1
		{"kana counts 2", "かなカナ", 8},                               // 平假名/片假名全角计 2
		{"emoji counts 1", "😀", 1},
		{"emoji repeated", "😀😀 hi", 5},
		{"cjk ext B counts 2", "\U00020000", 2}, // 𠀀:增补平面汉字同口径
		{"hangul syllable counts 2", "한글", 4},
		{"newline counts 1", "a\nb", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := billableChars(tc.text); got != tc.want {
				t.Errorf("billableChars(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
	// 基数 = text + instruction 之和:分别计再相加与连起来计一致。
	text, instruction := "你好", "用河南话"
	if got := billableChars(text) + billableChars(instruction); got != billableChars(text+instruction) {
		t.Errorf("sum %d != concatenated %d", got, billableChars(text+instruction))
	}
}
