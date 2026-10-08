package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
