package credentials

import (
	"strings"
	"testing"
)

func TestCipherRoundTrip(t *testing.T) {
	t.Parallel()
	cipher, err := NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cipher.Encrypt("super-secret")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, "super-secret") {
		t.Fatal("ciphertext contains plaintext")
	}
	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "super-secret" {
		t.Fatalf("got %q", decrypted)
	}
}

func TestCipherRejectsWrongKeyAndTampering(t *testing.T) {
	t.Parallel()
	first, _ := NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	second, _ := NewCipher([]byte("abcdef0123456789abcdef0123456789"))
	encrypted, _ := first.Encrypt("secret")
	if _, err := second.Decrypt(encrypted); err == nil {
		t.Fatal("expected wrong key failure")
	}
	if _, err := first.Decrypt(encrypted + "x"); err == nil {
		t.Fatal("expected tamper failure")
	}
}

func TestNewCipherRequiresAES256Key(t *testing.T) {
	t.Parallel()
	if _, err := NewCipher([]byte("short")); err == nil {
		t.Fatal("expected key validation error")
	}
}

func TestCipherRejectsMalformedEnvelopesAndUsesRandomNonce(t *testing.T) {
	t.Parallel()
	cipher, _ := NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	first, _ := cipher.Encrypt("same")
	second, _ := cipher.Encrypt("same")
	if first == second {
		t.Fatal("encryption reused nonce")
	}
	for _, value := range []string{"plaintext", "v2:a:b", "v1:not-base64!:YQ", "v1:YQ:not-base64!"} {
		if _, err := cipher.Decrypt(value); err == nil {
			t.Fatalf("accepted malformed envelope %q", value)
		}
	}
}
