package scene_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/scene"
)

// openTestStore 连 compose MySQL(宿主机 3307)上的专用一次性库,与
// character_mysql_test.go 同款 harness:库不可达自动 skip,保持 go test
// 全绿。65 号票两表空起步,无同序依赖(scenes 表自含,不 join 其他包的表)。
func openTestStore(t *testing.T) (*scene.MySQLStore, *sql.DB) {
	t.Helper()

	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = "root:infinitechance@tcp(localhost:3307)/infinitechance_test?parseTime=true"
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MYSQL_TEST_DSN: %v", err)
	}
	dbName := cfg.DBName + "_scene"
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
	for _, table := range []string{"scenes", "scene_images"} {
		if _, err := server.ExecContext(ctx, "DROP TABLE IF EXISTS `"+dbName+"`.`"+table+"`"); err != nil {
			t.Fatalf("drop stale table %s: %v", table, err)
		}
	}

	cfg.DBName = dbName
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	store := scene.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("scene EnsureSchema: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store, db
}

func mustCreateMySQLScene(t *testing.T, store *scene.MySQLStore, ipID int64, name string) scene.Scene {
	t.Helper()
	s, err := store.CreateScene(context.Background(), ipID, scene.SceneInput{Name: name})
	if err != nil {
		t.Fatalf("create scene %q: %v", name, err)
	}
	return s
}

// 建场景只有文本字段(无变体);同 IP 重名允许。
func TestMySQLCreateScene(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	s, err := store.CreateScene(ctx, 7, scene.SceneInput{
		Name: "老宅院", Alias: "陈家大院", Description: "一句话简介", Detail: "详细设定文本",
	})
	if err != nil {
		t.Fatalf("create scene: %v", err)
	}
	if s.ID == 0 || s.IPID != 7 || s.Name != "老宅院" || s.Alias != "陈家大院" ||
		s.Description != "一句话简介" || s.Detail != "详细设定文本" {
		t.Fatalf("created = %+v", s)
	}
	if s.CoverAssetID != 0 || len(s.Images) != 0 {
		t.Fatalf("new scene must be imageless, got %+v", s)
	}
	got, err := store.GetScene(ctx, s.ID)
	if err != nil || got.Name != "老宅院" || got.Detail != "详细设定文本" {
		t.Fatalf("GetScene = %+v, %v", got, err)
	}
	dup := mustCreateMySQLScene(t, store, 7, "老宅院")
	if dup.ID == s.ID {
		t.Fatalf("duplicate name must create a second row")
	}
}

// 图 CRUD:label/sort 落列、删除只动图行、挂缺失场景答 ErrSceneNotFound。
func TestMySQLImageLifecycle(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	s := mustCreateMySQLScene(t, store, 1, "老宅院")

	img, err := store.CreateImage(ctx, s.ID, scene.ImageInput{AssetID: 100, Label: "全景"})
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if img.SceneID != s.ID || img.AssetID != 100 || img.Label != "全景" || img.Sort != 0 {
		t.Fatalf("image = %+v", img)
	}

	sort := 3
	labeled, err := store.UpdateImage(ctx, img.ID, &scene.ImageUpdate{Label: mysqlPtr("夜景"), Sort: &sort})
	if err != nil || labeled.Label != "夜景" || labeled.Sort != 3 {
		t.Fatalf("label/sort update = %+v, %v", labeled, err)
	}

	if err := store.DeleteImage(ctx, img.ID); err != nil {
		t.Fatalf("delete image: %v", err)
	}
	if _, err := store.GetImage(ctx, img.ID); err != scene.ErrImageNotFound {
		t.Fatalf("get after delete = %v, want ErrImageNotFound", err)
	}

	if _, err := store.CreateImage(ctx, 99, scene.ImageInput{AssetID: 1}); err != scene.ErrSceneNotFound {
		t.Fatalf("image under missing scene = %v, want ErrSceneNotFound", err)
	}
}

// 列表树:场景 updated_at DESC、图按 sort+id;限本 IP;封面取「全景」图优先。
func TestMySQLListScenesTree(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	s := mustCreateMySQLScene(t, store, 5, "老宅院")
	if _, err := store.CreateImage(ctx, s.ID, scene.ImageInput{AssetID: 11, Label: "局部"}); err != nil {
		t.Fatalf("seed image: %v", err)
	}
	if _, err := store.CreateImage(ctx, s.ID, scene.ImageInput{AssetID: 12, Label: "全景"}); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	list, err := store.ListScenes(ctx, 5)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	got := list[0]
	if got.CoverAssetID != 12 {
		t.Fatalf("cover_asset_id = %d, want 12(全景优先)", got.CoverAssetID)
	}
	if len(got.Images) != 2 || got.Images[0].AssetID != 11 || got.Images[1].AssetID != 12 {
		t.Fatalf("image order = %+v, want sort+id order", got.Images)
	}

	otherList, err := store.ListScenes(ctx, 6)
	if err != nil || len(otherList) != 0 {
		t.Fatalf("cross-ip list = %+v, %v", otherList, err)
	}
}

