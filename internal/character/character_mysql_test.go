package character_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/character"
)

// openTestStore 连 compose MySQL(宿主机 3307)上的专用一次性库,与
// ip_mysql_test.go 同款 harness:库不可达自动 skip,保持 go test 全绿。
// 51 号票三表空起步,无同序依赖(character 表自含,不 join 其他包的表)。
func openTestStore(t *testing.T) (*character.MySQLStore, *sql.DB) {
	t.Helper()

	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = "root:infinitechance@tcp(localhost:3307)/infinitechance_test?parseTime=true"
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MYSQL_TEST_DSN: %v", err)
	}
	dbName := cfg.DBName + "_character"
	cfg.DBName = ""

	server, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open mysql server: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.PingContext(ctx); err != nil {
		t.Skipf("mysql unreachable, skipping store tests: %v", err)
	}
	if _, err := server.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `"+dbName+"`"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	for _, table := range []string{"characters", "character_variants", "character_images"} {
		if _, err := server.ExecContext(ctx, "DROP TABLE IF EXISTS `"+dbName+"`.`"+table+"`"); err != nil {
			t.Fatalf("drop stale table %s: %v", table, err)
		}
	}

	cfg.DBName = dbName
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	store := character.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("character EnsureSchema: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store, db
}

func mustCreateMySQLCharacter(t *testing.T, store *character.MySQLStore, ipID int64, name string) character.Character {
	t.Helper()
	c, err := store.CreateCharacter(context.Background(), ipID, character.CharacterInput{Name: name})
	if err != nil {
		t.Fatalf("create character %q: %v", name, err)
	}
	return c
}

// 建角色自动带「默认」变体;同 IP 重名允许。
func TestMySQLCreateCharacterSeedsDefaultVariant(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	c := mustCreateMySQLCharacter(t, store, 7, "小星")
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
	dup := mustCreateMySQLCharacter(t, store, 7, "小星")
	if dup.ID == c.ID {
		t.Fatalf("duplicate name must create a second row")
	}
}

// 变体增删改 + 最后一个不可删 + 删变体级联其图行。
func TestMySQLVariantLifecycle(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	c := mustCreateMySQLCharacter(t, store, 1, "小星")
	if err := store.DeleteVariant(ctx, c.Variants[0].ID); err != character.ErrLastVariant {
		t.Fatalf("delete last variant = %v, want ErrLastVariant", err)
	}

	v, err := store.CreateVariant(ctx, c.ID, "校服", "藏青色水手服")
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	name := "夏季校服"
	note := "白色短袖"
	updated, err := store.UpdateVariant(ctx, v.ID, &character.VariantUpdate{Name: &name, Note: &note})
	if err != nil || updated.Name != name || updated.Note != note {
		t.Fatalf("update variant = %+v, %v", updated, err)
	}

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
func TestMySQLImageLifecycle(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	c := mustCreateMySQLCharacter(t, store, 1, "小星")
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

	moved, err := store.UpdateImage(ctx, img.ID, &character.ImageUpdate{VariantID: &v2.ID})
	if err != nil || moved.VariantID != v2.ID {
		t.Fatalf("move image = %+v, %v", moved, err)
	}
	other := mustCreateMySQLCharacter(t, store, 1, "阿月")
	if _, err := store.UpdateImage(ctx, img.ID, &character.ImageUpdate{VariantID: &other.Variants[0].ID}); err != character.ErrVariantMismatch {
		t.Fatalf("cross-character move = %v, want ErrVariantMismatch", err)
	}
	sort := 3
	labeled, err := store.UpdateImage(ctx, img.ID, &character.ImageUpdate{Label: mysqlPtr("色板"), Sort: &sort})
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

// 列表树:角色 updated_at DESC、变体与图按 sort+id;限本 IP;封面取首个
// 变体的视图类图。
func TestMySQLListCharactersTree(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	c := mustCreateMySQLCharacter(t, store, 5, "小星")
	def := c.Variants[0]
	v2, err := store.CreateVariant(ctx, c.ID, "战斗形态", "")
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	if _, err := store.CreateImage(ctx, c.ID, character.ImageInput{VariantID: def.ID, AssetID: 11, Label: "表情集"}); err != nil {
		t.Fatalf("seed image: %v", err)
	}
	if _, err := store.CreateImage(ctx, c.ID, character.ImageInput{VariantID: def.ID, AssetID: 12, Label: "多视图"}); err != nil {
		t.Fatalf("seed image: %v", err)
	}
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

	otherList, err := store.ListCharacters(ctx, 6)
	if err != nil || len(otherList) != 0 {
		t.Fatalf("cross-ip list = %+v, %v", otherList, err)
	}
}

// 封面回落:无图 0;首变体无视图类图取第一张。
func TestMySQLCoverFallback(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	c := mustCreateMySQLCharacter(t, store, 1, "无图角色")
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

// 角色更新与级联删除。
func TestMySQLCharacterUpdateAndCascadeDelete(t *testing.T) {
	store, db := openTestStore(t)
	ctx := context.Background()

	c := mustCreateMySQLCharacter(t, store, 2, "小星")
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

// EnsureSchema 幂等。
func TestMySQLEnsureSchemaIdempotent(t *testing.T) {
	store, _ := openTestStore(t)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

// 缺失实体哨兵 + 跨角色变体守卫。
func TestMySQLMissingSentinels(t *testing.T) {
	store, _ := openTestStore(t)
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
	c := mustCreateMySQLCharacter(t, store, 1, "小星")
	other := mustCreateMySQLCharacter(t, store, 1, "阿月")
	if _, err := store.CreateImage(ctx, c.ID, character.ImageInput{VariantID: other.Variants[0].ID, AssetID: 1}); err != character.ErrVariantMismatch {
		t.Fatalf("cross-character create = %v, want ErrVariantMismatch", err)
	}
}

// 多角色列表的树完整性:12 个角色各带一张设定图,断言每个角色都拿到自己
// 的变体与图(部署事故回归 —— byID 曾在 append 扩容后指向旧底层数组,
// 先收入的角色整棵树丢失,单角色列表测不出来)。
func TestMySQLListCharactersMultiRowTree(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	const n = 12
	want := map[int64]int64{} // character id → asset id
	for i := 0; i < n; i++ {
		c := mustCreateMySQLCharacter(t, store, 3, fmt.Sprintf("角色%02d", i))
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

func mysqlPtr[T any](v T) *T { return &v }
