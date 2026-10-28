package character_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/gachal/InfiniteChance/internal/character"
	"github.com/gachal/InfiniteChance/internal/sqlitedb"
)

// newSQLiteWorld opens a throwaway desktop-shaped character store over one
// temp database(51 号票:三表空起步,无存量迁移,EnsureSchema 幂等)。
func newSQLiteWorld(t *testing.T) (*character.SQLiteStore, *sql.DB) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := character.NewSQLiteStore(db)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return store, db
}

func mustCreateCharacter(t *testing.T, store *character.SQLiteStore, ipID int64, name string) character.Character {
	t.Helper()
	c, err := store.CreateCharacter(context.Background(), ipID, character.CharacterInput{Name: name})
	if err != nil {
		t.Fatalf("create character %q: %v", name, err)
	}
	return c
}

// 建角色自动带「默认」变体(51 号票变体纪律第一条);可改名。
func TestSQLiteCreateCharacterSeedsDefaultVariant(t *testing.T) {
	store, _ := newSQLiteWorld(t)
	ctx := context.Background()

	c := mustCreateCharacter(t, store, 7, "小星")
	if c.ID == 0 || c.IPID != 7 || c.Name != "小星" {
		t.Fatalf("created = %+v", c)
	}
	if len(c.Variants) != 1 || c.Variants[0].Name != "默认" {
		t.Fatalf("variants = %+v, want exactly one named 默认", c.Variants)
	}

	got, err := store.GetCharacter(ctx, c.ID)
	if err != nil || len(got.Variants) != 1 || got.Variants[0].Name != "默认" {
		t.Fatalf("GetCharacter = %+v, %v", got, err)
	}

	// 同 IP 重名允许(51 号票口径)。
	dup := mustCreateCharacter(t, store, 7, "小星")
	if dup.ID == c.ID {
		t.Fatalf("duplicate name must create a second row")
	}
}

// 变体增删改:新建变体、改名与服装描述、最后一个不可删(ErrLastVariant)。
func TestSQLiteVariantLifecycle(t *testing.T) {
	store, _ := newSQLiteWorld(t)
	ctx := context.Background()

	c := mustCreateCharacter(t, store, 1, "小星")

	// 最后一个变体不可删:刚建的角色只有「默认」。
	if err := store.DeleteVariant(ctx, c.Variants[0].ID); err != character.ErrLastVariant {
		t.Fatalf("delete last variant = %v, want ErrLastVariant", err)
	}

	v, err := store.CreateVariant(ctx, c.ID, "校服", "藏青色水手服")
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	if v.CharacterID != c.ID || v.Name != "校服" || v.Note != "藏青色水手服" {
		t.Fatalf("variant = %+v", v)
	}

	name := "夏季校服"
	note := "白色短袖"
	updated, err := store.UpdateVariant(ctx, v.ID, &character.VariantUpdate{Name: &name, Note: &note})
	if err != nil || updated.Name != name || updated.Note != note {
		t.Fatalf("update variant = %+v, %v", updated, err)
	}

	// 删非最后变体:其图行随级联消失(先落一张图再删)。
	img, err := store.CreateImage(ctx, c.ID, character.ImageInput{VariantID: v.ID, AssetID: 42})
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if err := store.DeleteVariant(ctx, v.ID); err != nil {
		t.Fatalf("delete non-last variant: %v", err)
	}
	if _, err := store.GetImage(ctx, img.ID); err != character.ErrImageNotFound {
		t.Fatalf("image after variant delete = %v, want ErrImageNotFound", err)
	}
	after, err := store.GetCharacter(ctx, c.ID)
	if err != nil || len(after.Variants) != 1 {
		t.Fatalf("variants after delete = %+v, %v", after.Variants, err)
	}
}

// 图 CRUD:label/sort 落列、换变体必须同角色、删除只动图行。
func TestSQLiteImageLifecycle(t *testing.T) {
	store, _ := newSQLiteWorld(t)
	ctx := context.Background()

	c := mustCreateCharacter(t, store, 1, "小星")
	def := c.Variants[0]
	v2, err := store.CreateVariant(ctx, c.ID, "战斗形态", "")
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}

	img, err := store.CreateImage(ctx, c.ID, character.ImageInput{VariantID: def.ID, AssetID: 100, Label: "三视图"})
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if img.CharacterID != c.ID || img.VariantID != def.ID || img.AssetID != 100 || img.Label != "三视图" || img.Sort != 0 {
		t.Fatalf("image = %+v", img)
	}

	// 换变体:同角色允许。
	moved, err := store.UpdateImage(ctx, img.ID, &character.ImageUpdate{VariantID: &v2.ID})
	if err != nil || moved.VariantID != v2.ID {
		t.Fatalf("move image = %+v, %v", moved, err)
	}
	// 换变体:跨角色拒绝(ErrVariantMismatch)。
	other := mustCreateCharacter(t, store, 1, "阿月")
	if _, err := store.UpdateImage(ctx, img.ID, &character.ImageUpdate{VariantID: &other.Variants[0].ID}); err != character.ErrVariantMismatch {
		t.Fatalf("cross-character move = %v, want ErrVariantMismatch", err)
	}
	// label 与 sort 部分更新。
	sort := 3
	labeled, err := store.UpdateImage(ctx, img.ID, &character.ImageUpdate{Label: ptr("色板"), Sort: &sort})
	if err != nil || labeled.Label != "色板" || labeled.Sort != 3 || labeled.VariantID != v2.ID {
		t.Fatalf("label/sort update = %+v, %v", labeled, err)
	}

	if err := store.DeleteImage(ctx, img.ID); err != nil {
		t.Fatalf("delete image: %v", err)
	}
	if _, err := store.GetImage(ctx, img.ID); err != character.ErrImageNotFound {
		t.Fatalf("get after delete = %v, want ErrImageNotFound", err)
	}
}

