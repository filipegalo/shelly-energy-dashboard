package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

type Cipher struct {
	key []byte
}

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, errors.New("a 32-byte encryption key is required")
	}
	return &Cipher{key: append([]byte(nil), key...)}, nil
}

func (c *Cipher) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nil, nonce, []byte(plaintext), []byte("shelly-inventory:credential:v1"))
	return "v1:" + base64.RawStdEncoding.EncodeToString(nonce) + ":" + base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func (c *Cipher) Decrypt(envelope string) (string, error) {
	parts := strings.Split(envelope, ":")
	if len(parts) != 3 || parts[0] != "v1" {
		return "", errors.New("unsupported credential envelope")
	}
	nonce, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("invalid credential nonce")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", errors.New("invalid credential ciphertext")
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte("shelly-inventory:credential:v1"))
	if err != nil {
		return "", fmt.Errorf("decrypt credential: %w", err)
	}
	return string(plaintext), nil
}