// 封面回落:无图 0;无「全景」label 取第一张。
func TestMySQLCoverFallback(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	s := mustCreateMySQLScene(t, store, 1, "无图场景")
	list, err := store.ListScenes(ctx, 1)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[0].CoverAssetID != 0 {
		t.Fatalf("cover of empty scene = %d, want 0", list[0].CoverAssetID)
	}
	if _, err := store.CreateImage(ctx, s.ID, scene.ImageInput{AssetID: 9, Label: "日景"}); err != nil {
		t.Fatalf("seed image: %v", err)
	}
	list, err = store.ListScenes(ctx, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list[0].CoverAssetID != 9 {
		t.Fatalf("cover without panorama = %d, want first image 9", list[0].CoverAssetID)
	}
}

// 场景更新与级联删除。
func TestMySQLSceneUpdateAndCascadeDelete(t *testing.T) {
	store, db := openTestStore(t)
	ctx := context.Background()

	s := mustCreateMySQLScene(t, store, 2, "老宅院")
	alias := "陈家大院"
	desc := "一句话简介"
	detail := "详细设定文本"
	updated, err := store.UpdateScene(ctx, s.ID, &scene.SceneUpdate{
		Alias: &alias, Description: &desc, Detail: &detail,
	})
	if err != nil || updated.Alias != alias || updated.Description != desc || updated.Detail != detail || updated.Name != "老宅院" {
		t.Fatalf("update scene = %+v, %v", updated, err)
	}
	if _, err := store.CreateImage(ctx, s.ID, scene.ImageInput{AssetID: 1}); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	if err := store.DeleteScene(ctx, s.ID); err != nil {
		t.Fatalf("delete scene: %v", err)
	}
	if _, err := store.GetScene(ctx, s.ID); err != scene.ErrSceneNotFound {
		t.Fatalf("get after delete = %v, want ErrSceneNotFound", err)
	}
	var imageCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM scene_images`).Scan(&imageCount); err != nil || imageCount != 0 {
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

// 缺失实体哨兵。
func TestMySQLMissingSentinels(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	if _, err := store.GetScene(ctx, 99); err != scene.ErrSceneNotFound {
		t.Fatalf("missing scene = %v, want ErrSceneNotFound", err)
	}
	if _, err := store.UpdateScene(ctx, 99, &scene.SceneUpdate{}); err != scene.ErrSceneNotFound {
		t.Fatalf("missing scene update = %v, want ErrSceneNotFound", err)
	}
	if err := store.DeleteScene(ctx, 99); err != scene.ErrSceneNotFound {
		t.Fatalf("delete missing scene = %v, want ErrSceneNotFound", err)
	}
	if _, err := store.UpdateImage(ctx, 99, &scene.ImageUpdate{}); err != scene.ErrImageNotFound {
		t.Fatalf("missing image = %v, want ErrImageNotFound", err)
	}
	if err := store.DeleteImage(ctx, 99); err != scene.ErrImageNotFound {
		t.Fatalf("delete missing image = %v, want ErrImageNotFound", err)
	}
}

// 多场景列表的树完整性:12 个场景各带一张设定图(指针失效丢树回归)。
func TestMySQLListScenesMultiRowTree(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	const n = 12
	want := map[int64]int64{} // scene id → asset id
	for i := 0; i < n; i++ {
		s := mustCreateMySQLScene(t, store, 3, fmt.Sprintf("场景%02d", i))
		asset := int64(100 + i)
		if _, err := store.CreateImage(ctx, s.ID, scene.ImageInput{AssetID: asset, Label: "全景"}); err != nil {
			t.Fatalf("seed image for %q: %v", s.Name, err)
		}
		want[s.ID] = asset
	}

	list, err := store.ListScenes(ctx, 3)
	if err != nil || len(list) != n {
		t.Fatalf("list len = %d, %v, want %d", len(list), err, n)
	}
	for _, got := range list {
		asset, ok := want[got.ID]
		if !ok {
			t.Fatalf("unexpected scene id %d in list", got.ID)
		}
		if len(got.Images) != 1 {
			t.Fatalf("scene %d images = %+v, want 1(指针失效丢树)", got.ID, got.Images)
		}
		if got.Images[0].AssetID != asset {
			t.Fatalf("scene %d image = %d, want %d", got.ID, got.Images[0].AssetID, asset)
		}
		if got.CoverAssetID != asset {
			t.Fatalf("scene %d cover = %d, want %d", got.ID, got.CoverAssetID, asset)
		}
	}
}

func mysqlPtr[T any](v T) *T { return &v }
