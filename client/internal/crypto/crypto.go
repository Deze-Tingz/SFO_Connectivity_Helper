package crypto

import (
	"crypto/rand"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/nacl/box"
)

const (
	// KeySize is the size of NaCl box keys
	KeySize = 32
	// NonceSize is the size of NaCl box nonces
	NonceSize = 24
)

// SessionKeys holds the encryption keys for a session
type SessionKeys struct {
	PublicKey  [KeySize]byte
	PrivateKey [KeySize]byte
	SharedKey  [KeySize]byte // Derived from join code
}

// DeriveKeysFromCode derives encryption keys from a join code
// Both host and joiner use the same code to derive the same shared key
func DeriveKeysFromCode(code string) (*SessionKeys, error) {
	// Use Argon2id to derive a key from the join code
	// Salt is fixed (code-based sessions are short-lived)
	salt := []byte("sfo-connectivity-helper-v1")

	// Argon2id parameters (balanced for security and speed)
	derivedKey := argon2.IDKey(
		[]byte(code),
		salt,
		1,     // time
		64*1024, // memory (64 MB)
		4,     // threads
		KeySize,
	)

	keys := &SessionKeys{}
	copy(keys.SharedKey[:], derivedKey)

	// Generate ephemeral keypair for additional forward secrecy
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	keys.PublicKey = *pub
	keys.PrivateKey = *priv

	return keys, nil
}

// Encryptor handles encryption/decryption of messages
type Encryptor struct {
	sharedKey [KeySize]byte
	myPrivate [KeySize]byte
	myPublic  [KeySize]byte
	peerPub   [KeySize]byte
	peerKnown bool
}

// NewEncryptor creates a new encryptor with session keys
func NewEncryptor(keys *SessionKeys) *Encryptor {
	return &Encryptor{
		sharedKey: keys.SharedKey,
		myPrivate: keys.PrivateKey,
		myPublic:  keys.PublicKey,
	}
}

// GetPublicKey returns our public key to send to peer
func (e *Encryptor) GetPublicKey() []byte {
	return e.myPublic[:]
}

// SetPeerPublicKey sets the peer's public key for encryption
func (e *Encryptor) SetPeerPublicKey(peerPub []byte) error {
	if len(peerPub) != KeySize {
		return errors.New("invalid peer public key size")
	}
	copy(e.peerPub[:], peerPub)
	e.peerKnown = true
	return nil
}

// Encrypt encrypts a message using NaCl box
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	if !e.peerKnown {
		return nil, errors.New("peer public key not set")
	}

	var nonce [NonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, err
	}

	// nonce + ciphertext
	encrypted := box.Seal(nonce[:], plaintext, &nonce, &e.peerPub, &e.myPrivate)
	return encrypted, nil
}

// Decrypt decrypts a message using NaCl box
func (e *Encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	if !e.peerKnown {
		return nil, errors.New("peer public key not set")
	}

	if len(ciphertext) < NonceSize {
		return nil, errors.New("ciphertext too short")
	}

	var nonce [NonceSize]byte
	copy(nonce[:], ciphertext[:NonceSize])

	plaintext, ok := box.Open(nil, ciphertext[NonceSize:], &nonce, &e.peerPub, &e.myPrivate)
	if !ok {
		return nil, errors.New("decryption failed")
	}

	return plaintext, nil
}

// EncryptWithSharedKey encrypts using just the code-derived shared key
// Used for initial key exchange messages
func (e *Encryptor) EncryptWithSharedKey(plaintext []byte) ([]byte, error) {
	var nonce [NonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, err
	}

	// Use shared key as both public and private for symmetric-like encryption
	encrypted := box.Seal(nonce[:], plaintext, &nonce, &e.sharedKey, &e.sharedKey)
	return encrypted, nil
}

// DecryptWithSharedKey decrypts using just the code-derived shared key
func (e *Encryptor) DecryptWithSharedKey(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < NonceSize {
		return nil, errors.New("ciphertext too short")
	}

	var nonce [NonceSize]byte
	copy(nonce[:], ciphertext[:NonceSize])

	plaintext, ok := box.Open(nil, ciphertext[NonceSize:], &nonce, &e.sharedKey, &e.sharedKey)
	if !ok {
		return nil, errors.New("decryption failed")
	}

	return plaintext, nil
}
