package asset_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/gachal/InfiniteChance/internal/asset"
)

// ---- 48 号票:内容哈希去重(上传复用 + 删除防护) ----

// TestUploadSameBytesTwiceSharesObject 锁定去重主链:同一文件两次上传 =
// 素材库两条记录、存储一个对象;两条 content 端点都可读;删其一另一条预
// 览不坏;删光后对象字节消失。
func TestUploadSameBytesTwiceSharesObject(t *testing.T) {
	env := newAssetEnv(t)
	ctx := context.Background()

	w := postUpload(t, env.engine, "photo.png", "image", pngBytes)
	if w.Code != http.StatusCreated {
		t.Fatalf("first upload status = %d body %s, want 201", w.Code, w.Body.String())
	}
	first := decodeUpload(t, w)
	w = postUpload(t, env.engine, "again.png", "image", pngBytes)
	if w.Code != http.StatusCreated {
		t.Fatalf("second upload status = %d body %s, want 201", w.Code, w.Body.String())
	}
	second := decodeUpload(t, w)

	if first.ID == second.ID {
		t.Fatalf("both uploads landed on row %d, want two records", first.ID)
	}
	row1, row2 := env.store.assets[first.ID], env.store.assets[second.ID]
	if row1.ObjectKey != row2.ObjectKey {
		t.Fatalf("object keys %q vs %q, want one shared object", row1.ObjectKey, row2.ObjectKey)
	}
	if !strings.HasPrefix(row2.ObjectKey, "uploads/") {
		t.Errorf("object key = %q, want the first upload's uploads/ key", row2.ObjectKey)
	}
	if row1.ContentHash == "" || row1.ContentHash != row2.ContentHash {
		t.Errorf("content hashes %q vs %q, want equal non-empty fingerprints", row1.ContentHash, row2.ContentHash)
	}

	// 两条 content 端点都读得出同一份字节。
	for _, id := range []int64{first.ID, second.ID} {
		w := env.do(t, http.MethodGet, idPath(id, "/content"))
		if w.Code != http.StatusOK || w.Body.String() != string(pngBytes) {
			t.Errorf("asset %d content status = %d, want 200 with the bytes", id, w.Code)
		}
	}

	// 删其一:只删行,共享对象仍在,兄弟行预览不坏。
	if w := env.do(t, http.MethodDelete, idPath(first.ID, "")); w.Code != http.StatusNoContent {
		t.Fatalf("delete first status = %d, want 204", w.Code)
	}
	if _, err := env.storage.Open(ctx, row2.ObjectKey); err != nil {
		t.Fatalf("shared object deleted along with one row: %v", err)
	}
	if w := env.do(t, http.MethodGet, idPath(second.ID, "/content")); w.Code != http.StatusOK {
		t.Errorf("sibling content status = %d, want 200 (删一行另一条预览不坏)", w.Code)
	}

	// 删光:对象字节随之消失。
	if w := env.do(t, http.MethodDelete, idPath(second.ID, "")); w.Code != http.StatusNoContent {
		t.Fatalf("delete second status = %d, want 204", w.Code)
	}
	if _, err := env.storage.Open(ctx, row2.ObjectKey); err == nil {
		t.Errorf("object survived deleting every row pointing at it")
	}
}

// TestUploadReusesCrossPrefixObject 全前缀口径:下载画布产物再上传,增量
// 上传即命中 canvases/ 对象复用 —— 新行指向既有对象,零字节上云。
func TestUploadReusesCrossPrefixObject(t *testing.T) {
	env := newAssetEnv(t)
	ctx := context.Background()

	const canvasKey = "canvases/7/ct_old/image.png"
	seeded, err := env.store.Create(ctx, asset.Asset{
		Kind: asset.KindImage, CanvasID: 7, URL: "https://img.example/old.png",
		ObjectKey:   canvasKey,
		ContentType: "image/png", SizeBytes: int64(len(pngBytes)),
		ContentHash: sha256Hex(pngBytes),
	})
	if err != nil {
		t.Fatalf("seed canvas asset: %v", err)
	}
	if err := env.storage.Put(ctx, canvasKey, strings.NewReader(string(pngBytes)), int64(len(pngBytes)), "image/png"); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	w := postUpload(t, env.engine, "downloaded.png", "image", pngBytes)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s, want 201", w.Code, w.Body.String())
	}
	uploaded := decodeUpload(t, w)
	stored := env.store.assets[uploaded.ID]
	if stored.ObjectKey != canvasKey {
		t.Fatalf("object key = %q, want the canvases/ key %q reused", stored.ObjectKey, canvasKey)
	}
	if seeded.ContentHash != stored.ContentHash {
		t.Errorf("hash = %q, want the seeded fingerprint", stored.ContentHash)
	}
	// 复用行的 content 端点照常读出共享字节。
	if w := env.do(t, http.MethodGet, idPath(uploaded.ID, "/content")); w.Code != http.StatusOK {
		t.Errorf("reused row content status = %d, want 200", w.Code)
	}
}

// sha256Hex 是测试侧的指纹参考实现(与 asset 包内的 hashBytes 互证)。
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
