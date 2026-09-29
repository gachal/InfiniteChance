package objectstore

import (
	"bytes"
	"context"
	"errors"
	"hash/crc64"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	cos "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/gachal/InfiniteChance/internal/settings"
)

// crcTable 与 SDK 的上传校验同一多项式(COS 的 x-cos-hash-crc64ecma)。
var crcTable = crc64.MakeTable(crc64.ECMA)

// bucket URL 拼装:地域域名 + 带 APPID 桶名 → SDK 的 virtual-host 形态;
// 裸主机名补 https;空值报错。
func TestCOSBucketURL(t *testing.T) {
	u, err := cosBucketURL("https://cos.ap-guangzhou.myqcloud.com", "demo-1250000000")
	if err != nil {
		t.Fatal(err)
	}
	if u.String() != "https://demo-1250000000.cos.ap-guangzhou.myqcloud.com" {
		t.Fatalf("bucket url = %q", u.String())
	}

	u, err = cosBucketURL("cos.ap-guangzhou.myqcloud.com", "demo-1250000000")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u.String(), "https://demo-1250000000.") {
		t.Fatalf("scheme-less endpoint should default to https, got %q", u.String())
	}

	if _, err := cosBucketURL("", "demo-1250000000"); err == nil {
		t.Fatal("empty endpoint should fail")
	}
	if _, err := cosBucketURL("cos.ap-guangzhou.myqcloud.com", " "); err == nil {
		t.Fatal("empty bucket should fail")
	}
	if _, err := cosBucketURL("://not a url", "demo-1250000000"); err == nil {
		t.Fatal("unparseable endpoint should fail")
	}
}

// cosFakeServer 是够用的 COS 形状:PUT 存、GET 读(缺键 404 + XML 错误体)、
// DELETE 幂等 —— 让驱动套件跑在真 SDK 客户端上,签名与错误解析都是真的。
func cosFakeServer(t *testing.T) (*httptest.Server, *mapStore) {
	t.Helper()
	m := &mapStore{objs: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/")
		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			m.put(key, body)
			// SDK 落盘后按此头验完整性;假桶用同一多项式现算,让校验过。
			w.Header().Set("x-cos-hash-crc64ecma", strconv.FormatUint(crc64.Checksum(body, crcTable), 10))
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			body, ok := m.get(key)
			if !ok {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>not found</Message><Resource>/` + key + `</Resource></Error>`))
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(body)
		case http.MethodDelete:
			m.del(key)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, m
}

type mapStore struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func (m *mapStore) put(k string, b []byte) { m.mu.Lock(); defer m.mu.Unlock(); m.objs[k] = b }
func (m *mapStore) get(k string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objs[k]
	return b, ok
}
func (m *mapStore) del(k string) { m.mu.Lock(); defer m.mu.Unlock(); delete(m.objs, k) }

func testCOSDriver(t *testing.T) (*COS, *mapStore) {
	t.Helper()
	srv, m := cosFakeServer(t)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	d, err := newCOS(settings.COSConfig{
		Endpoint: "cos.ap-guangzhou.myqcloud.com", Bucket: "demo-1250000000",
		SecretID: "ID", SecretKey: "SK"}, u)
	if err != nil {
		t.Fatal(err)
	}
	return d, m
}

// 驱动走真 SDK 往返:Put 落键、Open 读回字节、缺键 fs.ErrNotExist、
// Delete 幂等。
func TestCOSDriverRoundTrip(t *testing.T) {
	ctx := context.Background()
	d, m := testCOSDriver(t)

	if err := d.Put(ctx, "canvases/1/ct_1/image.png", strings.NewReader("pngbytes"), 8, "image/png"); err != nil {
		t.Fatal(err)
	}
	if b, ok := m.get("canvases/1/ct_1/image.png"); !ok || string(b) != "pngbytes" {
		t.Fatalf("fake server bytes = %q ok=%v", b, ok)
	}

	rc, err := d.Open(ctx, "canvases/1/ct_1/image.png")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, []byte("pngbytes")) {
		t.Fatalf("open bytes = %q", got)
	}

	if _, err := d.Open(ctx, "missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("open missing = %v, want fs.ErrNotExist", err)
	}

	if err := d.Delete(ctx, "canvases/1/ct_1/image.png"); err != nil {
		t.Fatal(err)
	}
	if err := d.Delete(ctx, "canvases/1/ct_1/image.png"); err != nil {
		t.Fatalf("delete is idempotent: %v", err)
	}
}

// isCOSNotFound 直接吃 SDK 的错误形态(404 / NoSuchKey),其他错误不认。
func TestIsCOSNotFoundShapes(t *testing.T) {
	if isCOSNotFound(nil) {
		t.Fatal("nil is not not-found")
	}
	if !isCOSNotFound(&cos.ErrorResponse{Code: "NoSuchKey"}) {
		t.Fatal("NoSuchKey should be not-found")
	}
	if !isCOSNotFound(&cos.ErrorResponse{Response: &http.Response{StatusCode: http.StatusNotFound}}) {
		t.Fatal("404 should be not-found")
	}
	if isCOSNotFound(&cos.ErrorResponse{Code: "AccessDenied"}) {
		t.Fatal("AccessDenied is not not-found")
	}
	if isCOSNotFound(errors.New("boom")) {
		t.Fatal("plain error is not not-found")
	}
}
