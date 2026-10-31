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
