package canvastask_test

import (
	"context"
	"database/sql"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/canvastask"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/sqlitedb"
	"github.com/gachal/InfiniteChance/internal/voice"
)

// newSQLiteVoiceWorld opens a throwaway desktop-shaped stack over one temp
// database (57 号票):canvas tasks + assets + voices 全就位 —— runVoice 的
// finalize 与 runTts 的台账读取跨三张表,单库装配与桌面壳同形。worker 测试
// 走 SQLite 使音色闭环的轮询/句柄/终态逻辑不依赖 MySQL 基础设施。
func newSQLiteVoiceWorld(t *testing.T) (*canvastask.SQLiteStore, *voice.SQLiteStore, *sql.DB) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	assets := asset.NewSQLiteStore(db)
	if err := assets.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("asset EnsureSchema: %v", err)
	}
	if err := voice.NewSQLiteStore(db).EnsureSchema(context.Background()); err != nil {
		t.Fatalf("voice EnsureSchema: %v", err)
	}
	store := canvastask.NewSQLiteStore(db, assets)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("canvastask EnsureSchema: %v", err)
	}
	return store, voice.NewSQLiteStore(db), db
}

// stubChannels is the read-only channel directory the worker auto-pins
// from (57 号票)。
type stubChannels struct{ list []channel.Channel }

func (s *stubChannels) List(context.Context) ([]channel.Channel, error) { return s.list, nil }

func audioChannel(id int64, priority int, models ...string) channel.Channel {
	m := map[string]string{}
	for _, name := range models {
		m[name] = "upstream-" + name
	}
	return channel.Channel{
		ID: id, Name: "audio", Enabled: true, Priority: priority,
		ModelMap:     m,
		Capabilities: []channel.Capability{channel.CapAudio},
	}
}

// newSQLiteVoiceWorker wires a worker tuned for the voice-loop tests: fast
// polling, short voice deadline, quiet logging.
func newSQLiteVoiceWorker(store *canvastask.SQLiteStore, gateway *stubGateway, opts ...canvastask.WorkerOption) *canvastask.Worker {
	base := []canvastask.WorkerOption{
		canvastask.WithPollInterval(20 * time.Millisecond),
		canvastask.WithTaskTimeout(3 * time.Second),
		canvastask.WithVoicePollInterval(10 * time.Millisecond),
		canvastask.WithVoiceTaskTimeout(2 * time.Second),
		canvastask.WithConcurrency(1),
		canvastask.WithLogger(log.New(&strings.Builder{}, "", 0)),
	}
	return canvastask.NewWorker(store, gateway, append(base, opts...)...)
}

// runSQLiteWorker starts the worker loop and stops it at test cleanup
// (runWorker 的 SQLite 版:泛型 harness 只收 MySQL store)。
func runSQLiteWorker(t *testing.T, w *canvastask.Worker) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("worker did not stop within 2s of context cancel")
		}
	})
}

// awaitSQLiteTask polls until terminal (SQLite store 形态的 awaitTask)。
func awaitSQLiteTask(t *testing.T, store *canvastask.SQLiteStore, id string) canvastask.Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task, err := store.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if canvastask.Terminal(task.Status) {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s still active after 5s", id)
	return canvastask.Task{}
}

func seedVoiceTask(t *testing.T, store *canvastask.SQLiteStore, spec canvastask.VoiceSpec, prompt, model string) canvastask.Task {
	t.Helper()
	id, err := canvastask.NewID()
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create(context.Background(), canvastask.Task{
		ID: id, CanvasID: 7, NodeID: "voice-1", Kind: canvastask.KindVoice,
		Prompt: prompt, Model: model, VoiceSpec: spec,
	})
	if err != nil {
		t.Fatalf("seed voice task: %v", err)
	}
	return task
}

const previewText = "这一段台词用来试听音色的整体效果如何。"

