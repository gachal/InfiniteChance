// 台词合成的计费字符口径(56 号票):字符轨的单价按万字符计,计费字符数
// 由服务端按请求文本自算、不信客户端 —— CJK 汉字与全角字符计 2、其余 rune
// 计 1(基数 = text + instruction 之和)。上游是否对 instruction 计费未
// 证实,按「扣费向上取整不低估」取保守口径;真 key 对账证实不计后再放宽。
package relay

// billableChars returns the billable character weight of one text: 2 per
// CJK ideograph or fullwidth rune, 1 per any other rune. Invalid UTF-8
// bytes surface as U+FFFD and count 1 each — a malformed request bills, it
// does not crash the books.
func billableChars(s string) int64 {
	var n int64
	for _, r := range s {
		if wideRune(r) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// wideRune reports whether r lands in a CJK/fullwidth block — the shapes
// East Asian typography renders double-width and the upstream bills
// accordingly. Emoji, ASCII, halfwidth forms and control characters are
// plain 1s.
func wideRune(r rune) bool {
	switch {
	case r >= 0x2E80 && r <= 0x9FFF: // CJK 部首/标点/假名/统一汉字(含扩展 A)
		return true
	case r >= 0xAC00 && r <= 0xD7AF: // 谚文音节
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK 兼容表意文字
		return true
	case r >= 0xFE30 && r <= 0xFE4F: // CJK 兼容形式
		return true
	case r >= 0xFF00 && r <= 0xFF60: // 全角字母/数字/符号
		return true
	case r >= 0xFFE0 && r <= 0xFFE6: // 全角货币/记号
		return true
	case r >= 0x20000 && r <= 0x2FFFD, r >= 0x30000 && r <= 0x3FFFD: // 增补平面汉字
		return true
	default:
		return false
	}
}