// 列表树:角色 updated_at DESC、变体与图按 sort+id;限本 IP;封面 = 首个
// 变体排序最前的视图类图,无则任意第一张。
func TestSQLiteListCharactersTree(t *testing.T) {
	store, _ := newSQLiteWorld(t)
	ctx := context.Background()

	c := mustCreateCharacter(t, store, 5, "小星")
	def := c.Variants[0]
	v2, err := store.CreateVariant(ctx, c.ID, "战斗形态", "")
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}

	// 变体内图序:sort 优先。给默认变体落「非视图」(sort=0)与「多视图」
	// (sort=1),封面应取视图类;战斗形态不落图。
	if _, err := store.CreateImage(ctx, c.ID, character.ImageInput{VariantID: def.ID, AssetID: 11, Label: "表情集"}); err != nil {
		t.Fatalf("seed image: %v", err)
	}
	if _, err := store.CreateImage(ctx, c.ID, character.ImageInput{VariantID: def.ID, AssetID: 12, Label: "多视图"}); err != nil {
		t.Fatalf("seed image: %v", err)
	}
	// 第二变体落一张低 sort 的三视图:封面规则只看首个变体,不应翻转。
	if _, err := store.CreateImage(ctx, c.ID, character.ImageInput{VariantID: v2.ID, AssetID: 13, Label: "三视图"}); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	list, err := store.ListCharacters(ctx, 5)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	got := list[0]
	if got.CoverAssetID != 12 {
		t.Fatalf("cover_asset_id = %d, want 12(首变体的视图类图优先)", got.CoverAssetID)
	}
	if len(got.Variants) != 2 || got.Variants[0].ID != def.ID || got.Variants[1].ID != v2.ID {
		t.Fatalf("variant order = %+v, want creation order (sort 相同按 id)", got.Variants)
	}
	images := got.Variants[0].Images
	if len(images) != 2 || images[0].AssetID != 11 || images[1].AssetID != 12 {
		t.Fatalf("image order = %+v, want sort+id order", images)
	}

	// 限本 IP:别的 IP 看不见。
	otherList, err := store.ListCharacters(ctx, 6)
	if err != nil || len(otherList) != 0 {
		t.Fatalf("cross-ip list = %+v, %v", otherList, err)
	}
}

// 封面回落:首变体无视图类图时取第一张;全空时 0。
func TestSQLiteCoverFallback(t *testing.T) {
	store, _ := newSQLiteWorld(t)
	ctx := context.Background()

	c := mustCreateCharacter(t, store, 1, "无图角色")
	list, err := store.ListCharacters(ctx, 1)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[0].CoverAssetID != 0 {
		t.Fatalf("cover of empty character = %d, want 0", list[0].CoverAssetID)
	}

	def := c.Variants[0]
	if _, err := store.CreateImage(ctx, c.ID, character.ImageInput{VariantID: def.ID, AssetID: 9, Label: "姿态"}); err != nil {
		t.Fatalf("seed image: %v", err)
	}
	list, err = store.ListCharacters(ctx, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list[0].CoverAssetID != 9 {
		t.Fatalf("cover without view image = %d, want first image 9", list[0].CoverAssetID)
	}
}

// 角色更新与级联删除:PATCH 部分字段;删角色清变体与图行(素材行不动是
// asset 域的事实,本库断言图行/变体行清零即可)。
func TestSQLiteCharacterUpdateAndCascadeDelete(t *testing.T) {
	store, db := newSQLiteWorld(t)
	ctx := context.Background()

	c := mustCreateCharacter(t, store, 2, "小星")
	alias := "星酱"
	desc := "一句话简介"
	detail := "详细设定文本"
	updated, err := store.UpdateCharacter(ctx, c.ID, &character.CharacterUpdate{
		Alias: &alias, Description: &desc, Detail: &detail,
	})
	if err != nil || updated.Alias != alias || updated.Description != desc || updated.Detail != detail || updated.Name != "小星" {
		t.Fatalf("update character = %+v, %v", updated, err)
	}

	if _, err := store.CreateImage(ctx, c.ID, character.ImageInput{VariantID: c.Variants[0].ID, AssetID: 1}); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	if err := store.DeleteCharacter(ctx, c.ID); err != nil {
		t.Fatalf("delete character: %v", err)
	}
	if _, err := store.GetCharacter(ctx, c.ID); err != character.ErrCharacterNotFound {
		t.Fatalf("get after delete = %v, want ErrCharacterNotFound", err)
	}
	var variantCount, imageCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM character_variants`).Scan(&variantCount); err != nil || variantCount != 0 {
		t.Fatalf("variant rows after cascade = %d, %v", variantCount, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM character_images`).Scan(&imageCount); err != nil || imageCount != 0 {
		t.Fatalf("image rows after cascade = %d, %v", imageCount, err)
	}
}

