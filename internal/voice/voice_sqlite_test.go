package voice_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/gachal/InfiniteChance/internal/character"
	"github.com/gachal/InfiniteChance/internal/sqlitedb"
	"github.com/gachal/InfiniteChance/internal/voice"
)

// newSQLiteWorld opens a throwaway desktop-shaped voice store over one temp
// database, characters schema included(Delete 的被引检查跨读 characters
// 表 —— 同库两 store 与桌面装配同形)。
func newSQLiteWorld(t *testing.T) (*voice.SQLiteStore, *character.SQLiteStore, *sql.DB) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := character.NewSQLiteStore(db).EnsureSchema(context.Background()); err != nil {
		t.Fatalf("character EnsureSchema: %v", err)
	}
	store := voice.NewSQLiteStore(db)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("voice EnsureSchema: %v", err)
	}
	return store, character.NewSQLiteStore(db), db
}

func mustCreateVoice(t *testing.T, store *voice.SQLiteStore, voiceID, name string) voice.Voice {
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

// 台账行落列与读回:全字段往返,状态与模式原样保留。
func TestSQLiteVoiceRoundTrip(t *testing.T) {
	store, _, _ := newSQLiteWorld(t)
	ctx := context.Background()

	v := mustCreateVoice(t, store, "qwen-tts-plus-vd-abcd1234-xxxx", "少年音")
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

// 全局作用域:List 不带 IP 过滤,updated_at DESC 排序;clone 模式带样本
// 与试听素材引用。
func TestSQLiteVoiceListOrderAndCloneFields(t *testing.T) {
	store, _, _ := newSQLiteWorld(t)
	ctx := context.Background()

	first := mustCreateVoice(t, store, "vd-first-0001", "旧音色")
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

// 改名(PATCH 的存储面)与删除;删除被引用时拒绝(ErrVoiceInUse),
// 解绑后可删。
func TestSQLiteVoiceUpdateDeleteAndInUse(t *testing.T) {
	store, characters, _ := newSQLiteWorld(t)
	ctx := context.Background()

	v := mustCreateVoice(t, store, "vd-bound-0003", "少年音")
	rename := "清亮少年音"
	updated, err := store.Update(ctx, v.ID, &voice.VoiceUpdate{Name: &rename})
	if err != nil || updated.Name != rename {
		t.Fatalf("rename = %+v, %v", updated, err)
	}

	// 角色绑定后:BoundCharacters 计数、Delete 拒绝。
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

	// 解除绑定后可删。
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

// 同 voice_id 重复落行被唯一索引拦下(写 入点唯一,重复即 bug)。
func TestSQLiteVoiceUniqueVoiceID(t *testing.T) {
	store, _, _ := newSQLiteWorld(t)
	_ = mustCreateVoice(t, store, "vd-dup-0004", "第一个")
	if _, err := store.Create(context.Background(), voice.CreateInput{
		VoiceID: "vd-dup-0004", ChannelID: 3, TargetModel: "qwen-tts-plus",
		Name: "第二个", SourceMode: voice.SourceModeDesign, Status: voice.StatusOK,
	}); err == nil {
		t.Fatalf("duplicate voice_id must be rejected by the unique index")
	}
}

// 缺失哨兵;EnsureSchema 幂等(重复执行不报错)。
func TestSQLiteVoiceSentinelsAndSchemaIdempotent(t *testing.T) {
	store, _, _ := newSQLiteWorld(t)
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
