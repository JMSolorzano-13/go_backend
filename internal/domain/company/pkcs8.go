package company

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"hash"

	"golang.org/x/crypto/pbkdf2"
)

// Encrypted PKCS#8 ASN.1 structures
// EncryptedPrivateKeyInfo ::= SEQUENCE {
//   encryptionAlgorithm  AlgorithmIdentifier,
//   encryptedData        OCTET STRING }
type encryptedPrivateKeyInfo struct {
	Algo          algorithmIdentifier
	EncryptedData []byte
}

type algorithmIdentifier struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue
}

// PBES2-params ::= SEQUENCE {
//   keyDerivationFunc AlgorithmIdentifier,
//   encryptionScheme  AlgorithmIdentifier }
type pbes2Params struct {
	KDF              algorithmIdentifier
	EncryptionScheme algorithmIdentifier
}

// PBKDF2-params ::= SEQUENCE {
//   salt           OCTET STRING,
//   iterationCount INTEGER,
//   keyLength      INTEGER OPTIONAL,
//   prf            AlgorithmIdentifier OPTIONAL }
type pbkdf2Params struct {
	Salt           []byte
	IterationCount int
	KeyLength      int              `asn1:"optional"`
	PRF            algorithmIdentifier `asn1:"optional"`
}

var (
	oidPBES2              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 13}
	oidPBKDF2             = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 12}
	oidAES256CBC          = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 42}
	oidAES128CBC          = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 2}
	oidDESEDE3CBC         = asn1.ObjectIdentifier{1, 2, 840, 113549, 3, 7}
	oidHMACWithSHA1       = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 7}
	oidHMACWithSHA256     = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 9}
	oidPBEWithSHA1And3DES = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 1, 3}
)

// PBE-SHA1-3DES params (PKCS#12 style)
type pbeParams struct {
	Salt       []byte
	Iterations int
}

// parseEncryptedPKCS8 decrypts a DER-encoded encrypted PKCS#8 private key.
// Supports PBES2 (PBKDF2 + AES-CBC or 3DES-CBC) and PBE-SHA1-3DES-CBC.
func parseEncryptedPKCS8(der, password []byte) (interface{}, error) {
	var epki encryptedPrivateKeyInfo
	if _, err := asn1.Unmarshal(der, &epki); err != nil {
		return nil, fmt.Errorf("parse encrypted PKCS#8 envelope: %w", err)
	}

	var decrypted []byte
	var err error

	switch {
	case epki.Algo.Algorithm.Equal(oidPBES2):
		decrypted, err = decryptPBES2(epki.Algo.Parameters, epki.EncryptedData, password)
	case epki.Algo.Algorithm.Equal(oidPBEWithSHA1And3DES):
		decrypted, err = decryptPBESHA1And3DES(epki.Algo.Parameters, epki.EncryptedData, password)
	default:
		return nil, fmt.Errorf("unsupported PKCS#8 encryption algorithm: %v", epki.Algo.Algorithm)
	}
	if err != nil {
		return nil, err
	}

	key, err := x509.ParsePKCS8PrivateKey(decrypted)
	if err != nil {
		return nil, fmt.Errorf("parse decrypted PKCS#8: %w", err)
	}
	return key, nil
}

func decryptPBES2(paramsRaw asn1.RawValue, encrypted, password []byte) ([]byte, error) {
	var params pbes2Params
	if _, err := asn1.Unmarshal(paramsRaw.FullBytes, &params); err != nil {
		return nil, fmt.Errorf("parse PBES2 params: %w", err)
	}

	if !params.KDF.Algorithm.Equal(oidPBKDF2) {
		return nil, fmt.Errorf("unsupported KDF: %v", params.KDF.Algorithm)
	}

	var kdfParams pbkdf2Params
	if _, err := asn1.Unmarshal(params.KDF.Parameters.FullBytes, &kdfParams); err != nil {
		return nil, fmt.Errorf("parse PBKDF2 params: %w", err)
	}

	// Determine key length and cipher from encryption scheme
	var keyLen int
	var newBlockCipher func([]byte) (cipher.Block, error)

	switch {
	case params.EncryptionScheme.Algorithm.Equal(oidAES256CBC):
		keyLen = 32
		newBlockCipher = aes.NewCipher
	case params.EncryptionScheme.Algorithm.Equal(oidAES128CBC):
		keyLen = 16
		newBlockCipher = aes.NewCipher
	case params.EncryptionScheme.Algorithm.Equal(oidDESEDE3CBC):
		keyLen = 24
		newBlockCipher = des.NewTripleDESCipher
	default:
		return nil, fmt.Errorf("unsupported encryption scheme: %v", params.EncryptionScheme.Algorithm)
	}

	// Determine hash function for PBKDF2
	hashFunc := func() hash.Hash { return sha1.New() } // default
	if kdfParams.PRF.Algorithm != nil {
		if kdfParams.PRF.Algorithm.Equal(oidHMACWithSHA256) {
			hashFunc = sha256.New
		}
	}

	// Derive key
	dk := pbkdf2.Key(password, kdfParams.Salt, kdfParams.IterationCount, keyLen, hashFunc)

	// Extract IV from encryption scheme parameters
	var iv []byte
	if _, err := asn1.Unmarshal(params.EncryptionScheme.Parameters.FullBytes, &iv); err != nil {
		return nil, fmt.Errorf("parse IV: %w", err)
	}

	block, err := newBlockCipher(dk)
	if err != nil {
		return nil, err
	}

	if len(encrypted)%block.BlockSize() != 0 {
		return nil, errors.New("ciphertext not multiple of block size")
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(encrypted))
	mode.CryptBlocks(plain, encrypted)

	return unpad(plain)
}

func decryptPBESHA1And3DES(paramsRaw asn1.RawValue, encrypted, password []byte) ([]byte, error) {
	var params pbeParams
	if _, err := asn1.Unmarshal(paramsRaw.FullBytes, &params); err != nil {
		return nil, fmt.Errorf("parse PBE-SHA1-3DES params: %w", err)
	}

	// PKCS#12 key derivation for PBE-SHA1-3DES
	dk := pbkdf2.Key(password, params.Salt, params.Iterations, 24, sha1.New)
	iv := pbkdf2.Key(password, params.Salt, params.Iterations, 8, sha1.New)

	block, err := des.NewTripleDESCipher(dk)
	if err != nil {
		return nil, err
	}

	if len(encrypted)%block.BlockSize() != 0 {
		return nil, errors.New("ciphertext not multiple of block size")
	}

	mode := cipher.NewCBCDecrypter(block, iv[:8])
	plain := make([]byte, len(encrypted))
	mode.CryptBlocks(plain, encrypted)

	return unpad(plain)
}

// unpad removes PKCS#7 padding
func unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > len(data) {
		return nil, errors.New("invalid padding")
	}
	for i := len(data) - padLen; i < len(data); i++ {
		if data[i] != byte(padLen) {
			return nil, errors.New("invalid padding bytes")
		}
	}
	return data[:len(data)-padLen], nil
}

