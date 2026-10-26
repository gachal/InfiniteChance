package voice_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/gachal/InfiniteChance/internal/character"
	"github.com/gachal/InfiniteChance/internal/voice"
)

// openTestStore 连 compose MySQL(宿主机 3307)上的专用一次性库,与
// character_mysql_test.go 同款 harness:库不可达自动 skip。voices 的被引
// 检查跨读 characters 表,两表都要就位。
func openTestStore(t *testing.T) (*voice.MySQLStore, *character.MySQLStore, *sql.DB) {
	t.Helper()

	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = "root:infinitechance@tcp(localhost:3307)/infinitechance_test?parseTime=true"
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MYSQL_TEST_DSN: %v", err)
	}
	dbName := cfg.DBName + "_voice"
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
	for _, table := range []string{"characters", "character_variants", "character_images", "voices"} {
		if _, err := server.ExecContext(ctx, "DROP TABLE IF EXISTS `"+dbName+"`.`"+table+"`"); err != nil {
			t.Fatalf("drop stale table %s: %v", table, err)
		}
	}

	cfg.DBName = dbName
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := character.NewMySQLStore(db).EnsureSchema(ctx); err != nil {
		t.Fatalf("character EnsureSchema: %v", err)
	}
	store := voice.NewMySQLStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("voice EnsureSchema: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		server.Close()
	})
	return store, character.NewMySQLStore(db), db
}

func mustCreateMySQLVoice(t *testing.T, store *voice.MySQLStore, voiceID, name string) voice.Voice {
	t.Helper()
	v, err := store.Create(context.Background(), voice.CreateInput{
		VoiceID: voiceID, ChannelID: 3, TargetModel: "qwen-tts-plus",
		Name: name, SourceMode: voice.SourceModeDesign,
		SourcePrompt: "低沉磁性男声", Status: voice.StatusOK,
	})
	if err != nil {
		t.Fatalf("create voice %q: %v", name, err)
	}
	return v
}

func TestMySQLVoiceRoundTrip(t *testing.T) {
	store, _, _ := openTestStore(t)
	ctx := context.Background()

	v := mustCreateMySQLVoice(t, store, "qwen-tts-plus-vd-abcd1234-xxxx", "少年音")
	if v.ID == 0 || v.ChannelID != 3 || v.TargetModel != "qwen-tts-plus" || v.Status != voice.StatusOK {
		t.Fatalf("created = %+v", v)
	}
	if v.SourceMode != voice.SourceModeDesign || v.SourcePrompt != "低沉磁性男声" {
		t.Fatalf("source facts = %+v", v)
	}
	got, err := store.Get(ctx, v.ID)
	if err != nil || got.VoiceID != v.VoiceID || got.Name != "少年音" {
		t.Fatalf("Get = %+v, %v", got, err)
	}
}

func TestMySQLVoiceListOrderAndCloneFields(t *testing.T) {
	store, _, _ := openTestStore(t)
	ctx := context.Background()

	first := mustCreateMySQLVoice(t, store, "vd-first-0001", "旧音色")
	second, err := store.Create(ctx, voice.CreateInput{
		VoiceID: "vd-second-0002", ChannelID: 3, TargetModel: "qwen-tts-plus",
		Name: "克隆音色", SourceMode: voice.SourceModeClone,
		SourcePrompt: "https://example.com/sample.wav",
		SampleAssetID: 42, PreviewAssetID: 43, Status: voice.StatusOK,
	})
	if err != nil {
		t.Fatalf("create clone voice: %v", err)
	}
	list, err := store.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("order = [%d %d], want newest first", list[0].ID, list[1].ID)
	}
	if list[0].SampleAssetID != 42 || list[0].PreviewAssetID != 43 || list[0].SourceMode != voice.SourceModeClone {
		t.Fatalf("clone fields = %+v", list[0])
	}
}

func TestMySQLVoiceUpdateDeleteAndInUse(t *testing.T) {
	store, characters, _ := openTestStore(t)
	ctx := context.Background()

	v := mustCreateMySQLVoice(t, store, "vd-bound-0003", "少年音")
	rename := "清亮少年音"
	updated, err := store.Update(ctx, v.ID, &voice.VoiceUpdate{Name: &rename})
	if err != nil || updated.Name != rename {
		t.Fatalf("rename = %+v, %v", updated, err)
	}

	c, err := characters.CreateCharacter(ctx, 1, character.CharacterInput{Name: "小星"})
	if err != nil {
		t.Fatalf("create character: %v", err)
	}
	zero := int64(0)
	if _, err := characters.UpdateCharacter(ctx, c.ID, &character.CharacterUpdate{VoiceID: &v.ID}); err != nil {
		t.Fatalf("bind voice to character: %v", err)
	}
	bound, err := store.BoundCharacters(ctx, v.ID)
	if err != nil || bound != 1 {
		t.Fatalf("BoundCharacters = %d, %v, want 1", bound, err)
	}
	if err := store.Delete(ctx, v.ID); err != voice.ErrVoiceInUse {
		t.Fatalf("delete bound voice = %v, want ErrVoiceInUse", err)
	}

	if _, err := characters.UpdateCharacter(ctx, c.ID, &character.CharacterUpdate{VoiceID: &zero}); err != nil {
		t.Fatalf("unbind: %v", err)
	}
	if err := store.Delete(ctx, v.ID); err != nil {
		t.Fatalf("delete after unbind: %v", err)
	}
	if _, err := store.Get(ctx, v.ID); err != voice.ErrVoiceNotFound {
		t.Fatalf("get after delete = %v, want ErrVoiceNotFound", err)
	}
}

func TestMySQLVoiceUniqueVoiceID(t *testing.T) {
	store, _, _ := openTestStore(t)
	_ = mustCreateMySQLVoice(t, store, "vd-dup-0004", "第一个")
	if _, err := store.Create(context.Background(), voice.CreateInput{
		VoiceID: "vd-dup-0004", ChannelID: 3, TargetModel: "qwen-tts-plus",
		Name: "第二个", SourceMode: voice.SourceModeDesign, Status: voice.StatusOK,
	}); err == nil {
		t.Fatalf("duplicate voice_id must be rejected by the unique key")
	}
}

func TestMySQLVoiceSentinelsAndSchemaIdempotent(t *testing.T) {
	store, _, _ := openTestStore(t)
	ctx := context.Background()

	if _, err := store.Get(ctx, 99); err != voice.ErrVoiceNotFound {
		t.Fatalf("missing voice = %v, want ErrVoiceNotFound", err)
	}
	if _, err := store.Update(ctx, 99, &voice.VoiceUpdate{}); err != voice.ErrVoiceNotFound {
		t.Fatalf("update missing = %v, want ErrVoiceNotFound", err)
	}
	if err := store.Delete(ctx, 99); err != voice.ErrVoiceNotFound {
		t.Fatalf("delete missing = %v, want ErrVoiceNotFound", err)
	}
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}