// 设计模式全链:自动钉渠道(选优先级最高且映射命中的)→ create 收句柄
// → 轮询至 OK → voices 台账行 + 任务终态同事务;任务行带钉定的渠道与
// 上游句柄。
func TestSQLiteWorkerVoiceHappyPath(t *testing.T) {
	store, voices, _ := newSQLiteVoiceWorld(t)
	gateway := &stubGateway{
		createVoiceFn: func(_ context.Context, req canvastask.VoiceCreateRequest) (canvastask.VoiceCreateResult, error) {
			if req.ChannelID != 9 {
				t.Errorf("create channel_id = %d, want the higher-priority channel 9", req.ChannelID)
			}
			if req.Mode != "design" || req.TargetModel != "qwen-tts-plus" || req.PreviewText != previewText {
				t.Errorf("create request = %+v", req)
			}
			return canvastask.VoiceCreateResult{VoiceID: "vd-xyz", Status: "DEPLOYING"}, nil
		},
		queryVoiceFn: func(call int) (string, error) {
			if call == 1 {
				return "DEPLOYING", nil
			}
			return "OK", nil
		},
	}
	chans := &stubChannels{list: []channel.Channel{
		audioChannel(3, 10, "voice-enrollment", "qwen-tts-plus"),
		audioChannel(9, 20, "voice-enrollment", "qwen-tts-plus"),
	}}
	runSQLiteWorker(t, newSQLiteVoiceWorker(store, gateway,
		canvastask.WithChannels(chans), canvastask.WithVoiceLedger(voices)))

	task := seedVoiceTask(t, store, canvastask.VoiceSpec{
		Mode: "design", Name: "少年音", TargetModel: "qwen-tts-plus",
		VoicePrompt: "低沉磁性男声", PreviewText: previewText,
	}, previewText, "qwen-tts-plus")
	final := awaitSQLiteTask(t, store, task.ID)

	if final.Status != canvastask.StatusSucceeded {
		t.Fatalf("task = %+v (%q), want succeeded", final.Status, final.Error)
	}
	if final.VoiceSpec.ChannelID != 9 || final.VoiceSpec.RemoteVoiceID != "vd-xyz" {
		t.Fatalf("voice_spec = %+v, want pinned channel 9 and remote handle", final.VoiceSpec)
	}
	list, err := voices.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("voices = %+v, %v", list, err)
	}
	got := list[0]
	if got.VoiceID != "vd-xyz" || got.ChannelID != 9 || got.Status != voice.StatusOK ||
		got.SourceMode != "design" || got.SourcePrompt != "低沉磁性男声" || got.Name != "少年音" {
		t.Fatalf("ledger row = %+v", got)
	}
	if len(gateway.voiceQueries) < 2 || gateway.voiceQueries[len(gateway.voiceQueries)-1] != "vd-xyz" {
		t.Fatalf("queries = %v", gateway.voiceQueries)
	}
}

