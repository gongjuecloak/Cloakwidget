package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSingleConversionRejectsConcurrentRequest(t *testing.T) {
	if !conversionMu.TryLock() {
		t.Fatal("转换锁已被占用")
	}
	defer conversionMu.Unlock()

	called := false
	h := singleConversion(func(http.ResponseWriter, *http.Request) { called = true })
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodPost, "/api/convert", nil))
	if called || w.Code != http.StatusConflict {
		t.Fatalf("并发转换应返回 409，called=%v code=%d", called, w.Code)
	}
}

func TestClassifySourceFieldReportsSaveFailure(t *testing.T) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("source", "orders.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("not important"))
	_ = mw.Close()

	r := httptest.NewRequest(http.MethodPost, "/api/orders/convert", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	missingDir := filepath.Join(t.TempDir(), "missing")
	var orderPaths, workPaths, orderNames, workNames []string
	var detected []map[string]string
	err = classifySourceField(r, missingDir, defaultOrdersConfig(),
		&orderPaths, &workPaths, &orderNames, &workNames, &detected)
	if err == nil || !strings.Contains(err.Error(), "orders.xlsx") {
		t.Fatalf("保存失败必须带文件名返回，got=%v", err)
	}
}

func TestSameOriginMiddleware(t *testing.T) {
	called := false
	h := sameOriginMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8090/api/config", nil)
	r.Host = "127.0.0.1:8090"
	r.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if called || w.Code != http.StatusForbidden {
		t.Fatalf("跨站 POST 应被拒绝，called=%v code=%d", called, w.Code)
	}

	for _, origin := range []string{"http://127.0.0.1:8090", ""} {
		called = false
		r = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8090/api/config", nil)
		r.Host = "127.0.0.1:8090"
		r.Header.Set("Origin", origin)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if !called {
			t.Fatalf("同源或无 Origin 的请求应放行，origin=%q code=%d", origin, w.Code)
		}
	}
}

func TestPostOnly(t *testing.T) {
	called := false
	h := postOnly(func(http.ResponseWriter, *http.Request) { called = true })
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, "/api/open-folder", nil))
	if called || w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET 应返回 405，called=%v code=%d", called, w.Code)
	}
	h(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/open-folder", nil))
	if !called {
		t.Fatal("POST 应放行")
	}
}

func TestLimitBodyMiddlewareUsesRouteLimits(t *testing.T) {
	called := false
	h := limitBodyMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	request := func(path string, size int64) int {
		called = false
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("x"))
		r.ContentLength = size
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := request("/api/config", maxBodyBytes+1); code != http.StatusRequestEntityTooLarge || called {
		t.Fatalf("普通接口超限应返回 413，code=%d called=%v", code, called)
	}
	if code := request("/api/convert", maxBodyBytes+1); code != http.StatusOK || !called {
		t.Fatalf("转换接口应允许超过 4MB，code=%d called=%v", code, called)
	}
	if code := request("/api/convert", maxUploadBytes+1); code != http.StatusRequestEntityTooLarge || called {
		t.Fatalf("转换接口超过 256MB 应返回 413，code=%d called=%v", code, called)
	}
}

func TestUploadParseErrorReturns413ForUnknownLengthOverflow(t *testing.T) {
	w := httptest.NewRecorder()
	writeUploadParseError(w, &http.MaxBytesError{Limit: maxUploadBytes})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("MaxBytesError 应返回 413，got=%d", w.Code)
	}
}
