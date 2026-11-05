package relay

// 79 号票(同曲换词)mp3 时长嗅探的合成测试(评审修正):合法帧流 + ID3v1
// /APEv2 尾标、ID3v2 头、尾部杂标(<2KB)都要按已计帧成功结算 —— 这些
// 尾标不动帧数据,合法参考曲常带(ID3v1 是末 128 字节 "TAG",APEv2 是末
// 32 字节 "APETAGEX",Lyrics3 等歌词尾标形态各异),逐帧走到底撞上即放弃
// 会在提交链把合法参考曲拒成请求级 400。纯垃圾与中段坏帧(剩余 ≥2KB)
// 仍拒绝:真脏数据不猜。帧形状与 music_mureka_test.go 的 murekaMP3Blob
// 同款(48000Hz/128kbps MPEG-1 Layer III,384 字节/帧 = 24ms)。

import (
	"bytes"
	"testing"
)

// mp3TestFrames builds n clean MPEG-1 Layer III frames(384 字节/帧,每帧
// 1152 样本 @48000Hz = 24ms,128kbps 无 padding)。
func mp3TestFrames(n int) []byte {
	const frameSize = 384 // 144 × 128000 / 48000 = 384,无 padding
	b := make([]byte, frameSize*n)
	for i := 0; i < len(b); i += frameSize {
		b[i], b[i+1], b[i+2], b[i+3] = 0xFF, 0xFB, 0x94, 0x00
	}
	return b
}

func TestMP3DurationMSHeadTailTolerance(t *testing.T) {
	const frameMS = 24
	cases := []struct {
		name   string
		build  func() []byte
		wantMS int64
		wantOK bool
	}{
		{"纯帧流基线", func() []byte { return mp3TestFrames(100) }, 100 * frameMS, true},
		{"ID3v1 尾标(末 128 字节 TAG)", func() []byte {
			tail := make([]byte, 128)
			copy(tail, "TAG")
			return append(mp3TestFrames(100), tail...)
		}, 100 * frameMS, true},
		{"APEv2 尾标(末 32 字节 APETAGEX)", func() []byte {
			tail := make([]byte, 32)
			copy(tail, "APETAGEX")
			return append(mp3TestFrames(100), tail...)
		}, 100 * frameMS, true},
		{"APEv2+ID3v1 叠加尾标", func() []byte {
			ape := make([]byte, 32)
			copy(ape, "APETAGEX")
			id3 := make([]byte, 128)
			copy(id3, "TAG")
			b := append(mp3TestFrames(100), ape...)
			return append(b, id3...) // APE 在前、ID3v1 在最末,依序剥
		}, 100 * frameMS, true},
		{"ID3v2 头(synchsafe size 跳过)", func() []byte {
			header := make([]byte, 10)
			copy(header, "ID3")
			header[3], header[4], header[5] = 4, 0, 0 // v4, revision 0, flags 0
			const tagSize = 54
			header[9] = tagSize // synchsafe(54) = 00 00 00 36
			pad := make([]byte, tagSize)
			return append(append(header, pad...), mp3TestFrames(100)...)
		}, 100 * frameMS, true},
		{"Lyrics3 式尾部杂标(<2KB 容差结算)", func() []byte {
			tail := bytes.Repeat([]byte("Lyrics3v2 tail garbage;"), 10) // 220B,不以 TAG/APETAGEX 开头
			return append(mp3TestFrames(100), tail...)
		}, 100 * frameMS, true},
		{"纯垃圾仍失败", func() []byte { return []byte("definitely not an audio stream") }, 0, false},
		{"中段坏帧(剩余 ≥2KB)仍失败", func() []byte {
			b := mp3TestFrames(100)
			copy(b[50*384:], "XX") // 破坏第 50 帧帧头:此后剩余 ~19200B ≥ 2KB,不兜底
			return b
		}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ms, ok := mp3DurationMS(tc.build())
			if ok != tc.wantOK || (ok && ms != tc.wantMS) {
				t.Errorf("mp3DurationMS = (%d, %v), want (%d, %v)", ms, ok, tc.wantMS, tc.wantOK)
			}
		})
	}
}
