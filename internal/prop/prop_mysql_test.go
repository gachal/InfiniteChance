package prop_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/prop"
)

// openTestStore 连 compose MySQL(宿主机 3307)上的专用一次性库,与
// scene_mysql_test.go 同款 harness:库不可达自动 skip,保持 go test
// 全绿。65 号票两表空起步,无同序依赖(props 表自含,不 join 其他包的表)。
func openTestStore(t *testing.T) (*prop.MySQLStore, *sql.DB) {
	t.Helper()

	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = "root:infinitechance@tcp(localhost:3307)/infinitechance_test?parseTime=true"
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MYSQL_TEST_DSN: %v", err)
	}
	dbName := cfg.DBName + "_prop"
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
	for _, table := range []string{"props", "prop_images"} {
		if _, err := server.ExecContext(ctx, "DROP TABLE IF EXISTS `"+dbName+"`.`"+table+"`"); err != nil {
			t.Fatalf("drop stale table %s: %v", table, err)
		}
	}

	cfg.DBName = dbName
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	store := prop.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("prop EnsureSchema: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store, db
}

func mustCreateMySQLProp(t *testing.T, store *prop.MySQLStore, ipID int64, name string) prop.Prop {
	t.Helper()
	p, err := store.CreateProp(context.Background(), ipID, prop.PropInput{Name: name})
	if err != nil {
		t.Fatalf("create prop %q: %v", name, err)
	}
	return p
}

// 建道具只有文本字段(无变体);同 IP 重名允许。
func TestMySQLCreateProp(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	p, err := store.CreateProp(ctx, 7, prop.PropInput{
		Name: "怀表", Alias: "祖传怀表", Description: "一句话简介", Detail: "详细设定文本",
	})
	if err != nil {
		t.Fatalf("create prop: %v", err)
	}
	if p.ID == 0 || p.IPID != 7 || p.Name != "怀表" || p.Alias != "祖传怀表" ||
		p.Description != "一句话简介" || p.Detail != "详细设定文本" {
		t.Fatalf("created = %+v", p)
	}
	if p.CoverAssetID != 0 || len(p.Images) != 0 {
		t.Fatalf("new prop must be imageless, got %+v", p)
	}
	got, err := store.GetProp(ctx, p.ID)
	if err != nil || got.Name != "怀表" || got.Detail != "详细设定文本" {
		t.Fatalf("GetProp = %+v, %v", got, err)
	}
	dup := mustCreateMySQLProp(t, store, 7, "怀表")
	if dup.ID == p.ID {
		t.Fatalf("duplicate name must create a second row")
	}
}

// 图 CRUD:label/sort 落列、删除只动图行、挂缺失道具答 ErrPropNotFound。
func TestMySQLImageLifecycle(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	p := mustCreateMySQLProp(t, store, 1, "怀表")

	img, err := store.CreateImage(ctx, p.ID, prop.ImageInput{AssetID: 100, Label: "多视图"})
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if img.PropID != p.ID || img.AssetID != 100 || img.Label != "多视图" || img.Sort != 0 {
		t.Fatalf("image = %+v", img)
	}

	sort := 3
	labeled, err := store.UpdateImage(ctx, img.ID, &prop.ImageUpdate{Label: mysqlPtr("使用态"), Sort: &sort})
	if err != nil || labeled.Label != "使用态" || labeled.Sort != 3 {
		t.Fatalf("label/sort update = %+v, %v", labeled, err)
	}

	if err := store.DeleteImage(ctx, img.ID); err != nil {
		t.Fatalf("delete image: %v", err)
	}
	if _, err := store.GetImage(ctx, img.ID); err != prop.ErrImageNotFound {
		t.Fatalf("get after delete = %v, want ErrImageNotFound", err)
	}

	if _, err := store.CreateImage(ctx, 99, prop.ImageInput{AssetID: 1}); err != prop.ErrPropNotFound {
		t.Fatalf("image under missing prop = %v, want ErrPropNotFound", err)
	}
}

// 列表树:道具 updated_at DESC、图按 sort+id;限本 IP;封面取「多视图」图优先。
func TestMySQLListPropsTree(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	p := mustCreateMySQLProp(t, store, 5, "怀表")
	if _, err := store.CreateImage(ctx, p.ID, prop.ImageInput{AssetID: 11, Label: "细节"}); err != nil {
		t.Fatalf("seed image: %v", err)
	}
	if _, err := store.CreateImage(ctx, p.ID, prop.ImageInput{AssetID: 12, Label: "多视图"}); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	list, err := store.ListProps(ctx, 5)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	got := list[0]
	if got.CoverAssetID != 12 {
		t.Fatalf("cover_asset_id = %d, want 12(多视图优先)", got.CoverAssetID)
	}
	if len(got.Images) != 2 || got.Images[0].AssetID != 11 || got.Images[1].AssetID != 12 {
		t.Fatalf("image order = %+v, want sort+id order", got.Images)
	}

	otherList, err := store.ListProps(ctx, 6)
	if err != nil || len(otherList) != 0 {
		t.Fatalf("cross-ip list = %+v, %v", otherList, err)
	}
}