// UNDEPLOYED:任务失败、原因写明上游审核未通过、句柄被丢弃(重试走全新
// 创建);台账不落行。
func TestSQLiteWorkerVoiceUndeployedFailsAndDropsHandle(t *testing.T) {
	store, _, db := newSQLiteVoiceWorld(t)
	gateway := &stubGateway{
		createVoiceFn: func(context.Context, canvastask.VoiceCreateRequest) (canvastask.VoiceCreateResult, error) {
			return canvastask.VoiceCreateResult{VoiceID: "vd-bad", Status: "DEPLOYING"}, nil
		},
		queryVoiceFn: func(int) (string, error) { return "UNDEPLOYED", nil },
	}
	runSQLiteWorker(t, newSQLiteVoiceWorker(store, gateway))

	task := seedVoiceTask(t, store, canvastask.VoiceSpec{
		Mode: "design", Name: "少年音", TargetModel: "qwen-tts-plus",
		VoicePrompt: "描述", PreviewText: previewText,
		ChannelID: 3,
	}, previewText, "qwen-tts-plus")
	final := awaitSQLiteTask(t, store, task.ID)

	if final.Status != canvastask.StatusFailed || !strings.Contains(final.Error, "审核未通过") {
		t.Fatalf("task = %+v (%q), want failed with 审核未通过", final.Status, final.Error)
	}
	if final.VoiceSpec.RemoteVoiceID != "" {
		t.Fatalf("handle = %q, want dropped", final.VoiceSpec.RemoteVoiceID)
	}
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM voices`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("voices rows = %d, %v, want 0", count, err)
	}
}

// 重试带句柄:先 query 校验,已 OK 直接收尾,不再重复 create(不重复扣
// 定制费);崩溃恢复丢预览的形态下台账落无试听行,clone 出处记样本地址。
func TestSQLiteWorkerVoiceRetryWithHandleFinalizesWithoutCreate(t *testing.T) {
	store, voices, _ := newSQLiteVoiceWorld(t)
	gateway := &stubGateway{
		queryVoiceFn: func(int) (string, error) { return "OK", nil },
	}
	runSQLiteWorker(t, newSQLiteVoiceWorker(store, gateway))

	task := seedVoiceTask(t, store, canvastask.VoiceSpec{
		Mode: "clone", Name: "克隆音色", TargetModel: "qwen-tts-plus",
		PreviewText: previewText,
		ChannelID:   3, RemoteVoiceID: "vd-alive", SampleAssetID: 5, AudioURL: "https://example.com/s.wav",
	}, previewText, "qwen-tts-plus")
	final := awaitSQLiteTask(t, store, task.ID)

	if final.Status != canvastask.StatusSucceeded {
		t.Fatalf("task = %+v (%q), want succeeded via handle query", final.Status, final.Error)
	}
	if len(gateway.voices) != 0 {
		t.Fatalf("create calls = %d, want 0 (句柄在手不重复创建)", len(gateway.voices))
	}
	list, err := voices.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("voices = %+v, %v", list, err)
	}
	if list[0].VoiceID != "vd-alive" || list[0].SourcePrompt != "https://example.com/s.wav" || list[0].SampleAssetID != 5 {
		t.Fatalf("ledger row = %+v", list[0])
	}
	if list[0].PreviewAssetID != 0 {
		t.Fatalf("preview asset = %d, want 0 (崩溃恢复无预览形态)", list[0].PreviewAssetID)
	}
}

// 轮询超时:任务失败但句柄保留(重试续查不重复创建)。
func TestSQLiteWorkerVoicePollTimeoutKeepsHandle(t *testing.T) {
	store, _, _ := newSQLiteVoiceWorld(t)
	gateway := &stubGateway{
		createVoiceFn: func(context.Context, canvastask.VoiceCreateRequest) (canvastask.VoiceCreateResult, error) {
			return canvastask.VoiceCreateResult{VoiceID: "vd-slow", Status: "DEPLOYING"}, nil
		},
		queryVoiceFn: func(int) (string, error) { return "DEPLOYING", nil },
	}
	runSQLiteWorker(t, newSQLiteVoiceWorker(store, gateway))

	task := seedVoiceTask(t, store, canvastask.VoiceSpec{
		Mode: "design", Name: "少年音", TargetModel: "qwen-tts-plus",
		VoicePrompt: "描述", PreviewText: previewText,
		ChannelID: 3,
	}, previewText, "qwen-tts-plus")
	final := awaitSQLiteTask(t, store, task.ID)

	if final.Status != canvastask.StatusFailed || !strings.Contains(final.Error, "超时") {
		t.Fatalf("task = %+v (%q), want failed with 超时", final.Status, final.Error)
	}
	if final.VoiceSpec.RemoteVoiceID != "vd-slow" || final.VoiceSpec.ChannelID != 3 {
		t.Fatalf("voice_spec = %+v, want handle and channel preserved", final.VoiceSpec)
	}
}

// 台词合成全链:台账行 OK → speech(钉台账渠道与上游句柄)→ kind=audio
// 素材行 → 任务终态同事务。
func TestSQLiteWorkerTtsHappyPath(t *testing.T) {
	store, voices, db := newSQLiteVoiceWorld(t)
	ctx := context.Background()
	ledger, err := voices.Create(ctx, voice.CreateInput{
		VoiceID: "vd-tts", ChannelID: 4, TargetModel: "qwen-tts-plus",
		Name: "少年音", SourceMode: voice.SourceModeDesign, Status: voice.StatusOK,
	})
	if err != nil {
		t.Fatal(err)
	}

	gateway := &stubGateway{
		speechFn: func(_ context.Context, req canvastask.SpeechRequest) (canvastask.SpeechResult, error) {
			if req.ChannelID != 4 || req.Voice != "vd-tts" || req.Model != "qwen-tts-plus" {
				t.Errorf("speech request = %+v", req)
			}
			if req.Instruction != "带笑意" {
				t.Errorf("instruction = %q, want 带笑意", req.Instruction)
			}
			if req.Text != "你好,世界。" {
				t.Errorf("text = %q", req.Text)
			}
			return canvastask.SpeechResult{AudioURL: "https://audio.example/out.wav", Chars: 6}, nil
		},
	}
	runSQLiteWorker(t, newSQLiteVoiceWorker(store, gateway, canvastask.WithVoiceLedger(voices)))

	id, err := canvastask.NewID()
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create(ctx, canvastask.Task{
		ID: id, CanvasID: 7, NodeID: "audio-1", Kind: canvastask.KindTts,
		Prompt: "你好,世界。", Model: "qwen-tts-plus",
		VoiceSpec: canvastask.VoiceSpec{LedgerID: ledger.ID, Instruction: "带笑意"},
	})
	if err != nil {
		t.Fatal(err)
	}
	final := awaitSQLiteTask(t, store, task.ID)
	if final.Status != canvastask.StatusSucceeded || final.AssetID == 0 {
		t.Fatalf("task = %+v (%q), want succeeded with asset", final.Status, final.Error)
	}
	a, err := asset.NewSQLiteStore(db).Get(ctx, final.AssetID)
	if err != nil {
		t.Fatalf("asset Get: %v", err)
	}
	if a.Kind != asset.KindAudio || a.CanvasID != 7 || a.TaskID != task.ID || a.URL != "https://audio.example/out.wav" {
		t.Fatalf("asset = %+v, want kind=audio with task provenance", a)
	}
}

// 台账行不可用(UNDEPLOYED)时任务失败,给可读原因。
func TestSQLiteWorkerTtsRejectsUnavailableVoice(t *testing.T) {
	store, voices, _ := newSQLiteVoiceWorld(t)
	ctx := context.Background()
	ledger, err := voices.Create(ctx, voice.CreateInput{
		VoiceID: "vd-na", ChannelID: 4, TargetModel: "qwen-tts-plus",
		Name: "未审核", SourceMode: voice.SourceModeDesign, Status: voice.StatusUndeployed,
	})
	if err != nil {
		t.Fatal(err)
	}
	gateway := &stubGateway{}
	runSQLiteWorker(t, newSQLiteVoiceWorker(store, gateway, canvastask.WithVoiceLedger(voices)))

	id, err := canvastask.NewID()
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create(ctx, canvastask.Task{
		ID: id, CanvasID: 7, NodeID: "audio-2", Kind: canvastask.KindTts,
		Prompt: "你好,世界。", Model: "qwen-tts-plus",
		VoiceSpec: canvastask.VoiceSpec{LedgerID: ledger.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	final := awaitSQLiteTask(t, store, task.ID)
	if final.Status != canvastask.StatusFailed || !strings.Contains(final.Error, "尚不可合成") {
		t.Fatalf("task = %+v (%q), want failed with 尚不可合成", final.Status, final.Error)
	}
	if len(gateway.speeches) != 0 {
		t.Fatalf("speech calls = %d, want 0", len(gateway.speeches))
	}
}

// 台账行已删:任务失败,可读原因(素材行不产生)。
func TestSQLiteWorkerTtsFailsOnMissingLedger(t *testing.T) {
	store, voices, _ := newSQLiteVoiceWorld(t)
	gateway := &stubGateway{}
	runSQLiteWorker(t, newSQLiteVoiceWorker(store, gateway, canvastask.WithVoiceLedger(voices)))

	id, err := canvastask.NewID()
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create(context.Background(), canvastask.Task{
		ID: id, CanvasID: 7, NodeID: "audio-3", Kind: canvastask.KindTts,
		Prompt: "你好,世界。", Model: "qwen-tts-plus",
		VoiceSpec: canvastask.VoiceSpec{LedgerID: 999},
	})
	if err != nil {
		t.Fatal(err)
	}
	final := awaitSQLiteTask(t, store, task.ID)
	if final.Status != canvastask.StatusFailed || !strings.Contains(final.Error, "不存在") {
		t.Fatalf("task = %+v (%q), want failed with 不存在", final.Status, final.Error)
	}
}

// 渠道目录为空:自动钉渠道失败,任务给可读原因(不盲目提交注定失败的
// 网关调用)。
func TestSQLiteWorkerVoiceFailsWithoutCapableChannel(t *testing.T) {
	store, _, _ := newSQLiteVoiceWorld(t)
	gateway := &stubGateway{}
	runSQLiteWorker(t, newSQLiteVoiceWorker(store, gateway,
		canvastask.WithChannels(&stubChannels{list: []channel.Channel{
			audioChannel(3, 10, "voice-enrollment"), // 缺 target_model 映射
		}})))

	task := seedVoiceTask(t, store, canvastask.VoiceSpec{
		Mode: "design", Name: "少年音", TargetModel: "qwen-tts-plus",
		VoicePrompt: "描述", PreviewText: previewText,
	}, previewText, "qwen-tts-plus")
	final := awaitSQLiteTask(t, store, task.ID)
	if final.Status != canvastask.StatusFailed || !strings.Contains(final.Error, "渠道") {
		t.Fatalf("task = %+v (%q), want failed with 渠道 reason", final.Status, final.Error)
	}
	if len(gateway.voices) != 0 {
		t.Fatalf("create calls = %d, want 0", len(gateway.voices))
	}
	if len(gateway.voiceQueries) != 0 {
		t.Fatalf("query calls = %d, want 0", len(gateway.voiceQueries))
	}
}
