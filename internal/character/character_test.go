package character_test

import (
	"testing"

	"github.com/gachal/InfiniteChance/internal/character"
)

// 封面投影是纯函数,投影顺序在此直接钉死(评审修复:删掉不可达的第二遍
// 扫描后,函数与 51 号票 API 节规则统一成一条:逐变体扫描,首个有图的
// 变体内视图类优先、无视图类图取第一张;变体序先于视图类,跨变体兜底,
// 全空 = 0)。落库链路的投影再由 store 测试各钉一例(视图类优先/回落/
// 空角色),两边口径一致。
func TestCoverAssetIDProjectionOrder(t *testing.T) {
	img := func(id int64, label string) character.Image {
		return character.Image{AssetID: id, Label: label}
	}

	cases := []struct {
		name     string
		variants []character.Variant
		want     int64
	}{
		{
			name: "no images at all answers 0",
			variants: []character.Variant{
				{ID: 1},
				{ID: 2},
			},
			want: 0,
		},
		{
			name: "view label wins inside the first variant",
			variants: []character.Variant{
				{ID: 1, Images: []character.Image{img(11, "表情集"), img(12, "多视图"), img(13, "三视图")}},
			},
			want: 12,
		},
		{
			name: "first image of the first variant without a view label",
			variants: []character.Variant{
				{ID: 1, Images: []character.Image{img(9, "姿态"), img(10, "色板")}},
			},
			want: 9,
		},
		{
			name: "variant order beats a view label in a later variant",
			variants: []character.Variant{
				{ID: 1, Images: []character.Image{img(11, "表情集")}},
				{ID: 2, Images: []character.Image{img(13, "三视图")}},
			},
			want: 11,
		},
		{
			name: "empty first variant falls through to the next one",
			variants: []character.Variant{
				{ID: 1},
				{ID: 2, Images: []character.Image{img(21, "色板"), img(22, "三视图")}},
			},
			want: 22,
		},
		{
			name:     "no variants answers 0",
			variants: nil,
			want:     0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := character.CoverAssetID(character.Character{Variants: tc.variants})
			if got != tc.want {
				t.Fatalf("CoverAssetID = %d, want %d", got, tc.want)
			}
		})
	}
}