// 封面回落:无图 0;无「多视图」label 取第一张。
func TestMySQLCoverFallback(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	p := mustCreateMySQLProp(t, store, 1, "无图道具")
	list, err := store.ListProps(ctx, 1)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[0].CoverAssetID != 0 {
		t.Fatalf("cover of empty prop = %d, want 0", list[0].CoverAssetID)
	}
	if _, err := store.CreateImage(ctx, p.ID, prop.ImageInput{AssetID: 9, Label: "细节"}); err != nil {
		t.Fatalf("seed image: %v", err)
	}
	list, err = store.ListProps(ctx, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list[0].CoverAssetID != 9 {
		t.Fatalf("cover without multi-view = %d, want first image 9", list[0].CoverAssetID)
	}
}

// 道具更新与级联删除。
func TestMySQLPropUpdateAndCascadeDelete(t *testing.T) {
	store, db := openTestStore(t)
	ctx := context.Background()

	p := mustCreateMySQLProp(t, store, 2, "怀表")
	alias := "祖传怀表"
	desc := "一句话简介"
	detail := "详细设定文本"
	updated, err := store.UpdateProp(ctx, p.ID, &prop.PropUpdate{
		Alias: &alias, Description: &desc, Detail: &detail,
	})
	if err != nil || updated.Alias != alias || updated.Description != desc || updated.Detail != detail || updated.Name != "怀表" {
		t.Fatalf("update prop = %+v, %v", updated, err)
	}
	if _, err := store.CreateImage(ctx, p.ID, prop.ImageInput{AssetID: 1}); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	if err := store.DeleteProp(ctx, p.ID); err != nil {
		t.Fatalf("delete prop: %v", err)
	}
	if _, err := store.GetProp(ctx, p.ID); err != prop.ErrPropNotFound {
		t.Fatalf("get after delete = %v, want ErrPropNotFound", err)
	}
	var imageCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM prop_images`).Scan(&imageCount); err != nil || imageCount != 0 {
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

	if _, err := store.GetProp(ctx, 99); err != prop.ErrPropNotFound {
		t.Fatalf("missing prop = %v, want ErrPropNotFound", err)
	}
	if _, err := store.UpdateProp(ctx, 99, &prop.PropUpdate{}); err != prop.ErrPropNotFound {
		t.Fatalf("missing prop update = %v, want ErrPropNotFound", err)
	}
	if err := store.DeleteProp(ctx, 99); err != prop.ErrPropNotFound {
		t.Fatalf("delete missing prop = %v, want ErrPropNotFound", err)
	}
	if _, err := store.UpdateImage(ctx, 99, &prop.ImageUpdate{}); err != prop.ErrImageNotFound {
		t.Fatalf("missing image = %v, want ErrImageNotFound", err)
	}
	if err := store.DeleteImage(ctx, 99); err != prop.ErrImageNotFound {
		t.Fatalf("delete missing image = %v, want ErrImageNotFound", err)
	}
}

// 多道具列表的树完整性:12 个道具各带一张设定图(指针失效丢树回归)。
func TestMySQLListPropsMultiRowTree(t *testing.T) {
	store, _ := openTestStore(t)
	ctx := context.Background()

	const n = 12
	want := map[int64]int64{} // prop id → asset id
	for i := 0; i < n; i++ {
		p := mustCreateMySQLProp(t, store, 3, fmt.Sprintf("道具%02d", i))
		asset := int64(100 + i)
		if _, err := store.CreateImage(ctx, p.ID, prop.ImageInput{AssetID: asset, Label: "多视图"}); err != nil {
			t.Fatalf("seed image for %q: %v", p.Name, err)
		}
		want[p.ID] = asset
	}

	list, err := store.ListProps(ctx, 3)
	if err != nil || len(list) != n {
		t.Fatalf("list len = %d, %v, want %d", len(list), err, n)
	}
	for _, got := range list {
		asset, ok := want[got.ID]
		if !ok {
			t.Fatalf("unexpected prop id %d in list", got.ID)
		}
		if len(got.Images) != 1 {
			t.Fatalf("prop %d images = %+v, want 1(指针失效丢树)", got.ID, got.Images)
		}
		if got.Images[0].AssetID != asset {
			t.Fatalf("prop %d image = %d, want %d", got.ID, got.Images[0].AssetID, asset)
		}
		if got.CoverAssetID != asset {
			t.Fatalf("prop %d cover = %d, want %d", got.ID, got.CoverAssetID, asset)
		}
	}
}

func mysqlPtr[T any](v T) *T { return &v }