// EnsureSchema 幂等:重复执行不报错、不重建(空起步无迁移,51 号票)。
func TestSQLiteEnsureSchemaIdempotent(t *testing.T) {
	store, _ := newSQLiteWorld(t)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

// 变体与图操作对不存在实体答哨兵错误。
func TestSQLiteMissingSentinels(t *testing.T) {
	store, _ := newSQLiteWorld(t)
	ctx := context.Background()

	if _, err := store.GetCharacter(ctx, 99); err != character.ErrCharacterNotFound {
		t.Fatalf("missing character = %v, want ErrCharacterNotFound", err)
	}
	if _, err := store.CreateVariant(ctx, 99, "x", ""); err != character.ErrCharacterNotFound {
		t.Fatalf("variant under missing character = %v, want ErrCharacterNotFound", err)
	}
	if _, err := store.CreateImage(ctx, 99, character.ImageInput{VariantID: 1, AssetID: 1}); err != character.ErrCharacterNotFound {
		t.Fatalf("image under missing character = %v, want ErrCharacterNotFound", err)
	}
	if _, err := store.UpdateVariant(ctx, 99, &character.VariantUpdate{}); err != character.ErrVariantNotFound {
		t.Fatalf("missing variant = %v, want ErrVariantNotFound", err)
	}
	if err := store.DeleteVariant(ctx, 99); err != character.ErrVariantNotFound {
		t.Fatalf("delete missing variant = %v, want ErrVariantNotFound", err)
	}
	if _, err := store.UpdateImage(ctx, 99, &character.ImageUpdate{}); err != character.ErrImageNotFound {
		t.Fatalf("missing image = %v, want ErrImageNotFound", err)
	}
	if err := store.DeleteImage(ctx, 99); err != character.ErrImageNotFound {
		t.Fatalf("delete missing image = %v, want ErrImageNotFound", err)
	}
	// 跨角色的变体建图:variant_id 不属于该角色。
	c := mustCreateCharacter(t, store, 1, "小星")
	other := mustCreateCharacter(t, store, 1, "阿月")
	if _, err := store.CreateImage(ctx, c.ID, character.ImageInput{VariantID: other.Variants[0].ID, AssetID: 1}); err != character.ErrVariantMismatch {
		t.Fatalf("cross-character create = %v, want ErrVariantMismatch", err)
	}
}

// 多角色列表的树完整性:12 个角色各带一张设定图,断言每个角色都拿到自己
// 的变体与图(部署事故回归 —— byID 曾在 append 扩容后指向旧底层数组,
// 先收入的角色整棵树丢失,单角色列表测不出来)。
func TestSQLiteListCharactersMultiRowTree(t *testing.T) {
	store, _ := newSQLiteWorld(t)
	ctx := context.Background()

	const n = 12
	want := map[int64]int64{} // character id → asset id
	for i := 0; i < n; i++ {
		c := mustCreateCharacter(t, store, 3, fmt.Sprintf("角色%02d", i))
		asset := int64(100 + i)
		if _, err := store.CreateImage(ctx, c.ID, character.ImageInput{
			VariantID: c.Variants[0].ID, AssetID: asset, Label: "三视图",
		}); err != nil {
			t.Fatalf("seed image for %q: %v", c.Name, err)
		}
		want[c.ID] = asset
	}

	list, err := store.ListCharacters(ctx, 3)
	if err != nil || len(list) != n {
		t.Fatalf("list len = %d, %v, want %d", len(list), err, n)
	}
	for _, got := range list {
		asset, ok := want[got.ID]
		if !ok {
			t.Fatalf("unexpected character id %d in list", got.ID)
		}
		if len(got.Variants) != 1 || len(got.Variants[0].Images) != 1 {
			t.Fatalf("character %d tree = %+v, want 1 variant with 1 image(指针失效丢树)", got.ID, got.Variants)
		}
		if got.Variants[0].Images[0].AssetID != asset {
			t.Fatalf("character %d image = %d, want %d", got.ID, got.Variants[0].Images[0].AssetID, asset)
		}
		if got.CoverAssetID != asset {
			t.Fatalf("character %d cover = %d, want %d", got.ID, got.CoverAssetID, asset)
		}
	}
}

func ptr[T any](v T) *T { return &v }
