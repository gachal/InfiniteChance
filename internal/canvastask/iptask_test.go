package canvastask

import (
	"strings"
	"testing"
)

// 五类型模板的字面量钉子(70 号票):模板是 canvas/server 代码常量,不进
// 技能库;前端 canvas/web/src/sheet.ts 备一份同字面量,两侧各自用本组
// 字面量钉住防漂移 —— 改任何一段文案必须两侧同改。
func TestSheetTypeTemplatesPinLiterals(t *testing.T) {
	want := map[string]string{
		SheetTypeTurnaround: "角色设定图·三视图:同一角色的全身站姿正面、侧面、背面三视图对照,白底,全身完整无裁切,各视角比例一致,标准设定图画风",
		SheetTypeMultiView:  "角色设定图·多视图:同一角色的全身多角度视图对照(正面、四分之三、侧面、背面),白底,各视角比例一致,标准设定图画风",
		SheetTypeExprSheet:  "角色设定图·表情集:同一角色头部的多种表情对照(喜、怒、哀、惊等),白底,构图整齐,表情差异清晰,标准设定图画风",
		SheetTypePalette:    "角色设定图·色板:该角色的配色色板展示(发色、肤色、服装主色与辅色色块并列),白底,标注干净,标准设定图画风",
		SheetTypePose:       "角色设定图·姿态集:同一角色的多个典型动作与姿态对照,全身,白底,姿态自然清晰,标准设定图画风",
	}
	if len(SheetTypes) != 5 {
		t.Fatalf("SheetTypes = %v, want exactly five types", SheetTypes)
	}
	if SheetTypes[0] != SheetTypeTurnaround {
		t.Fatalf("默认类型必须是三视图,got %q", SheetTypes[0])
	}
	for _, typ := range SheetTypes {
		got, ok := sheetTypeTemplates[typ]
		if !ok {
			t.Fatalf("type %q has no template", typ)
		}
		if got != want[typ] {
			t.Fatalf("template[%q] =\n%q\nwant\n%q", typ, got, want[typ])
		}
	}
}

// 拼装口径(70 号票定案):类型模板 + 角色四件套(name/alias/description/
// detail)+ 变体名与服装描述,IP 风格段(69 号票 StyleSegment,有则拼)放
// prompt 前部单独成行;空字段整行跳过;总长截断 ≤2000 runes。
func TestBuildSheetPromptAssembly(t *testing.T) {
	f := SheetFacts{
		Name: "林小满", Alias: "小满",
		Description: "爱冒险的少女",
		Detail:      "十六岁,黑色短发",
		VariantName: "冬季制服",
		VariantNote: "深蓝色大衣配红色围巾",
	}
	got := BuildSheetPrompt(SheetTypeTurnaround, f, "")
	want := strings.Join([]string{
		"角色设定图·三视图:同一角色的全身站姿正面、侧面、背面三视图对照,白底,全身完整无裁切,各视角比例一致,标准设定图画风",
		"角色:林小满",
		"别名:小满",
		"简介:爱冒险的少女",
		"详细设定:十六岁,黑色短发",
		"造型:冬季制服",
		"服装:深蓝色大衣配红色围巾",
	}, "\n")
	if got != want {
		t.Fatalf("BuildSheetPrompt =\n%q\nwant\n%q", got, want)
	}

	// 有风格:风格段拼在最前、单独成行(69 号票注入面同形,73 号票换形)。
	withStyle := BuildSheetPrompt(SheetTypeTurnaround, f, " 赛博朋克冷色调 ")
	if !strings.HasPrefix(withStyle, "画面风格:赛博朋克冷色调\n"+want) {
		t.Fatalf("style segment must lead the prompt, got %q", withStyle)
	}

	// 空字段整行跳过:只有必给的名字与变体名。
	bare := BuildSheetPrompt(SheetTypePose, SheetFacts{Name: "阿银", VariantName: "默认"}, "")
	wantBare := "角色设定图·姿态集:同一角色的多个典型动作与姿态对照,全身,白底,姿态自然清晰,标准设定图画风\n角色:阿银\n造型:默认"
	if bare != wantBare {
		t.Fatalf("bare =\n%q\nwant\n%q", bare, wantBare)
	}

	// 未定类型不在此裁决(handler 拦),但拼装对未知类型落空模板行 ——
	// 防御面:未知 type 不 panic、不产生空行塌陷以外的噪音。
	unknown := BuildSheetPrompt("不存在的类型", SheetFacts{Name: "X", VariantName: "默认"}, "")
	if !strings.HasPrefix(unknown, "角色:X") {
		t.Fatalf("unknown type should skip the template line, got %q", unknown)
	}
}

func TestBuildSheetPromptTruncatesAt2000Runes(t *testing.T) {
	f := SheetFacts{
		Name:        strings.Repeat("满", 2000),
		Detail:      strings.Repeat("细", 2000),
		VariantName: "默认",
	}
	got := BuildSheetPrompt(SheetTypePalette, f, "")
	if n := len([]rune(got)); n > MaxSheetPromptRunes {
		t.Fatalf("prompt runes = %d, want ≤ %d", n, MaxSheetPromptRunes)
	}
}
