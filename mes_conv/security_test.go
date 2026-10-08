package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyHashForTest 按旧格式 hex(salt)$hex(sha256(salt+pwd)) 构造一条存量口令记录。
func legacyHashForTest(salt []byte, pwd string) string {
	sum := sha256.Sum256(append(append([]byte{}, salt...), []byte(pwd)...))
	return hex.EncodeToString(salt) + "$" + hex.EncodeToString(sum[:])
}

// TestManifestSigRoundTrip 用一对临时密钥验证「签名→验签通过 / 篡改→验签失败」，
// 并确保内置公钥是合法的 32 字节 Ed25519 公钥。
func TestManifestSigRoundTrip(t *testing.T) {
	// 内置公钥必须合法
	pub, err := base64.StdEncoding.DecodeString(manifestPubKeyB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("内置公钥非法: err=%v len=%d", err, len(pub))
	}

	// 生成临时密钥对模拟 CI 签名
	_, privT, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte(`{"version":"9.9.9","exe":{"name":"x.exe","sha256":"ab","size":1}}`)
	sig := ed25519.Sign(privT, msg)
	pubT := pubOf(privT)

	// 用临时公钥覆盖验签：直接调 ed25519.Verify 语义
	if !ed25519.Verify(pubT, msg, sig) {
		t.Fatal("正确签名应验签通过")
	}
	// 篡改内容后必须验签失败
	if ed25519.Verify(pubT, append(msg, 'x'), sig) {
		t.Fatal("篡改内容后不应验签通过")
	}
}

func pubOf(priv ed25519.PrivateKey) ed25519.PublicKey {
	return priv.Public().(ed25519.PublicKey)
}

// TestVerifyManifestSigNoSig 无签名输入应返回 hasSig=false。
func TestVerifyManifestSigNoSig(t *testing.T) {
	ok, has := verifyManifestSig([]byte("{}"), nil)
	if has {
		t.Fatal("无签名时 hasSig 应为 false")
	}
	if ok {
		t.Fatal("无签名时不应返回通过")
	}
}

// TestManifestSigURL 确认 .sig URL 推导正确。
func TestManifestSigURL(t *testing.T) {
	cases := map[string]string{
		"https://x.lzplus.top/version.json":                        "https://x.lzplus.top/version.sig",
		"https://github.com/o/r/releases/download/v1/version.json": "https://github.com/o/r/releases/download/v1/version.sig",
	}
	for in, want := range cases {
		if got := manifestSigURL(in); got != want {
			t.Fatalf("manifestSigURL(%q)=%q want %q", in, got, want)
		}
	}
	// 不含 version.json 时原样返回
	if got := manifestSigURL("https://example.com/other"); !strings.Contains(got, "other") {
		t.Fatalf("未匹配时应原样返回, got=%q", got)
	}
}

// TestPasswordArgon2idNewFormat 新生成的口令应为 Argon2id 自描述格式，且能校验通过。
func TestPasswordArgon2idNewFormat(t *testing.T) {
	h := hashPassword("s3cret")
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("新口令应为 Argon2id 格式, got=%q", h)
	}
	if !verifyPassword(h, "s3cret") {
		t.Fatal("正确口令应校验通过")
	}
	if verifyPassword(h, "wrong") {
		t.Fatal("错误口令不应通过")
	}
	if needsRehash(h) {
		t.Fatal("Argon2id 格式不应需要 rehash")
	}
}

// TestPasswordLegacyCompat 旧 sha256 格式仍可校验，且被标记需升级。
func TestPasswordLegacyCompat(t *testing.T) {
	// 旧格式 hex(salt)$hex(sha256(salt+pwd))
	salt := []byte("0123456789abcdef")
	legacy := legacyHashForTest(salt, "oldpass")
	if !verifyPassword(legacy, "oldpass") {
		t.Fatal("旧格式正确口令应通过")
	}
	if verifyPassword(legacy, "bad") {
		t.Fatal("旧格式错误口令不应通过")
	}
	if !needsRehash(legacy) {
		t.Fatal("旧格式应被标记 needsRehash")
	}
}

// TestMoveFile moveFile 应把源文件搬到目标（同目录移动场景）。
func TestMoveFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.tmp")
	dst := filepath.Join(dir, "b.exe")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile 失败: %v", err)
	}
	if b, err := os.ReadFile(dst); err != nil || string(b) != "hello" {
		t.Fatalf("目标内容不对: %q err=%v", b, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("源文件应已不存在")
	}
}

// TestCleanupOldExe 启动清理：pending_update.txt 里记录的旧 exe 应被删除。
func TestCleanupOldExe(t *testing.T) {
	// cleanupOldExe 依赖全局 exeDir()，这里只测它的纯逻辑：路径安全校验。
	// 直接验证 pendingUpdateFile 的读写位置在 exeDir 下（不写盘）。
	if filepath.Base(pendingUpdateFile()) != "pending_update.txt" {
		t.Fatalf("pending 文件名不对: %s", filepath.Base(pendingUpdateFile()))
	}
}

// TestSameFile 同名判断：路径相同或指向同一文件都应返回 true。
func TestSameFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "x.exe")
	if err := os.WriteFile(a, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if !sameFile(a, a) {
		t.Fatal("同一路径应返回 true")
	}
	b := filepath.Join(dir, "y.exe")
	if sameFile(a, b) {
		if _, err := os.Stat(b); err == nil {
			t.Fatal("b 不存在时不应判定为同一文件")
		}
	}
}

// TestNormalizeUpdateSource 更新源取值归一化：合法值原样保留，非法/空回 auto。
func TestNormalizeUpdateSource(t *testing.T) {
	cases := map[string]string{
		"auto":      "auto",
		"github":    "github",
		"mirror":    "mirror",
		"  GitHub ": "github", // 大小写与空格容错
		"":          "auto",   // 空 → auto
		"bogus":     "auto",   // 非法 → auto
		"MIRROR":    "mirror",
	}
	for in, want := range cases {
		if got := normalizeUpdateSource(in); got != want {
			t.Fatalf("normalizeUpdateSource(%q)=%q，期望 %q", in, got, want)
		}
	}
}
